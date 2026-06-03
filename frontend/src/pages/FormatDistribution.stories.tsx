/**
 * FormatDistribution stories — v0.3.7 anti-slop wave (SemanticColors variant).
 */
import type { StoryObj } from '@storybook/react';

// ─── SemanticColors variant ──────────────────────────────────────────────────

export const SemanticColors: StoryObj = {
  name: 'SemanticColors — all 9 mapped formats',
  render: () => {
    const formats = [
      'QuickDraft', 'PremierDraft', 'Ladder', 'Play',
      'Alchemy', 'Historic', 'Explorer', 'Traditional', 'Unknown',
    ];
    const FORMAT_COLOR: Record<string, string> = {
      QuickDraft:   'var(--vault-sapphire)',
      PremierDraft: 'var(--vault-sapphire-light)',
      Ladder:       'var(--vault-success)',
      Play:         'var(--vault-fg-secondary)',
      Alchemy:      'var(--vault-warning)',
      Historic:     'var(--vault-indigo)',
      Explorer:     'var(--vault-indigo-light)',
      Traditional:  'var(--vault-danger)',
    };
    const getColor = (f: string) => FORMAT_COLOR[f] ?? 'var(--vault-fg-muted)';
    return (
      <div style={{ padding: 24, background: '#0D1117', display: 'flex', flexWrap: 'wrap', gap: 12 }}>
        {formats.map((format) => (
          <div
            key={format}
            style={{
              display: 'flex', alignItems: 'center', gap: 8,
              background: '#161C26', padding: '8px 12px', borderRadius: 8,
              border: '1px solid #2A3347',
            }}
          >
            <div
              style={{
                width: 12, height: 12, borderRadius: 2,
                background: getColor(format),
                flexShrink: 0,
              }}
            />
            <span style={{ color: '#F1F5F9', fontFamily: 'sans-serif', fontSize: 13 }}>{format}</span>
          </div>
        ))}
      </div>
    );
  },
};

export default {
  title: 'Pages/FormatDistribution',
  parameters: {
    layout: 'fullscreen',
    backgrounds: { default: 'dark', values: [{ name: 'dark', value: '#0D1117' }] },
  },
};
