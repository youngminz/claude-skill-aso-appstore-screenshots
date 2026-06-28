# AGENTS.md

This file provides guidance to Codex (Codex.ai/code) when working with code in this repository.

## What This Is

A Codex skill (`aso-appstore-screenshots`) that guides users through creating high-converting App Store screenshots. It is invoked via the `/aso-appstore-screenshots` slash command from within a user's app project.

## Architecture

Four files + one asset make up the skill:

- **SKILL.md** — The skill prompt. Defines a multi-phase workflow: Benefit Discovery → Screenshot Pairing → Generation. Uses Codex's memory system to persist state across conversations so users can resume mid-workflow. Generation first creates a deterministic scaffold via `compose.mjs`, then sends it to Nano Banana Pro for AI enhancement.
- **compose.mjs** — The primary screenshot renderer. It uses Playwright + HTML/CSS to deterministically render App Store screenshot scaffolds from a background hex colour, localized headline lines, locale, and simulator screenshot path. The device shell is rendered directly in HTML/CSS.
- **generate_frame.py** — Legacy utility that generates a standalone device frame PNG (`assets/device_frame.png`) kept as a reference asset.
- **showcase.py** — Generates a showcase image showing up to 3 final screenshots side-by-side with an optional GitHub link at the bottom. Used as the final step after all screenshots are approved.
- **assets/device_frame.png** — Legacy reference frame asset retained for compatibility. It is no longer used by `compose.mjs`.

## Running compose.mjs

```bash
# Requires: npm install
# Requires: npx playwright install chromium
# Requires: SF Pro Display Black font at /Library/Fonts/SF-Pro-Display-Black.otf
# Optional for Korean: Pretendard in ~/Library/Fonts
# Japanese, Chinese, and Arabic use system fonts on macOS

node compose.mjs \
  --bg "#E31837" \
  --line1 "TRACK" \
  --line2 "TRADING CARD PRICES" \
  --screenshot path/to/simulator.png \
  --output output.png \
  --locale auto
```

## Key Design Decisions

- **Two-stage generation**: `compose.mjs` creates a deterministic scaffold first (text + CSS device shell + screenshot), then Nano Banana Pro enhances it. This avoids the inconsistencies of generating from scratch.
- **compose.mjs outputs exact App Store Connect dimensions** (1290×2796 for iPhone 6.7") — no post-processing crop needed.
- **Device shell is CSS-rendered** — iPhone and iPad hardware outlines are drawn directly in `web/renderer.html`, so the scaffold no longer depends on a frame overlay image.
- **Line 1 text auto-sizes** — shrinks from 172px down to 100px to fit longer localized hooks (e.g. "TURN YOURSELF") within the canvas width.
- **Typography is locale-aware** — English uses SF Pro Display Black, Korean uses Pretendard, Japanese uses Hiragino Sans, Simplified Chinese uses PingFang SC, Traditional Chinese uses PingFang TC, and Arabic uses SF Arabic.
- **CJK subtitle layout is character-based** — Japanese and Chinese do not rely on whitespace wrapping, and subtitles can shrink slightly when 2 lines are still too wide.
- **Prefer explicit Chinese locale selection** — use `--locale zh-Hans` or `--locale zh-Hant` because Han-only copy is ambiguous under auto-detection.
- **SKILL.md always generates 3 versions in parallel** for each benefit so the user can pick the best one.
- **The crop/resize step in SKILL.md is mandatory** after every `generate_image` or `edit_image` call — raw Nano Banana output is never the correct dimensions for App Store Connect.
- **Memory is central to the workflow** — benefits, screenshot assessments, pairings, brand colour, and generation state are all persisted so users can resume across conversations.
