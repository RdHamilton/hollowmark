/**
 * MagicGlyphs stories — Magic-native Home nav-tile glyphs (#1026).
 *
 * Documents all 4 glyph components at 24px per the design spec (AC3/AC4).
 * Each renders at the canonical 24px size and must read clearly at that size.
 *
 * Glyph → nav-tile mapping (AC1):
 *   LedgerGlyph    → Match History
 *   FanCardsGlyph  → Draft
 *   DeckStackGlyph → Decks
 *   BinderGlyph    → Collection
 */
import type { Meta, StoryObj } from '@storybook/react';
import {
  LedgerGlyph,
  FanCardsGlyph,
  DeckStackGlyph,
  BinderGlyph,
} from './MagicGlyphs';

const meta: Meta = {
  title: 'Components/MagicGlyphs',
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'dark',
      values: [{ name: 'dark', value: '#0D1117' }],
    },
  },
};

export default meta;
type Story = StoryObj;

const glyphStyle = {
  color: 'var(--vault-fg-secondary, #94A3B8)',
  display: 'flex',
  flexDirection: 'column' as const,
  alignItems: 'center',
  gap: 8,
  padding: 16,
  background: '#161C26',
  borderRadius: 8,
  border: '1px solid #2A3347',
  width: 80,
};

const labelStyle = {
  fontSize: 11,
  fontFamily: 'monospace',
  color: '#4E6080',
  textAlign: 'center' as const,
  lineHeight: 1.3,
};

// ── Individual glyph stories (AC4 — one per glyph at 24px) ──────────────────

export const LedgerGlyphStory: Story = {
  name: 'LedgerGlyph — Match History',
  render: () => (
    <div style={glyphStyle}>
      <LedgerGlyph size={24} />
      <span style={labelStyle}>Ledger<br />24px</span>
    </div>
  ),
};

export const FanCardsGlyphStory: Story = {
  name: 'FanCardsGlyph — Draft',
  render: () => (
    <div style={glyphStyle}>
      <FanCardsGlyph size={24} />
      <span style={labelStyle}>Fan Cards<br />24px</span>
    </div>
  ),
};

export const DeckStackGlyphStory: Story = {
  name: 'DeckStackGlyph — Decks',
  render: () => (
    <div style={glyphStyle}>
      <DeckStackGlyph size={24} />
      <span style={labelStyle}>Deck Stack<br />24px</span>
    </div>
  ),
};

export const BinderGlyphStory: Story = {
  name: 'BinderGlyph — Collection',
  render: () => (
    <div style={glyphStyle}>
      <BinderGlyph size={24} />
      <span style={labelStyle}>Binder<br />24px</span>
    </div>
  ),
};

// ── All 4 together — eye-test consistency suite ──────────────────────────────

export const AllGlyphsSuite: Story = {
  name: 'All 4 Glyphs — consistency suite at 24px',
  render: () => (
    <div style={{ display: 'flex', gap: 16, padding: 24, background: '#0D1117' }}>
      {[
        { Glyph: LedgerGlyph, label: 'Match\nHistory' },
        { Glyph: FanCardsGlyph, label: 'Draft' },
        { Glyph: DeckStackGlyph, label: 'Decks' },
        { Glyph: BinderGlyph, label: 'Collection' },
      ].map(({ Glyph, label }) => (
        <div key={label} style={glyphStyle}>
          <Glyph size={24} />
          <span style={labelStyle}>{label.replace('\n', '\n')}</span>
        </div>
      ))}
    </div>
  ),
};
