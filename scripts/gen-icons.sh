#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GATEWAY_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${GATEWAY_ROOT}/.." && pwd)"
ASSETS="${GATEWAY_ROOT}/assets"

SOURCE="${1:-${REPO_ROOT}/icon.ico}"

mkdir -p "${ASSETS}"

if [[ ! -f "${SOURCE}" ]]; then
  echo "icon source not found: ${SOURCE}"
  exit 1
fi

cp -f "${SOURCE}" "${ASSETS}/icon.ico"

if [[ -f "${ASSETS}/icon.png" ]]; then
  echo "icon.png already exists: ${ASSETS}/icon.png"
  exit 0
fi

if command -v magick >/dev/null 2>&1; then
  magick "${SOURCE}" "${ASSETS}/icon.png"
  echo "icon.png generated with ImageMagick"
  exit 0
fi

if command -v convert >/dev/null 2>&1; then
  convert "${SOURCE}" "${ASSETS}/icon.png"
  echo "icon.png generated with ImageMagick convert"
  exit 0
fi

echo "icon.png not generated. Install ImageMagick or provide icon.png manually."
exit 1
