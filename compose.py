#!/usr/bin/env python3
"""
App Store Screenshot Composer
Composites headline text, device frame template, and app screenshot
into a deterministic App Store Connect scaffold.

Supports multiple device profiles so the same script can generate both
iPhone and iPad scaffolds.
"""

import argparse
import os
from PIL import Image, ImageDraw, ImageFont, ImageChops

PROFILES = {
    "iphone-67": {
        "canvas_w": 1290,
        "canvas_h": 2796,
        "device_w": 1030,
        "device_h": 2800,
        "device_y": 720,
        "bezel": 15,
        "screen_corner_r": 62,
        "device_corner_r": 77,
        "text_top": 200,
        "verb_size_max": 256,
        "verb_size_min": 150,
        "desc_size": 124,
        "verb_desc_gap": 20,
        "desc_line_gap": 24,
        "max_text_ratio": 0.92,
        "max_verb_ratio": 0.92,
        "dynamic_island": True,
    },
    "ipad-13": {
        "canvas_w": 2064,
        "canvas_h": 2752,
        "device_w": 1760,
        "device_h": 2332,
        "device_y": 600,
        "bezel": 22,
        "screen_corner_r": 40,
        "device_corner_r": 64,
        "text_top": 150,
        "verb_size_max": 320,
        "verb_size_min": 180,
        "desc_size": 150,
        "verb_desc_gap": 24,
        "desc_line_gap": 28,
        "max_text_ratio": 0.84,
        "max_verb_ratio": 0.86,
        "dynamic_island": False,
    },
}

FONT_PATH = "/Library/Fonts/SF-Pro-Display-Black.otf"
FRAME_PATH = os.path.join(os.path.dirname(__file__), "assets", "device_frame.png")


def hex_to_rgb(h):
    h = h.lstrip("#")
    return tuple(int(h[i : i + 2], 16) for i in (0, 2, 4))


def word_wrap(draw, text, font, max_w):
    words = text.split()
    lines, cur = [], ""
    for w in words:
        test = f"{cur} {w}".strip()
        if draw.textlength(test, font=font) <= max_w:
            cur = test
        else:
            if cur:
                lines.append(cur)
            cur = w
    if cur:
        lines.append(cur)
    return lines


def fit_font(text, max_w, size_max, size_min):
    """Return the largest font size where text fits within max_w."""
    dummy = ImageDraw.Draw(Image.new("RGBA", (1, 1)))
    for size in range(size_max, size_min - 1, -4):
        font = ImageFont.truetype(FONT_PATH, size)
        bbox = dummy.textbbox((0, 0), text, font=font)
        if (bbox[2] - bbox[0]) <= max_w:
            return font
    return ImageFont.truetype(FONT_PATH, size_min)


def draw_centered(draw, y, text, font, canvas_w, desc_line_gap, max_w=None):
    lines = word_wrap(draw, text, font, max_w) if max_w else [text]
    for line in lines:
        bbox = draw.textbbox((0, 0), line, font=font)
        h = bbox[3] - bbox[1]
        # Use anchor="mt" (middle-top) for pixel-perfect horizontal centering
        # Adjust y by bbox[1] offset so text top aligns with intended position
        draw.text(
            (canvas_w // 2, y - bbox[1]), line, fill="white", font=font, anchor="mt"
        )
        y += h + desc_line_gap
    return y


def infer_profile(screenshot_path):
    shot = Image.open(screenshot_path)
    if shot.width >= 1800 and shot.height >= 2400:
        return "ipad-13"
    return "iphone-67"


def build_ipad_frame(profile):
    device_w = profile["device_w"]
    device_h = profile["device_h"]
    device_corner_r = profile["device_corner_r"]
    bezel = profile["bezel"]
    screen_corner_r = profile["screen_corner_r"]
    screen_w = device_w - 2 * bezel
    screen_h = device_h - 2 * bezel

    frame = Image.new("RGBA", (device_w, device_h), (0, 0, 0, 0))
    fd = ImageDraw.Draw(frame)
    fd.rounded_rectangle(
        [0, 0, device_w - 1, device_h - 1],
        radius=device_corner_r,
        fill=(34, 34, 36, 255),
    )
    fd.rounded_rectangle(
        [2, 2, device_w - 3, device_h - 3],
        radius=device_corner_r - 2,
        fill=(18, 18, 20, 255),
    )

    cutout = Image.new("L", (device_w, device_h), 255)
    ImageDraw.Draw(cutout).rounded_rectangle(
        [bezel, bezel, bezel + screen_w, bezel + screen_h],
        radius=screen_corner_r,
        fill=0,
    )
    frame.putalpha(ImageChops.multiply(frame.getchannel("A"), cutout))

    button_color = (22, 22, 24, 255)
    fd.rounded_rectangle(
        [device_w, 420, device_w + 5, 620], radius=3, fill=button_color
    )
    fd.rounded_rectangle([-5, 360, 0, 500], radius=3, fill=button_color)
    fd.rounded_rectangle([-5, 540, 0, 680], radius=3, fill=button_color)
    return frame


def load_frame(profile_name, profile):
    if profile_name == "iphone-67" and os.path.exists(FRAME_PATH):
        return Image.open(FRAME_PATH).convert("RGBA")
    return build_ipad_frame(profile)


def compose(bg_hex, verb, desc, screenshot_path, output_path, profile_name):
    if profile_name == "auto":
        profile_name = infer_profile(screenshot_path)
    profile = PROFILES[profile_name]

    canvas_w = int(profile["canvas_w"])
    canvas_h = int(profile["canvas_h"])
    device_w = int(profile["device_w"])
    device_h = int(profile["device_h"])
    bezel = int(profile["bezel"])
    screen_w = int(device_w - 2 * bezel)
    screen_corner_r = int(profile["screen_corner_r"])
    max_text_w = int(canvas_w * profile["max_text_ratio"])
    max_verb_w = int(canvas_w * profile["max_verb_ratio"])

    bg = hex_to_rgb(bg_hex)

    # ── 1. Canvas ───────────────────────────────────────────────────
    canvas = Image.new("RGBA", (canvas_w, canvas_h), (*bg, 255))
    draw = ImageDraw.Draw(canvas)

    # ── 2. Measure text, then center between top of canvas & device ─
    verb_font = fit_font(
        verb.upper(),
        max_verb_w,
        profile["verb_size_max"],
        profile["verb_size_min"],
    )
    desc_font = ImageFont.truetype(FONT_PATH, int(profile["desc_size"]))

    # Measure total text block height (dry run at y=0)
    dummy = ImageDraw.Draw(Image.new("RGBA", (1, 1)))
    m_y = 0
    m_y = draw_centered(
        dummy, m_y, verb.upper(), verb_font, canvas_w, profile["desc_line_gap"]
    )
    m_y += profile["verb_desc_gap"]
    draw_centered(
        dummy,
        m_y,
        desc.upper(),
        desc_font,
        canvas_w,
        profile["desc_line_gap"],
        max_w=max_text_w,
    )

    device_y = int(profile["device_y"])
    text_top = int(profile["text_top"])

    # Draw text at centered position
    y = text_top
    y = draw_centered(
        draw, y, verb.upper(), verb_font, canvas_w, profile["desc_line_gap"]
    )
    y += profile["verb_desc_gap"]
    draw_centered(
        draw,
        y,
        desc.upper(),
        desc_font,
        canvas_w,
        profile["desc_line_gap"],
        max_w=max_text_w,
    )
    device_x = (canvas_w - device_w) // 2
    screen_x = device_x + bezel
    screen_y = device_y + bezel

    # ── 4. Screenshot into screen area ──────────────────────────────
    shot = Image.open(screenshot_path).convert("RGBA")

    # Scale to fill screen width
    scale = screen_w / shot.width
    sc_w = screen_w
    sc_h = int(shot.height * scale)
    shot = shot.resize((sc_w, sc_h), Image.Resampling.LANCZOS)

    # Screen extends to bottom of canvas + overflow
    screen_h = canvas_h - screen_y + 500

    # Screen mask (rounded rect)
    scr_mask = Image.new("L", canvas.size, 0)
    ImageDraw.Draw(scr_mask).rounded_rectangle(
        [screen_x, screen_y, screen_x + screen_w, screen_y + screen_h],
        radius=screen_corner_r,
        fill=255,
    )

    # Black screen bg + screenshot on top
    scr_layer = Image.new("RGBA", canvas.size, (0, 0, 0, 0))
    ImageDraw.Draw(scr_layer).rounded_rectangle(
        [screen_x, screen_y, screen_x + screen_w, screen_y + screen_h],
        radius=screen_corner_r,
        fill=(0, 0, 0, 255),
    )
    scr_layer.paste(shot, (screen_x, screen_y))
    scr_layer.putalpha(scr_mask)

    canvas = Image.alpha_composite(canvas, scr_layer)

    # ── 6. Device frame template ───────────────────────────────────
    frame_template = load_frame(profile_name, profile)

    # Place frame template onto canvas-sized layer at calculated position
    frame_layer = Image.new("RGBA", canvas.size, (0, 0, 0, 0))
    frame_layer.paste(frame_template, (device_x, device_y))
    canvas = Image.alpha_composite(canvas, frame_layer)

    # ── 7. Save ────────────────────────────────────────────────────
    canvas.convert("RGB").save(output_path, "PNG")
    print(f"✓ {output_path} ({canvas_w}×{canvas_h}) [{profile_name}]")


def main():
    p = argparse.ArgumentParser(description="Compose App Store screenshot")
    p.add_argument("--bg", required=True, help="Background hex colour (#E31837)")
    p.add_argument("--verb", required=True, help="Action verb (TRACK)")
    p.add_argument(
        "--desc", required=True, help="Benefit descriptor (TRADING CARD PRICES)"
    )
    p.add_argument("--screenshot", required=True, help="Simulator screenshot path")
    p.add_argument("--output", required=True, help="Output file path")
    p.add_argument(
        "--device",
        default="auto",
        choices=["auto", *PROFILES.keys()],
        help="Target device profile (default: infer from screenshot)",
    )
    args = p.parse_args()

    compose(args.bg, args.verb, args.desc, args.screenshot, args.output, args.device)


if __name__ == "__main__":
    main()
