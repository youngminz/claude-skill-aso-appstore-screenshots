#!/usr/bin/env bash
set -euo pipefail

appearance="light"
status_time="9:41"
simulator_id=""
clear_status_bar="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --appearance)
      appearance="$2"
      shift 2
      ;;
    --time)
      status_time="$2"
      shift 2
      ;;
    --simulator-id)
      simulator_id="$2"
      shift 2
      ;;
    --clear)
      clear_status_bar="true"
      shift
      ;;
    -h|--help)
      cat <<'EOF'
Usage:
  prepare_simulator_screenshots.sh [--appearance light|dark] [--time 9:41]
  prepare_simulator_screenshots.sh --clear

Prepares the first booted iPhone/iPad simulator for App Store screenshots by
setting a clean status bar and appearance.
EOF
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

if [[ -z "$simulator_id" ]]; then
  simulator_id="$(
    xcrun simctl list devices booted |
      grep -E 'iPhone|iPad' |
      head -1 |
      grep -oE '[A-Z0-9]{8}-[A-Z0-9]{4}-[A-Z0-9]{4}-[A-Z0-9]{4}-[A-Z0-9]{12}' || true
  )"
fi

if [[ -z "$simulator_id" ]]; then
  echo "No running iOS simulator found. Start an iPhone or iPad simulator first." >&2
  exit 1
fi

if [[ "$clear_status_bar" == "true" ]]; then
  xcrun simctl status_bar "$simulator_id" clear
  echo "Cleared simulator status bar override: $simulator_id"
  exit 0
fi

xcrun simctl status_bar "$simulator_id" override \
  --time "$status_time" \
  --batteryState charged \
  --batteryLevel 100 \
  --cellularMode active \
  --cellularBars 4 \
  --wifiBars 3 \
  --operatorName " "

xcrun simctl ui "$simulator_id" appearance "$appearance"

echo "Simulator is ready for screenshots: $simulator_id"
