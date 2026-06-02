/**
 * ManaWheel — five-color pentagon with a center V-mark "vault".
 *
 * Ported from the marketing ui_kit:
 *   vault-mtg-docs/engineering/design/rebranding/Ray Hamilton Engineering Design System/
 *   ui_kits/vaultmtg-web/ManaWheel.jsx
 *
 * Usage in the SPA: loading/empty state only (not decorative chrome).
 * The five color orbs stay canonical — W top, U upper-right, B lower-right,
 * R lower-left, G upper-left. Only the accent (pentagon, star connections,
 * center V-mark, halo) changes; default is Vault Sapphire #4A90D9.
 */

import React from 'react';

interface ManaWheelProps {
  /** Accent color. Defaults to Vault Sapphire #4A90D9. */
  color?: string;
  /** Rendered height in px. Width scales proportionally. */
  size?: number;
  /** Accessible label for the SVG. */
  ariaLabel?: string;
}

// Pentagon vertex coordinates — center (240, 300), radius 165.
// W = top, U = upper-right, B = lower-right, R = lower-left, G = upper-left
const W: [number, number] = [240, 135];
const U: [number, number] = [397, 250];
const B: [number, number] = [337, 460];
const R: [number, number] = [143, 460];
const G: [number, number] = [83, 250];

function hexToRgba(hex: string, alpha: number): string {
  const r = parseInt(hex.slice(1, 3), 16);
  const g = parseInt(hex.slice(3, 5), 16);
  const b = parseInt(hex.slice(5, 7), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

const ManaWheel: React.FC<ManaWheelProps> = ({
  color = '#4A90D9',
  size = 160,
  ariaLabel = 'VaultMTG five-color mana wheel',
}) => {
  // Unique IDs so multiple instances on the same page don't share gradient IDs
  const safeColorId = color.replace('#', '');
  const haloId = `mw-halo-${safeColorId}`;
  const vGlowId = `mw-vglow-${safeColorId}`;

  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      viewBox="0 0 480 600"
      style={{
        width: 'auto',
        height: size,
        filter: `drop-shadow(0 16px 40px ${hexToRgba(color, 0.18)})`,
      }}
      aria-label={ariaLabel}
      role="img"
      data-testid="mana-wheel"
    >
      <defs>
        <radialGradient id={haloId} cx="50%" cy="50%" r="50%">
          <stop offset="0%"   stopColor={color} stopOpacity="0.35" />
          <stop offset="35%"  stopColor={color} stopOpacity="0.10" />
          <stop offset="80%"  stopColor={color} stopOpacity="0" />
        </radialGradient>

        <radialGradient id="mw-orb-white" cx="35%" cy="30%" r="70%">
          <stop offset="0%"   stopColor="#FFFAF0" />
          <stop offset="45%"  stopColor="#E8E0C8" />
          <stop offset="100%" stopColor="#9C9580" />
        </radialGradient>
        <radialGradient id="mw-orb-blue" cx="35%" cy="30%" r="70%">
          <stop offset="0%"   stopColor="#7CB5F0" />
          <stop offset="45%"  stopColor="#4A90D9" />
          <stop offset="100%" stopColor="#1F4A82" />
        </radialGradient>
        <radialGradient id="mw-orb-black" cx="35%" cy="30%" r="70%">
          <stop offset="0%"   stopColor="#C5ADE0" />
          <stop offset="45%"  stopColor="#9B7FC2" />
          <stop offset="100%" stopColor="#4B3A6A" />
        </radialGradient>
        <radialGradient id="mw-orb-red" cx="35%" cy="30%" r="70%">
          <stop offset="0%"   stopColor="#E87560" />
          <stop offset="45%"  stopColor="#C94E3A" />
          <stop offset="100%" stopColor="#6E2418" />
        </radialGradient>
        <radialGradient id="mw-orb-green" cx="35%" cy="30%" r="70%">
          <stop offset="0%"   stopColor="#6BD08D" />
          <stop offset="45%"  stopColor="#3A9E5F" />
          <stop offset="100%" stopColor="#1A4A2C" />
        </radialGradient>

        <radialGradient id={vGlowId} cx="50%" cy="50%" r="60%">
          <stop offset="0%"   stopColor={color} stopOpacity="0.4" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </radialGradient>

        <filter id="mw-orb-shadow" x="-50%" y="-50%" width="200%" height="200%">
          <feGaussianBlur in="SourceAlpha" stdDeviation="4" />
          <feOffset dx="0" dy="6" />
          <feComponentTransfer>
            <feFuncA type="linear" slope="0.5" />
          </feComponentTransfer>
          <feMerge>
            <feMergeNode />
            <feMergeNode in="SourceGraphic" />
          </feMerge>
        </filter>
      </defs>

      {/* Halo behind the wheel */}
      <ellipse cx="240" cy="300" rx="240" ry="280" fill={`url(#${haloId})`} />

      {/* Inner star — enemy connections (thin) */}
      <g
        stroke={hexToRgba(color, 0.30)}
        strokeWidth="1.5"
        fill="none"
        strokeLinecap="round"
      >
        <line x1={W[0]} y1={W[1]} x2={B[0]} y2={B[1]} />
        <line x1={W[0]} y1={W[1]} x2={R[0]} y2={R[1]} />
        <line x1={U[0]} y1={U[1]} x2={R[0]} y2={R[1]} />
        <line x1={U[0]} y1={U[1]} x2={G[0]} y2={G[1]} />
        <line x1={B[0]} y1={B[1]} x2={G[0]} y2={G[1]} />
      </g>

      {/* Outer pentagon — friend connections (thicker) */}
      <g
        stroke={hexToRgba(color, 0.65)}
        strokeWidth="2.5"
        fill="none"
        strokeLinecap="round"
      >
        <line x1={W[0]} y1={W[1]} x2={U[0]} y2={U[1]} />
        <line x1={U[0]} y1={U[1]} x2={B[0]} y2={B[1]} />
        <line x1={B[0]} y1={B[1]} x2={R[0]} y2={R[1]} />
        <line x1={R[0]} y1={R[1]} x2={G[0]} y2={G[1]} />
        <line x1={G[0]} y1={G[1]} x2={W[0]} y2={W[1]} />
      </g>

      {/* Color orbs — always the five-color canon */}
      <g filter="url(#mw-orb-shadow)">
        {(
          [
            { p: W, grad: 'mw-orb-white' },
            { p: U, grad: 'mw-orb-blue' },
            { p: B, grad: 'mw-orb-black' },
            { p: R, grad: 'mw-orb-red' },
            { p: G, grad: 'mw-orb-green' },
          ] as Array<{ p: [number, number]; grad: string }>
        ).map(({ p, grad }, i) => (
          <g key={i}>
            <circle cx={p[0]} cy={p[1]} r="40" fill={`url(#${grad})`} />
            <circle
              cx={p[0]}
              cy={p[1]}
              r="40"
              fill="none"
              stroke={hexToRgba(color, 0.4)}
              strokeWidth="1"
            />
          </g>
        ))}
      </g>

      {/* Specular highlights */}
      <g fill="rgba(255,255,255,0.5)">
        <ellipse cx={W[0] - 12} cy={W[1] - 12} rx="9" ry="5" />
        <ellipse cx={U[0] - 12} cy={U[1] - 12} rx="9" ry="5" />
        <ellipse cx={B[0] - 12} cy={B[1] - 12} rx="9" ry="5" />
        <ellipse cx={R[0] - 12} cy={R[1] - 12} rx="9" ry="5" />
        <ellipse cx={G[0] - 12} cy={G[1] - 12} rx="9" ry="5" />
      </g>

      {/* Center vault — V mark in accent color */}
      <circle cx="240" cy="300" r="80" fill={`url(#${vGlowId})`} />
      <g transform="translate(240, 300) scale(1.6) translate(-32, -32)">
        <path
          fill={color}
          fillRule="evenodd"
          d="M 8 10 L 22 10 L 32 38 L 42 10 L 56 10 L 32 56 Z M 19 27 L 45 27 L 45 31 L 19 31 Z"
        />
      </g>
    </svg>
  );
};

export default ManaWheel;
