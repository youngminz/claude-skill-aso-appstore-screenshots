#!/usr/bin/env node

import fs from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";
import { imageSizeFromFile } from "image-size/fromFile";
import { chromium } from "playwright";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const rootDir = __dirname;
const homeDir = process.env.HOME ?? "";

const profiles = {
  "iphone-67": {
    canvasWidth: 1290,
    canvasHeight: 2796,
    deviceWidth: 1030,
    deviceHeight: 2800,
    deviceY: 720,
    bezel: 15,
    screenCornerRadius: 62,
    deviceCornerRadius: 77,
    textTop: 200,
    verbSizeMax: 256,
    verbSizeMin: 150,
    descSize: 124,
    verbDescGap: 20,
    descLineGap: 24,
    maxTextWidth: Math.floor(1290 * 0.92),
    maxVerbWidth: Math.floor(1290 * 0.92),
    framePath: path.join(rootDir, "assets", "device_frame.png"),
  },
  "ipad-13": {
    canvasWidth: 2064,
    canvasHeight: 2752,
    deviceWidth: 1760,
    deviceHeight: 2332,
    deviceY: 600,
    bezel: 22,
    screenCornerRadius: 40,
    deviceCornerRadius: 64,
    textTop: 150,
    verbSizeMax: 320,
    verbSizeMin: 180,
    descSize: 150,
    verbDescGap: 24,
    descLineGap: 28,
    maxTextWidth: Math.floor(2064 * 0.84),
    maxVerbWidth: Math.floor(2064 * 0.86),
    framePath: null,
  },
};

const typographyProfiles = {
  latin: {
    fontFamily: '"SF Pro Display Black", sans-serif',
    fontLoadFamily: '"SF Pro Display Black"',
    fontWeight: 900,
    direction: "ltr",
    lang: "en",
    textTransform: "uppercase",
    lineHeightRatio: 0.733,
    subtitleGapOffset: 60,
  },
  korean: {
    fontFamily: '"Pretendard", "Apple SD Gothic Neo", "AppleGothic", sans-serif',
    fontLoadFamily: '"Pretendard"',
    fontWeight: 800,
    direction: "ltr",
    lang: "ko",
    textTransform: "none",
    lineHeightRatio: 0.9,
    subtitleGapOffset: 52,
  },
  arabic: {
    fontFamily: '"SF Arabic", "Geeza Pro", "Baghdad", "Damascus", sans-serif',
    fontLoadFamily: '"SF Arabic"',
    fontWeight: 700,
    direction: "rtl",
    lang: "ar",
    textTransform: "none",
    lineHeightRatio: 1,
    subtitleGapOffset: 56,
  },
};

const bundledFontCandidates = {
  latin: [
    {
      family: "SF Pro Display Black",
      fontWeight: "900",
      path: "/Library/Fonts/SF-Pro-Display-Black.otf",
    },
  ],
  korean: [
    {
      family: "Pretendard",
      fontWeight: "45 920",
      path: path.join(homeDir, "Library", "Fonts", "PretendardVariable.ttf"),
    },
    {
      family: "Pretendard",
      fontWeight: "800",
      path: path.join(homeDir, "Library", "Fonts", "Pretendard-ExtraBold.otf"),
    },
  ],
  arabic: [
    {
      family: "SF Arabic",
      fontWeight: "700",
      path: "/System/Library/Fonts/SFArabic.ttf",
    },
  ],
};

function usage() {
  return [
    "Usage:",
    "  node compose.mjs \\",
    "    --bg '#E31837' \\",
    "    --verb 'Track' \\",
    "    --desc 'Trading Card Prices' \\",
    "    --screenshot '/path/to/screenshot.png' \\",
    "    --output '/path/to/output.png' \\",
    "    [--device auto|iphone-67|ipad-13]",
  ].join("\n");
}

function parseArgs(argv) {
  const values = {};
  for (let index = 0; index < argv.length; index += 1) {
    const key = argv[index];
    if (!key.startsWith("--")) {
      throw new Error(usage());
    }
    const value = argv[index + 1];
    if (value == null) {
      throw new Error(`Missing value for ${key}\n\n${usage()}`);
    }
    values[key.slice(2)] = value;
    index += 1;
  }

  for (const key of ["bg", "verb", "desc", "screenshot", "output"]) {
    if (!values[key]) {
      throw new Error(usage());
    }
  }

  const device = values.device ?? "auto";
  if (!["auto", ...Object.keys(profiles)].includes(device)) {
    throw new Error(`Unsupported device '${device}'\n\n${usage()}`);
  }

  return {
    background: values.bg,
    verb: values.verb,
    desc: values.desc,
    screenshotPath: path.resolve(values.screenshot),
    outputPath: path.resolve(values.output),
    device,
  };
}

async function fileToDataUrl(filePath) {
  const ext = path.extname(filePath).toLowerCase();
  const mimeType = {
    ".png": "image/png",
    ".jpg": "image/jpeg",
    ".jpeg": "image/jpeg",
    ".webp": "image/webp",
    ".otf": "font/otf",
    ".ttf": "font/ttf",
  }[ext];

  if (!mimeType) {
    throw new Error(`Unsupported file type for ${filePath}`);
  }

  const buffer = await fs.readFile(filePath);
  return `data:${mimeType};base64,${buffer.toString("base64")}`;
}

async function inferProfileName(screenshotPath) {
  const { width = 0, height = 0 } = await imageSizeFromFile(screenshotPath);
  if (width >= 1800 && height >= 2400) {
    return "ipad-13";
  }
  return "iphone-67";
}

async function resolveFirstExistingFont(candidates) {
  for (const candidate of candidates) {
    try {
      await fs.access(candidate.path);
      return candidate;
    } catch {
      continue;
    }
  }
  return null;
}

function fontFormat(filePath) {
  const ext = path.extname(filePath).toLowerCase();
  return {
    ".otf": "opentype",
    ".ttf": "truetype",
  }[ext];
}

async function buildFontFaceCss() {
  const fontFaceBlocks = [];

  for (const candidates of Object.values(bundledFontCandidates)) {
    const font = await resolveFirstExistingFont(candidates);
    if (!font) {
      continue;
    }

    const dataUrl = await fileToDataUrl(font.path);
    fontFaceBlocks.push(`@font-face {
        font-family: "${font.family}";
        src: url("${dataUrl}") format("${fontFormat(font.path)}");
        font-weight: ${font.fontWeight};
        font-style: normal;
      }`);
  }

  return fontFaceBlocks.join("\n\n");
}

async function buildHtml(config) {
  const templatePath = path.join(rootDir, "web", "renderer.html");
  const template = await fs.readFile(templatePath, "utf8");
  const fontFaceCss = await buildFontFaceCss();

  return template
    .replace("__FONT_FACE_CSS__", fontFaceCss)
    .replace("__RENDER_CONFIG_JSON__", JSON.stringify(config));
}

function detectScript(text) {
  if (/[가-힣ㄱ-ㅎㅏ-ㅣ]/u.test(text)) {
    return "korean";
  }
  if (/[\u0600-\u06FF\u0750-\u077F\u08A0-\u08FF]/u.test(text)) {
    return "arabic";
  }
  return "latin";
}

async function main() {
  const options = parseArgs(process.argv.slice(2));
  const profileName =
    options.device === "auto"
      ? await inferProfileName(options.screenshotPath)
      : options.device;
  const profile = profiles[profileName];
  const screenshotDataUrl = await fileToDataUrl(options.screenshotPath);
  const frameDataUrl = profile.framePath ? await fileToDataUrl(profile.framePath) : null;
  const script = detectScript(`${options.verb} ${options.desc}`);
  const typography = typographyProfiles[script];

  const renderConfig = {
    background: options.background,
    verb: options.verb,
    desc: options.desc,
    profile,
    typography,
    screenshotDataUrl,
    frameDataUrl,
  };

  const html = await buildHtml(renderConfig);
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage({
    viewport: {
      width: profile.canvasWidth,
      height: profile.canvasHeight,
    },
    deviceScaleFactor: 1,
  });

  page.on("pageerror", (error) => {
    process.stderr.write(`pageerror: ${error.message}\n`);
  });

  await page.setContent(html, { waitUntil: "load" });
  await page.waitForFunction(
    () => document.body.dataset.renderReady === "true" || !!document.body.dataset.renderError,
  );
  const renderError = await page.evaluate(() => document.body.dataset.renderError ?? null);
  if (renderError) {
    throw new Error(`Renderer failed: ${renderError}`);
  }

  await page.screenshot({
    path: options.outputPath,
    type: "png",
  });

  await browser.close();
  process.stdout.write(
    `✓ ${options.outputPath} (${profile.canvasWidth}×${profile.canvasHeight}) [${profileName}]\n`,
  );
}

main().catch((error) => {
  process.stderr.write(`${error.message}\n`);
  process.exit(1);
});
