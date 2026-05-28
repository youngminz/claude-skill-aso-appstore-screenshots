#!/usr/bin/env -S uv run
# /// script
# requires-python = ">=3.13"
# dependencies = [
#   "Pillow>=11.2.1",
# ]
# ///

from __future__ import annotations

import argparse
import os
import json
import shutil
import subprocess
import tempfile
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from pathlib import Path

from PIL import Image


SKILL_DIR = Path(__file__).resolve().parents[1]
DEFAULT_CONFIG_PATH = Path("scripts") / "aso_localized_screenshots_config.json"
PNGQUANT_QUALITY = "80-95"
PNGQUANT_SPEED = 1


@dataclass(frozen=True)
class ShotDefinition:
    slug: str
    source_filename: str | None = None
    source_index: int | None = None


@dataclass(frozen=True)
class ShotCopy:
    slug: str
    source_filename: str | None
    source_index: int | None
    verb: str
    desc: str


@dataclass(frozen=True)
class LocaleConfig:
    output_locale: str
    source_locale: str
    compose_locale: str
    shots: tuple[ShotCopy, ...]


@dataclass(frozen=True)
class DeviceConfig:
    source_root: Path
    source_filename_prefix: str
    source_glob: str | None
    output_root: Path
    output_subdir: str | None
    compose_device: str
    target_size: tuple[int, int]


@dataclass(frozen=True)
class RenderTask:
    device: DeviceConfig
    locale: LocaleConfig
    shot: ShotCopy
    output_path: Path


@dataclass(frozen=True)
class AppConfig:
    brand_color: str
    locale_configs: tuple[LocaleConfig, ...]
    device_configs: dict[str, DeviceConfig]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Generate localized ASO scaffold screenshots from a JSON config."
    )
    parser.add_argument(
        "--project-root",
        type=Path,
        default=Path.cwd(),
        help="App project root. Relative paths in the config are resolved from here.",
    )
    parser.add_argument(
        "--config",
        type=Path,
        default=DEFAULT_CONFIG_PATH,
        help="Path to the JSON config file. Relative paths are resolved from --project-root.",
    )
    parser.add_argument(
        "--device",
        action="append",
        help="Limit generation to one or more devices defined in the config.",
    )
    parser.add_argument(
        "--locale",
        action="append",
        help="Limit generation to one or more App Store locales defined in the config.",
    )
    parser.add_argument(
        "--skip-existing",
        action="store_true",
        help="Skip files that already exist in the target output folders.",
    )
    parser.add_argument(
        "--jobs",
        type=int,
        default=max(1, os.cpu_count() or 1),
        help="Maximum number of concurrent render jobs. Defaults to CPU core count.",
    )
    return parser.parse_args()


def resolve_from_root(project_root: Path, path: Path) -> Path:
    return path if path.is_absolute() else project_root / path


def display_path(path: Path, project_root: Path) -> str:
    try:
        return str(path.relative_to(project_root))
    except ValueError:
        return str(path)


def load_config(config_path: Path, project_root: Path) -> AppConfig:
    data = json.loads(config_path.read_text(encoding="utf-8"))

    shots = tuple(
        ShotDefinition(
            slug=item["slug"],
            source_filename=item.get("sourceFilename"),
            source_index=item.get("sourceIndex"),
        )
        for item in data["shots"]
    )

    device_configs = {
        name: DeviceConfig(
            source_root=resolve_from_root(project_root, Path(item["sourceRoot"])),
            source_filename_prefix=item.get("sourceFilenamePrefix", ""),
            source_glob=item.get("sourceGlob"),
            output_root=resolve_from_root(project_root, Path(item["outputRoot"])),
            output_subdir=item.get("outputSubdir"),
            compose_device=item["composeDevice"],
            target_size=(item["targetSize"][0], item["targetSize"][1]),
        )
        for name, item in data["devices"].items()
    }

    locale_configs = []
    for locale_item in data["locales"]:
        copies = locale_item["copy"]
        source_filenames = locale_item.get("sourceFilenames", {})
        locale_shots = []
        for shot in shots:
            shot_copy = copies[shot.slug]
            locale_shots.append(
                ShotCopy(
                    slug=shot.slug,
                    source_filename=source_filenames.get(
                        shot.slug,
                        shot.source_filename,
                    ),
                    source_index=shot.source_index,
                    verb=shot_copy["verb"],
                    desc=shot_copy["desc"],
                )
            )
        locale_configs.append(
            LocaleConfig(
                output_locale=locale_item["outputLocale"],
                source_locale=locale_item.get("sourceLocale", locale_item["outputLocale"]),
                compose_locale=locale_item.get("composeLocale", "auto"),
                shots=tuple(locale_shots),
            )
        )

    return AppConfig(
        brand_color=data["brandColor"],
        locale_configs=tuple(locale_configs),
        device_configs=device_configs,
    )


def selected_locale_configs(
    locale_configs: tuple[LocaleConfig, ...], selected_locales: set[str] | None
) -> tuple[LocaleConfig, ...]:
    if not selected_locales:
        return locale_configs
    return tuple(config for config in locale_configs if config.output_locale in selected_locales)


def selected_devices(
    device_configs: dict[str, DeviceConfig], selected: list[str] | None
) -> tuple[DeviceConfig, ...]:
    if not selected:
        return tuple(device_configs.values())
    return tuple(device_configs[name] for name in dict.fromkeys(selected))


def validate_selection(args: argparse.Namespace, app_config: AppConfig) -> None:
    if args.device:
        unknown_devices = sorted(set(args.device) - set(app_config.device_configs))
        if unknown_devices:
            raise SystemExit(f"Unknown device(s): {', '.join(unknown_devices)}")

    if args.locale:
        known_locales = {config.output_locale for config in app_config.locale_configs}
        unknown_locales = sorted(set(args.locale) - known_locales)
        if unknown_locales:
            raise SystemExit(f"Unknown locale(s): {', '.join(unknown_locales)}")


def remove_ds_store_files(base_dir: Path) -> None:
    if not base_dir.exists():
        return
    for ds_store_path in base_dir.rglob(".DS_Store"):
        ds_store_path.unlink(missing_ok=True)


def ensure_skill_assets_available() -> None:
    required_paths = (
        SKILL_DIR / "compose.mjs",
        SKILL_DIR / "web" / "renderer.html",
    )
    missing = [str(path) for path in required_paths if not path.exists()]
    if missing:
        missing_text = "\n".join(f"- {path}" for path in missing)
        raise FileNotFoundError(f"Missing required skill assets:\n{missing_text}")


def compose_output_png(
    screenshot_path: Path,
    output_path: Path,
    verb: str,
    desc: str,
    compose_locale: str,
    compose_device: str,
    brand_color: str,
) -> None:
    command = [
        "node",
        str(SKILL_DIR / "compose.mjs"),
        "--bg",
        brand_color,
        "--verb",
        verb,
        "--desc",
        desc,
        "--screenshot",
        str(screenshot_path),
        "--output",
        str(output_path),
        "--device",
        compose_device,
        "--locale",
        compose_locale,
    ]
    subprocess.run(command, check=True, cwd=SKILL_DIR)


def save_output_png(
    source_png: Path,
    output_png: Path,
    target_size: tuple[int, int],
) -> None:
    output_png.parent.mkdir(parents=True, exist_ok=True)
    with Image.open(source_png) as image:
        image = image.convert("RGB")
        if image.size != target_size:
            image = image.resize(target_size, Image.Resampling.LANCZOS)
        image.save(output_png, format="PNG", optimize=True, compress_level=9)

    pngquant_path = shutil.which("pngquant")
    if not pngquant_path:
        raise FileNotFoundError("pngquant is required but was not found")

    subprocess.run(
        [
            pngquant_path,
            "--force",
            "--strip",
            "--skip-if-larger",
            f"--quality={PNGQUANT_QUALITY}",
            f"--speed={PNGQUANT_SPEED}",
            "--ext",
            ".png",
            str(output_png),
        ],
        check=True,
    )


def list_source_files(device: DeviceConfig, locale: LocaleConfig) -> tuple[Path, ...]:
    if not device.source_glob:
        return ()
    locale_dir = device.source_root / locale.source_locale
    matches = tuple(
        sorted(path for path in locale_dir.glob(device.source_glob) if path.is_file())
    )
    if not matches:
        raise FileNotFoundError(
            f"No source screenshots matched '{device.source_glob}' in {locale_dir}"
        )
    return matches


def resolve_screenshot_path(device: DeviceConfig, locale: LocaleConfig, shot: ShotCopy) -> Path:
    if shot.source_index is not None:
        source_files = list_source_files(device, locale)
        if shot.source_index < 0 or shot.source_index >= len(source_files):
            raise IndexError(
                f"Shot index {shot.source_index} is out of range for {locale.output_locale} "
                f"({len(source_files)} matched source files)"
            )
        return source_files[shot.source_index]

    if shot.source_filename is None:
        raise ValueError(
            f"Shot '{shot.slug}' needs either sourceFilename or sourceIndex"
        )

    return (
        device.source_root
        / locale.source_locale
        / f"{device.source_filename_prefix}{shot.source_filename}"
    )


def build_render_tasks(
    devices: tuple[DeviceConfig, ...],
    locale_configs: tuple[LocaleConfig, ...],
    skip_existing: bool,
    project_root: Path,
) -> tuple[RenderTask, ...]:
    tasks: list[RenderTask] = []

    for device in devices:
        device.output_root.mkdir(parents=True, exist_ok=True)
        for locale in locale_configs:
            locale_output_dir = device.output_root / locale.output_locale
            if device.output_subdir:
                locale_output_dir = locale_output_dir / device.output_subdir
            locale_output_dir.mkdir(parents=True, exist_ok=True)

            for shot in locale.shots:
                screenshot_path = resolve_screenshot_path(device, locale, shot)
                if not screenshot_path.exists():
                    raise FileNotFoundError(f"Missing screenshot source: {screenshot_path}")

                output_path = locale_output_dir / f"{shot.slug}.png"
                if skip_existing and output_path.exists():
                    print(f"skip {display_path(output_path, project_root)}")
                    continue

                tasks.append(
                    RenderTask(
                        device=device,
                        locale=locale,
                        shot=shot,
                        output_path=output_path,
                    )
                )

    return tuple(tasks)


def render_task(
    task: RenderTask,
    brand_color: str,
    temp_root: Path,
) -> Path:
    device_temp_name = task.device.output_subdir or task.device.output_root.name
    temp_png_path = (
        temp_root
        / f"{device_temp_name}-{task.locale.output_locale}-{task.shot.slug}.png"
    )
    screenshot_path = resolve_screenshot_path(task.device, task.locale, task.shot)

    compose_output_png(
        screenshot_path=screenshot_path,
        output_path=temp_png_path,
        verb=task.shot.verb,
        desc=task.shot.desc,
        compose_locale=task.locale.compose_locale,
        compose_device=task.device.compose_device,
        brand_color=brand_color,
    )
    save_output_png(
        temp_png_path,
        task.output_path,
        task.device.target_size,
    )
    return task.output_path


def generate_device_outputs(
    devices: tuple[DeviceConfig, ...],
    locale_configs: tuple[LocaleConfig, ...],
    skip_existing: bool,
    brand_color: str,
    jobs: int,
    project_root: Path,
) -> None:
    tasks = build_render_tasks(
        devices=devices,
        locale_configs=locale_configs,
        skip_existing=skip_existing,
        project_root=project_root,
    )
    if not tasks:
        return

    with tempfile.TemporaryDirectory(prefix="aso-device-screenshots-") as temp_dir:
        temp_root = Path(temp_dir)
        max_workers = max(1, min(jobs, len(tasks)))
        with ThreadPoolExecutor(max_workers=max_workers) as executor:
            futures = {
                executor.submit(
                    render_task,
                    task,
                    brand_color,
                    temp_root,
                ): task
                for task in tasks
            }
            for future in as_completed(futures):
                output_path = future.result()
                print(f"saved {display_path(output_path, project_root)}")

    for device in devices:
        remove_ds_store_files(device.output_root)


def main() -> None:
    args = parse_args()
    ensure_skill_assets_available()

    project_root = args.project_root.resolve()
    config_path = resolve_from_root(project_root, args.config)

    app_config = load_config(config_path, project_root)
    validate_selection(args, app_config)

    locale_configs = selected_locale_configs(
        app_config.locale_configs, set(args.locale) if args.locale else None
    )
    devices = selected_devices(app_config.device_configs, args.device)

    generate_device_outputs(
        devices=devices,
        locale_configs=locale_configs,
        skip_existing=args.skip_existing,
        brand_color=app_config.brand_color,
        jobs=args.jobs,
        project_root=project_root,
    )

    print("✓ Localized ASO scaffold screenshots generated.")


if __name__ == "__main__":
    main()
