/**
 * Icon integrity test — Hollowmark raster icon regen (#3237 follow-up).
 *
 * Asserts:
 *   1. All required raster icon files exist in public/.
 *   2. Each PNG is the correct pixel dimension.
 *   3. The .ico file is non-trivially sized (multi-res .ico > 4 KB).
 *   4. Each PNG's SHA-256 matches the raster produced from
 *      logo-hollowmark-app-icon.svg (the source of truth).
 *      This catches any future accidental overwrite with a non-Hollowmark asset.
 *   5. index.html SVG favicon references logo-hollowmark-app-icon.svg.
 *   6. index.html has no vaultmtg raster (PNG/ICO) references.
 *   7. site.webmanifest icon sources exist on disk.
 *
 * These are node-environment tests (filesystem, not DOM) so they
 * run under the vitest "node" project in vite.config.ts.
 *
 * PNG dimension reading uses a minimal header parse — PNG stores width and
 * height as 4-byte big-endian integers at byte offsets 16 and 20 respectively.
 *
 * Expected SHA-256 values were derived by running:
 *   cairosvg logo-hollowmark-app-icon.svg -o <file> --output-width W --output-height H
 * against the committed SVG (logo-hollowmark-app-icon.svg @ 1facc875).
 */

import { describe, it, expect } from 'vitest';
import crypto from 'crypto';
import fs from 'fs';
import path from 'path';

const PUBLIC = path.resolve(__dirname, '../../public');

function readPngDimensions(filePath: string): { width: number; height: number } {
  const buf = fs.readFileSync(filePath);
  // PNG signature: 8 bytes; IHDR chunk: 4 (length) + 4 (type) + data starts at byte 16
  const width = buf.readUInt32BE(16);
  const height = buf.readUInt32BE(20);
  return { width, height };
}

function sha256(filePath: string): string {
  return crypto.createHash('sha256').update(fs.readFileSync(filePath)).digest('hex');
}

// Expected SHA-256 values derived from logo-hollowmark-app-icon.svg via cairosvg.
// Update this table whenever the master SVG is intentionally replaced.
const EXPECTED_PNGS: {
  file: string;
  width: number;
  height: number;
  sha256: string;
}[] = [
  {
    file: 'favicon-16.png',
    width: 16,
    height: 16,
    sha256: '719ba80a7bb6238b1d41e58b9b7da56220feac77d83da381f5f16da3cc5a8a8c',
  },
  {
    file: 'favicon-32.png',
    width: 32,
    height: 32,
    sha256: 'a4990134d8ba7bcf6325bb016e1ce12652fb0e65435db66260577e53fb42e2a1',
  },
  {
    file: 'apple-touch-icon.png',
    width: 180,
    height: 180,
    sha256: 'dbcc3e61d030672a7040308c44ca5676899a5227638e2b0b0aff4e2091d65cfe',
  },
  {
    file: 'icon-192.png',
    width: 192,
    height: 192,
    sha256: '1f5d36f9f681d8b1a318d6c41b12502001afe3641f4edbe1780ed5ccfdf6f242',
  },
  {
    file: 'icon-512.png',
    width: 512,
    height: 512,
    sha256: 'd308fdfe6e2b161ba54037269462297ed4df2c358f4bf24a6e474f2e57ec02a9',
  },
];

describe('Hollowmark raster icon set', () => {
  it.each(EXPECTED_PNGS)(
    '$file exists at $width×$height with Hollowmark content',
    ({ file, width, height, sha256: expectedHash }) => {
      const filePath = path.join(PUBLIC, file);
      expect(fs.existsSync(filePath), `${file} must exist`).toBe(true);

      const dims = readPngDimensions(filePath);
      expect(dims.width, `${file} width`).toBe(width);
      expect(dims.height, `${file} height`).toBe(height);

      const actualHash = sha256(filePath);
      expect(
        actualHash,
        `${file} must be rasterized from logo-hollowmark-app-icon.svg. ` +
          `Got ${actualHash}, expected ${expectedHash}. ` +
          `Regenerate with cairosvg --output-width ${width} --output-height ${height}.`,
      ).toBe(expectedHash);
    },
  );

  it('favicon.ico exists and is a multi-res ICO (> 4 KB)', () => {
    const icoPath = path.join(PUBLIC, 'favicon.ico');
    expect(fs.existsSync(icoPath), 'favicon.ico must exist').toBe(true);
    const { size } = fs.statSync(icoPath);
    expect(size, 'favicon.ico must be > 4096 bytes (multi-res)').toBeGreaterThan(4096);
  });

  it('index.html SVG favicon references logo-hollowmark-app-icon.svg, not a vaultmtg asset', () => {
    const html = fs.readFileSync(path.join(PUBLIC, '../index.html'), 'utf8');
    expect(html).toContain('logo-hollowmark-app-icon.svg');
    expect(html).not.toMatch(/rel="icon"[^>]*logo-vaultmtg/);
  });

  it('index.html has no vaultmtg raster references (PNG or ICO)', () => {
    const html = fs.readFileSync(path.join(PUBLIC, '../index.html'), 'utf8');
    expect(html).not.toMatch(/href="[^"]*vaultmtg[^"]*\.(png|ico)"/i);
  });

  it('site.webmanifest icon sources exist on disk', () => {
    const manifest = JSON.parse(
      fs.readFileSync(path.join(PUBLIC, 'site.webmanifest'), 'utf8'),
    ) as { icons: { src: string }[] };
    for (const icon of manifest.icons) {
      const src = icon.src.replace(/^\//, '');
      const filePath = path.join(PUBLIC, src);
      expect(
        fs.existsSync(filePath),
        `manifest icon ${icon.src} must exist on disk`,
      ).toBe(true);
    }
  });
});
