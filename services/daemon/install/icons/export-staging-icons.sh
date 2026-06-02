#!/usr/bin/env bash
# export-staging-icons.sh — Reproducibly regenerate the staging tray icon assets from the
# canonical SVG master. Produces vaultmtg-staging.icns, vaultmtg-staging.ico, and the
# staging tray PNG (services/daemon/internal/tray/assets/staging_icon.png).
#
# Design spec: vault-mtg-docs/engineering/design/specs/daemon-staging-tray-icon-spec.md
# Ticket: vault-mtg-tickets#657
#
# Usage:
#   SVG_PATH=<path/to/logo-vaultmtg-app-icon.svg> bash export-staging-icons.sh
#
# Or, if vault-mtg-docs is cloned alongside vault-mtg:
#   bash export-staging-icons.sh   # uses default SVG_PATH below
#
# Requirements:
#   - rsvg-convert  (librsvg — brew install librsvg)
#   - iconutil      (macOS built-in, /usr/bin/iconutil)
#   - magick        (ImageMagick 7 — brew install imagemagick)
#   - python3       (macOS built-in — for pixel-art badge generation)
#   - Arial Black   (/System/Library/Fonts/Supplemental/Arial Black.ttf — macOS built-in)
#
# Outputs (relative to this script's directory):
#   vaultmtg-staging.icns                                macOS staging icon (full iconset 16→512@2x)
#   vaultmtg-staging.ico                                 Windows multi-res staging icon (16/32/48/256 px)
#   ../../internal/tray/assets/staging_icon.png          Staging tray icon (32×32 RGBA PNG)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../../../.." && pwd)"

# Default SVG location: vault-mtg-docs cloned alongside vault-mtg.
DOCS_ASSETS="${REPO_ROOT}/../vault-mtg-docs/engineering/design/rebranding/Ray Hamilton Engineering Design System/assets"
DEFAULT_SVG="${DOCS_ASSETS}/logo-vaultmtg-app-icon.svg"
SVG_PATH="${SVG_PATH:-${DEFAULT_SVG}}"

ARIAL_BLACK="/System/Library/Fonts/Supplemental/Arial Black.ttf"

if [[ ! -f "${SVG_PATH}" ]]; then
  echo "ERROR: SVG not found at: ${SVG_PATH}" >&2
  echo "Set SVG_PATH= to the canonical logo-vaultmtg-app-icon.svg." >&2
  exit 1
fi

if [[ ! -f "${ARIAL_BLACK}" ]]; then
  echo "ERROR: Arial Black not found at ${ARIAL_BLACK}" >&2
  echo "  This font is a macOS built-in at /System/Library/Fonts/Supplemental/" >&2
  exit 1
fi

# Verify required tools.
for tool in rsvg-convert iconutil magick python3; do
  if ! command -v "${tool}" &>/dev/null; then
    echo "ERROR: ${tool} not found in PATH." >&2
    echo "  rsvg-convert: brew install librsvg" >&2
    echo "  iconutil:     macOS built-in (/usr/bin/iconutil)" >&2
    echo "  magick:       brew install imagemagick" >&2
    exit 1
  fi
done

echo "[export-staging-icons] SVG source: ${SVG_PATH}"

TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

# ---------------------------------------------------------------------------
# Pixel-art S badge generator (for 16px and 18px — Prof YELLOW condition)
#
# At 16px, font-rendered letters produce anti-aliased smudge. This badge uses
# a hand-crafted pixel-art S with 2-pixel strokes, guaranteeing legibility on
# non-Retina (96 DPI) Windows displays per the design spec §Badge Design.
# ---------------------------------------------------------------------------
python3 - <<'PYEOF'
import struct, zlib, os

def write_png_rgba(filename, width, height, pixels):
    """Write minimal RGBA PNG — no external deps."""
    raw = b''
    for y in range(height):
        raw += b'\x00'  # filter type None per row
        for x in range(width):
            r, g, b, a = pixels[y][x]
            raw += bytes([r, g, b, a])
    compressed = zlib.compress(raw, 9)
    def chunk(name, data):
        c = name + data
        return struct.pack('>I', len(data)) + c + struct.pack('>I', zlib.crc32(c) & 0xffffffff)
    png = b'\x89PNG\r\n\x1a\n'
    png += chunk(b'IHDR', struct.pack('>IIBBBBB', width, height, 8, 6, 0, 0, 0))
    png += chunk(b'IDAT', compressed)
    png += chunk(b'IEND', b'')
    with open(filename, 'wb') as f:
        f.write(png)

W = (255, 255, 255, 255)   # white badge background
D = (13, 17, 23, 255)      # dark S letterform (#0D1117)

tmp = os.environ.get('TMP_DIR', '/tmp')

# 9x9 pixel-art S badge — used for 16px and 18px base icons.
#
#   . = white (badge background)
#   X = dark (#0D1117, S letterform)
#
#   . . . . . . . . .   top border
#   . . X X X X . . .   top horizontal bar (4px)
#   . X X . . . . . .   upper-left stroke (2px)
#   . X X . . . . . .   upper-left stroke (2px, continued)
#   . . X X X . . . .   middle crossbar (3px)
#   . . . . X X . . .   lower-right stroke (2px)
#   . . . . X X . . .   lower-right stroke (2px, continued)
#   . . X X X X . . .   bottom horizontal bar (4px)
#   . . . . . . . . .   bottom border
#
badge9 = [
    [W, W, W, W, W, W, W, W, W],
    [W, W, D, D, D, D, W, W, W],
    [W, D, D, W, W, W, W, W, W],
    [W, D, D, W, W, W, W, W, W],
    [W, W, D, D, D, W, W, W, W],
    [W, W, W, W, D, D, W, W, W],
    [W, W, W, W, D, D, W, W, W],
    [W, W, D, D, D, D, W, W, W],
    [W, W, W, W, W, W, W, W, W],
]
write_png_rgba(os.path.join(tmp, 'badge_9px_pixelart.png'), 9, 9, badge9)
print(f"[pixel-art] badge_9px_pixelart.png written to {tmp}")
PYEOF
TMP_DIR="${TMP}" python3 - <<'PYEOF'
import struct, zlib, os

def write_png_rgba(filename, width, height, pixels):
    raw = b''
    for y in range(height):
        raw += b'\x00'
        for x in range(width):
            r, g, b, a = pixels[y][x]
            raw += bytes([r, g, b, a])
    compressed = zlib.compress(raw, 9)
    def chunk(name, data):
        c = name + data
        return struct.pack('>I', len(data)) + c + struct.pack('>I', zlib.crc32(c) & 0xffffffff)
    png = b'\x89PNG\r\n\x1a\n'
    png += chunk(b'IHDR', struct.pack('>IIBBBBB', width, height, 8, 6, 0, 0, 0))
    png += chunk(b'IDAT', compressed)
    png += chunk(b'IEND', b'')
    with open(filename, 'wb') as f:
        f.write(png)

W = (255, 255, 255, 255)
D = (13, 17, 23, 255)
tmp = os.environ['TMP_DIR']

badge9 = [
    [W, W, W, W, W, W, W, W, W],
    [W, W, D, D, D, D, W, W, W],
    [W, D, D, W, W, W, W, W, W],
    [W, D, D, W, W, W, W, W, W],
    [W, W, D, D, D, W, W, W, W],
    [W, W, W, W, D, D, W, W, W],
    [W, W, W, W, D, D, W, W, W],
    [W, W, D, D, D, D, W, W, W],
    [W, W, W, W, W, W, W, W, W],
]
write_png_rgba(os.path.join(tmp, 'badge_9px_pixelart.png'), 9, 9, badge9)
print(f"[pixel-art] badge_9px_pixelart.png written")
PYEOF

# ---------------------------------------------------------------------------
# Helper: make_staged_icon <base_size> <badge_size> <badge_x_offset> <output>
# Composites a staging badge onto the base VaultMTG icon.
#
# badge_x_offset = base_size - badge_size (lower-right placement)
#
# For sizes <= 18px: uses the pixel-art badge (badge_size must be 9).
# For sizes >= 32px: renders S from Arial Black at 4x intermediate then scales down.
# ---------------------------------------------------------------------------
make_staged_icon() {
  local base_size="$1"
  local badge_size="$2"
  local output="$3"

  local base_png="${TMP}/base_${base_size}.png"
  rsvg-convert -w "${base_size}" -h "${base_size}" "${SVG_PATH}" -o "${base_png}"

  local offset=$(( base_size - badge_size ))

  if [[ "${base_size}" -le 18 ]]; then
    # Pixel-art badge — guaranteed legible at small sizes (Prof YELLOW condition).
    local badge_png="${TMP}/badge_9px_pixelart.png"
    magick "${base_png}" "${badge_png}" \
      -geometry "+${offset}+${offset}" \
      -compose Over -composite \
      "${output}"
  else
    # Font-rendered badge at 4x intermediate, then scaled down.
    local font_size=$(( badge_size * 4 ))
    local intermediate="${TMP}/badge_${badge_size}_intermediate.png"
    magick -size "${font_size}x${font_size}" xc:white \
      -fill '#0D1117' \
      -font "${ARIAL_BLACK}" \
      -pointsize "${font_size}" \
      -gravity Center \
      -annotate 0 "S" \
      -resize "${badge_size}x${badge_size}" \
      "${intermediate}"
    magick "${base_png}" "${intermediate}" \
      -gravity SouthEast \
      -compose Over -composite \
      "${output}"
  fi

  echo "[export-staging-icons] ${base_size}px staging icon written: ${output}"
}

# ---------------------------------------------------------------------------
# macOS .icns — full iconset (16→512 plus @2x retina variants)
# Badge sizes chosen so the badge is visually proportional at each resolution.
# ---------------------------------------------------------------------------
ICONSET_DIR="${TMP}/vaultmtg-staging.iconset"
mkdir -p "${ICONSET_DIR}"

echo "[export-staging-icons] generating .icns iconset PNGs ..."

# 16px: 9px badge (pixel-art, Prof YELLOW)
make_staged_icon 16  9  "${ICONSET_DIR}/icon_16x16.png"
# 32px: 12px badge (font-rendered via Arial Black 4x)
make_staged_icon 32  12 "${ICONSET_DIR}/icon_16x16@2x.png"
make_staged_icon 32  12 "${ICONSET_DIR}/icon_32x32.png"
make_staged_icon 64  24 "${ICONSET_DIR}/icon_32x32@2x.png"
make_staged_icon 128 48 "${ICONSET_DIR}/icon_128x128.png"
make_staged_icon 256 96 "${ICONSET_DIR}/icon_128x128@2x.png"
make_staged_icon 256 96 "${ICONSET_DIR}/icon_256x256.png"
make_staged_icon 512 192 "${ICONSET_DIR}/icon_256x256@2x.png"
make_staged_icon 512 192 "${ICONSET_DIR}/icon_512x512.png"
make_staged_icon 1024 384 "${ICONSET_DIR}/icon_512x512@2x.png"

iconutil -c icns "${ICONSET_DIR}" -o "${SCRIPT_DIR}/vaultmtg-staging.icns"
echo "[export-staging-icons] vaultmtg-staging.icns written ($(du -sh "${SCRIPT_DIR}/vaultmtg-staging.icns" | cut -f1))"

# ---------------------------------------------------------------------------
# Windows .ico — multi-resolution (16 / 32 / 48 / 256 px)
# ---------------------------------------------------------------------------
echo "[export-staging-icons] generating .ico PNGs ..."
ICO_TMP="${TMP}/ico"
mkdir -p "${ICO_TMP}"

make_staged_icon 16  9  "${ICO_TMP}/icon_16.png"
make_staged_icon 32  12 "${ICO_TMP}/icon_32.png"
make_staged_icon 48  18 "${ICO_TMP}/icon_48.png"
make_staged_icon 256 96 "${ICO_TMP}/icon_256.png"

magick \
  "${ICO_TMP}/icon_16.png" \
  "${ICO_TMP}/icon_32.png" \
  "${ICO_TMP}/icon_48.png" \
  "${ICO_TMP}/icon_256.png" \
  "${SCRIPT_DIR}/vaultmtg-staging.ico"
echo "[export-staging-icons] vaultmtg-staging.ico written ($(du -sh "${SCRIPT_DIR}/vaultmtg-staging.ico" | cut -f1))"

# ---------------------------------------------------------------------------
# Staging tray icon PNG — 32×32 RGBA (embedded by //go:embed in tray.go)
# ---------------------------------------------------------------------------
TRAY_ASSET="${SCRIPT_DIR}/../../internal/tray/assets/staging_icon.png"
echo "[export-staging-icons] generating staging tray icon.png (32×32) ..."
make_staged_icon 32 12 "${TMP}/tray_staging.png"
cp "${TMP}/tray_staging.png" "${TRAY_ASSET}"
echo "[export-staging-icons] staging_icon.png written: ${TRAY_ASSET}"

echo "[export-staging-icons] done — Result: SUCCESS"
