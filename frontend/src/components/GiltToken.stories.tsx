import type { Meta, StoryObj } from '@storybook/react';

/**
 * Hollowmark Gilt token — design-system swatch and alias reference.
 *
 * hollowmark-tickets#1047: adds the `--vault-gilt` legacy alias for the
 * `--hollowmark-gilt` design token, and this Storybook story as the visual
 * contract / Chromatic snapshot target.
 *
 * Token definition (index.css):
 *   --hollowmark-gilt:  #B87D32  (aged brass — canonical gilt)
 *   --vault-gilt:       var(--hollowmark-gilt)  (legacy alias — same value)
 *
 * Usage context: gold-economy surfaces only — quest gold rewards, completion
 * milestones (OTJ 100%), and the mythic wildcard tally. NOT a CTA colour;
 * sapphire (--accent) owns CTAs.
 */
const meta: Meta = {
  title: 'Tokens/Gilt',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

/** Renders a color swatch with label. */
function Swatch({
  varName,
  label,
  note,
}: {
  varName: string;
  label: string;
  note?: string;
}) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 16,
        fontFamily: 'var(--font-mono, monospace)',
        fontSize: 13,
      }}
    >
      <div
        style={{
          width: 64,
          height: 64,
          borderRadius: 8,
          background: `var(${varName})`,
          flexShrink: 0,
          border: '1px solid rgba(255,255,255,0.1)',
        }}
      />
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        <span style={{ color: 'var(--vault-fg, #F1F5F9)', fontWeight: 600 }}>
          {varName}
        </span>
        <span style={{ color: 'var(--vault-fg-secondary, #94A3B8)' }}>{label}</span>
        {note && (
          <span style={{ color: 'var(--vault-fg-muted, #7890AA)', fontSize: 11 }}>
            {note}
          </span>
        )}
      </div>
    </div>
  );
}

/**
 * Primary gilt swatch — the canonical `--hollowmark-gilt` aged-brass token.
 */
export const HollowmarkGilt: Story = {
  name: '--hollowmark-gilt (canonical)',
  render: () => (
    <Swatch
      varName="--hollowmark-gilt"
      label="#B87D32 — aged brass"
      note="Primary gilt token. Use for gold-economy moments."
    />
  ),
};

/**
 * Legacy alias swatch — `--vault-gilt` resolves to the same value as
 * `--hollowmark-gilt` via a var() pointer. Both swatches must be
 * visually identical; any divergence is a Chromatic failure.
 */
export const VaultGiltAlias: Story = {
  name: '--vault-gilt (legacy alias)',
  render: () => (
    <Swatch
      varName="--vault-gilt"
      label="#B87D32 — aged brass (via --hollowmark-gilt)"
      note="Legacy alias. Resolves to var(--hollowmark-gilt). Identical color expected."
    />
  ),
};

/**
 * Both tokens side-by-side — used in Chromatic diff review to confirm they
 * resolve to the same color. Ray approves this Chromatic snapshot.
 */
export const GiltAliasParity: Story = {
  name: 'Alias parity — canonical vs legacy',
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24 }}>
      <Swatch
        varName="--hollowmark-gilt"
        label="#B87D32 — canonical"
      />
      <Swatch
        varName="--vault-gilt"
        label="#B87D32 — legacy alias (must match above)"
      />
      <p
        style={{
          fontFamily: 'var(--font-mono, monospace)',
          fontSize: 11,
          color: 'var(--vault-fg-muted, #7890AA)',
          maxWidth: 340,
          lineHeight: 1.5,
        }}
      >
        Both swatches must render identical. Divergence indicates a broken var()
        chain — file a P1 against hollowmark-tickets.
      </p>
    </div>
  ),
};

/**
 * Gilt in context — shows the aged-brass token applied to the kind of surface
 * it is designed for: a completion milestone badge.
 */
export const GiltInContext: Story = {
  name: 'Gilt in context — completion badge',
  render: () => (
    <div
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 8,
        padding: '6px 14px',
        borderRadius: 20,
        background: 'var(--vault-bg-raised, #161C26)',
        border: '1px solid var(--vault-gilt, #B87D32)',
        fontFamily: 'var(--font-body, sans-serif)',
        fontSize: 13,
        fontWeight: 600,
        color: 'var(--vault-gilt, #B87D32)',
      }}
    >
      <span>100%</span>
      <span style={{ fontSize: 11, fontWeight: 400 }}>OTJ Complete</span>
    </div>
  ),
};
