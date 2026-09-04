#!/usr/bin/env bash
set -euo pipefail

fail() {
  printf 'Kodex Playwright preparation failed: %s\n' "$*" >&2
  exit 1
}

usage() {
  printf '%s\n' "Usage: $0 --frontend-directory <absolute-path>" >&2
}

frontend_directory=""
while (($# > 0)); do
  case "$1" in
    --frontend-directory) frontend_directory=${2:-}; shift 2 ;;
    --help) usage; exit 0 ;;
    *) usage; fail "unsupported argument: $1" ;;
  esac
done

[[ "$frontend_directory" == /* && -d "$frontend_directory" ]] ||
  fail 'frontend directory is invalid'
playwright_cli="$frontend_directory/node_modules/.bin/playwright"
[[ -x "$playwright_cli" ]] || fail 'project-local Playwright CLI is absent'

browser_path=$(
  cd -- "$frontend_directory"
  node --input-type=module <<'JS'
import { chromium } from 'playwright';
process.stdout.write(chromium.executablePath());
JS
)
[[ "$browser_path" == "$HOME"/.cache/ms-playwright/* ]] ||
  fail 'Playwright browser path is outside the current user cache'

if [[ ! -x "$browser_path" ]]; then
  sudo -n "$playwright_cli" install-deps chromium
  "$playwright_cli" install chromium
fi
[[ -x "$browser_path" ]] || fail 'Chromium executable is absent after installation'

(
  cd -- "$frontend_directory"
  node --input-type=module <<'JS'
import { chromium } from 'playwright';
const browser = await chromium.launch({ headless: true });
await browser.close();
JS
) || fail 'Chromium launch readback failed'

printf 'Kodex Playwright Chromium is ready\n'
