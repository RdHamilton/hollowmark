/**
 * MagicGlyphs component tests (#1026).
 *
 * Verifies:
 *  - Each glyph renders an SVG with the correct data-glyph attribute (AC2).
 *  - Size prop is respected (default 24px, custom sizes).
 *  - Extra SVG props are forwarded (e.g., className, style).
 */
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import {
  LedgerGlyph,
  FanCardsGlyph,
  DeckStackGlyph,
  BinderGlyph,
} from './MagicGlyphs';

describe('MagicGlyphs', () => {
  describe('LedgerGlyph', () => {
    it('renders an SVG with data-glyph="ledger"', () => {
      const { container } = render(<LedgerGlyph />);
      const svg = container.querySelector('svg[data-glyph="ledger"]');
      expect(svg).not.toBeNull();
    });

    it('defaults to 24px size', () => {
      const { container } = render(<LedgerGlyph />);
      const svg = container.querySelector('svg');
      expect(svg?.getAttribute('width')).toBe('24');
      expect(svg?.getAttribute('height')).toBe('24');
    });

    it('respects a custom size prop', () => {
      const { container } = render(<LedgerGlyph size={20} />);
      const svg = container.querySelector('svg');
      expect(svg?.getAttribute('width')).toBe('20');
      expect(svg?.getAttribute('height')).toBe('20');
    });

    it('forwards extra props (e.g., className)', () => {
      const { container } = render(<LedgerGlyph className="home-nav-icon" />);
      const svg = container.querySelector('svg');
      expect(svg?.classList.contains('home-nav-icon')).toBe(true);
    });
  });

  describe('FanCardsGlyph', () => {
    it('renders an SVG with data-glyph="fan-cards"', () => {
      const { container } = render(<FanCardsGlyph />);
      const svg = container.querySelector('svg[data-glyph="fan-cards"]');
      expect(svg).not.toBeNull();
    });

    it('defaults to 24px size', () => {
      const { container } = render(<FanCardsGlyph />);
      const svg = container.querySelector('svg');
      expect(svg?.getAttribute('width')).toBe('24');
      expect(svg?.getAttribute('height')).toBe('24');
    });
  });

  describe('DeckStackGlyph', () => {
    it('renders an SVG with data-glyph="deck-stack"', () => {
      const { container } = render(<DeckStackGlyph />);
      const svg = container.querySelector('svg[data-glyph="deck-stack"]');
      expect(svg).not.toBeNull();
    });

    it('defaults to 24px size', () => {
      const { container } = render(<DeckStackGlyph />);
      const svg = container.querySelector('svg');
      expect(svg?.getAttribute('width')).toBe('24');
      expect(svg?.getAttribute('height')).toBe('24');
    });
  });

  describe('BinderGlyph', () => {
    it('renders an SVG with data-glyph="binder"', () => {
      const { container } = render(<BinderGlyph />);
      const svg = container.querySelector('svg[data-glyph="binder"]');
      expect(svg).not.toBeNull();
    });

    it('defaults to 24px size', () => {
      const { container } = render(<BinderGlyph />);
      const svg = container.querySelector('svg');
      expect(svg?.getAttribute('width')).toBe('24');
      expect(svg?.getAttribute('height')).toBe('24');
    });
  });

  describe('all glyphs set aria-hidden', () => {
    it.each([
      ['LedgerGlyph', LedgerGlyph],
      ['FanCardsGlyph', FanCardsGlyph],
      ['DeckStackGlyph', DeckStackGlyph],
      ['BinderGlyph', BinderGlyph],
    ] as const)('%s sets aria-hidden="true"', (_name, Glyph) => {
      const { container } = render(<Glyph />);
      const svg = container.querySelector('svg');
      expect(svg?.getAttribute('aria-hidden')).toBe('true');
    });
  });
});
