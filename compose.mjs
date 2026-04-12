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
    shellStyle: "iphone",
    deviceWidth: 1030,
    deviceHeight: 2800,
    deviceY: 720,
    bezel: 15,
    screenCornerRadius: 129,
    deviceCornerRadius: 144,
    textTop: 200,
    verbSizeMax: 256,
    verbSizeMin: 150,
    descSize: 124,
    verbDescGap: 20,
    descLineGap: 24,
    maxTextWidth: Math.floor(1290 * 0.92),
    maxVerbWidth: Math.floor(1290 * 0.92),
  },
  "ipad-13": {
    canvasWidth: 2064,
    canvasHeight: 2752,
    shellStyle: "ipad",
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
  },
};

const localeTypographyMap = {
  en: "latin",
  ko: "korean",
  ja: "japanese",
  ar: "arabic",
  he: "hebrew",
  hi: "hindi",
  th: "thai",
  "zh-Hans": "simplifiedChinese",
  "zh-Hant": "traditionalChinese",
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
  japanese: {
    fontFamily: '"Hiragino Sans", "Hiragino Kaku Gothic ProN", sans-serif',
    fontLoadFamily: '"Hiragino Sans"',
    fontWeight: 700,
    direction: "ltr",
    lang: "ja",
    textTransform: "none",
    wrapMode: "cjk",
    maxSubtitleLines: 2,
    descSizeMinScale: 0.7,
    lineHeightRatio: 0.92,
    subtitleGapOffset: 52,
  },
  simplifiedChinese: {
    fontFamily: '"PingFang SC", "Hiragino Sans GB", sans-serif',
    fontLoadFamily: '"PingFang SC"',
    fontWeight: 700,
    direction: "ltr",
    lang: "zh-Hans",
    textTransform: "none",
    wrapMode: "cjk",
    maxSubtitleLines: 2,
    descSizeMinScale: 0.7,
    lineHeightRatio: 0.92,
    subtitleGapOffset: 52,
  },
  traditionalChinese: {
    fontFamily: '"PingFang TC", "PingFang HK", "Hiragino Sans GB", sans-serif',
    fontLoadFamily: '"PingFang TC"',
    fontWeight: 700,
    direction: "ltr",
    lang: "zh-Hant",
    textTransform: "none",
    wrapMode: "cjk",
    maxSubtitleLines: 2,
    descSizeMinScale: 0.7,
    lineHeightRatio: 0.92,
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
  hebrew: {
    fontFamily: '"SF Hebrew", "Arial Hebrew", sans-serif',
    fontLoadFamily: '"SF Hebrew"',
    fontWeight: 700,
    direction: "rtl",
    lang: "he",
    textTransform: "none",
    lineHeightRatio: 1,
    subtitleGapOffset: 56,
  },
  hindi: {
    fontFamily: '"Kohinoor Devanagari", ".SF Devanagari", "Arial Unicode MS", sans-serif',
    fontLoadFamily: '"Kohinoor Devanagari"',
    fontWeight: 700,
    direction: "ltr",
    lang: "hi",
    textTransform: "none",
    lineHeightRatio: 1,
    subtitleGapOffset: 56,
  },
  thai: {
    fontFamily: '"Sukhumvit Set", "Thonburi", "Arial Unicode MS", sans-serif',
    fontLoadFamily: '"Sukhumvit Set"',
    fontWeight: 700,
    direction: "ltr",
    lang: "th",
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
  hebrew: [
    {
      family: "SF Hebrew",
      fontWeight: "700",
      path: "/System/Library/Fonts/SFHebrew.ttf",
    },
  ],
  hindi: [],
  thai: [],
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
    "    [--device auto|iphone-67|ipad-13] \\",
    "    [--locale auto|en|ko|ja|ar|he|hi|th|zh-Hans|zh-Hant]",
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

  const locale = values.locale ?? "auto";
  if (!["auto", ...Object.keys(localeTypographyMap)].includes(locale)) {
    throw new Error(`Unsupported locale '${locale}'\n\n${usage()}`);
  }

  return {
    background: values.bg,
    verb: values.verb,
    desc: values.desc,
    screenshotPath: path.resolve(values.screenshot),
    outputPath: path.resolve(values.output),
    device,
    locale,
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
  if (/[\u3040-\u30FF\u31F0-\u31FF\uFF66-\uFF9D]/u.test(text)) {
    return "japanese";
  }
  if (/[\u4E00-\u9FFF\u3400-\u4DBF]/u.test(text)) {
    return "simplifiedChinese";
  }
  if (/[\u0600-\u06FF\u0750-\u077F\u08A0-\u08FF]/u.test(text)) {
    return "arabic";
  }
  if (/[\u0590-\u05FF]/u.test(text)) {
    return "hebrew";
  }
  if (/[\u0900-\u097F]/u.test(text)) {
    return "hindi";
  }
  if (/[\u0E00-\u0E7F]/u.test(text)) {
    return "thai";
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
  const typographyKey =
    options.locale === "auto"
      ? detectScript(`${options.verb} ${options.desc}`)
      : localeTypographyMap[options.locale];
  const typography = typographyProfiles[typographyKey];

  const renderConfig = {
    background: options.background,
    verb: options.verb,
    desc: options.desc,
    profile,
    typography,
    screenshotDataUrl,
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
