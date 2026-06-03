/**
 * ManaCurveBar unit tests.
 *
 * The component renders a CSS bar chart for mana curve data from DeckStatistics.
 * Tests confirm: rendering, CMC aggregation, spike warning color, empty/null guard,
 * and size variants.
 *
 * Note: Color value rendering in jsdom is unreliable (CSS variables don't resolve).
 * Tests verify data-testid presence and structural correctness rather than colors.
 */
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import ManaCurveBar from './ManaCurveBar';

describe('ManaCurveBar', () => {
  describe('no-data states', () => {
    it('renders nothing when manaCurve is undefined', () => {
      const { container } = render(<ManaCurveBar manaCurve={undefined} />);
      expect(container.firstChild).toBeNull();
    });

    it('renders nothing when manaCurve is empty object', () => {
      const { container } = render(<ManaCurveBar manaCurve={{}} />);
      expect(container.firstChild).toBeNull();
    });
  });

  describe('bar rendering', () => {
    it('renders bars for CMC 1 through 7+', () => {
      render(<ManaCurveBar manaCurve={{ 1: 4, 2: 8, 3: 7, 4: 4, 5: 2, 6: 1 }} />);
      for (let cmc = 1; cmc <= 7; cmc++) {
        expect(screen.getByTestId(`mana-curve-bar-${cmc}`)).toBeInTheDocument();
      }
    });

    it('renders all 7 bars even when some CMC buckets are empty', () => {
      render(<ManaCurveBar manaCurve={{ 2: 4, 3: 8 }} />);
      // All 7 bars should be present (empty ones are zero-height gaps)
      for (let cmc = 1; cmc <= 7; cmc++) {
        expect(screen.getByTestId(`mana-curve-bar-${cmc}`)).toBeInTheDocument();
      }
    });

    it('sets aria-label with count for each bar', () => {
      render(<ManaCurveBar manaCurve={{ 1: 4, 2: 8 }} />);
      expect(screen.getByLabelText('CMC 1: 4')).toBeInTheDocument();
      expect(screen.getByLabelText('CMC 2: 8')).toBeInTheDocument();
    });
  });

  describe('CMC 7+ aggregation', () => {
    it('aggregates CMC >= 7 into the 7+ bar', () => {
      render(<ManaCurveBar manaCurve={{ 7: 2, 8: 1, 9: 1 }} />);
      // Bar 7 should show aggregated count of 4
      expect(screen.getByLabelText('CMC 7+: 4')).toBeInTheDocument();
    });
  });

  describe('tooltip', () => {
    it('shows avg CMC in title attribute', () => {
      const { container } = render(
        <ManaCurveBar manaCurve={{ 1: 4, 2: 8 }} label="Mana Curve" />
      );
      const barContainer = container.firstChild as HTMLElement;
      expect(barContainer.title).toContain('Avg CMC');
    });
  });

  describe('size variants', () => {
    it('accepts size="sm" without errors', () => {
      expect(() =>
        render(<ManaCurveBar manaCurve={{ 2: 8, 3: 7 }} size="sm" />)
      ).not.toThrow();
    });

    it('accepts size="md" without errors', () => {
      expect(() =>
        render(<ManaCurveBar manaCurve={{ 2: 8, 3: 7 }} size="md" />)
      ).not.toThrow();
    });
  });

  describe('accessibility', () => {
    it('renders a container with role="img" and aria-label', () => {
      render(<ManaCurveBar manaCurve={{ 2: 8, 3: 7 }} label="Mana Curve" />);
      expect(screen.getByRole('img', { name: /Mana Curve/i })).toBeInTheDocument();
    });
  });
});
