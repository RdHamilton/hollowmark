/**
 * ManaWheel component tests.
 *
 * Verifies rendering with default and custom props, accessibility, and
 * that the component is testable in happy-dom.
 */

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import ManaWheel from './ManaWheel';

describe('ManaWheel', () => {
  it('renders the SVG element', () => {
    render(<ManaWheel />);
    expect(screen.getByTestId('mana-wheel')).toBeInTheDocument();
  });

  it('has default aria-label', () => {
    render(<ManaWheel />);
    const svg = screen.getByRole('img');
    expect(svg).toHaveAttribute('aria-label', 'VaultMTG five-color mana wheel');
  });

  it('accepts a custom aria-label', () => {
    render(<ManaWheel ariaLabel="Loading your stats" />);
    const svg = screen.getByRole('img');
    expect(svg).toHaveAttribute('aria-label', 'Loading your stats');
  });

  it('uses default sapphire color when no color prop', () => {
    render(<ManaWheel />);
    // The SVG renders without errors when default color is used
    expect(screen.getByTestId('mana-wheel')).toBeInTheDocument();
  });

  it('accepts custom color prop', () => {
    // Just verifies no throw and the element renders
    render(<ManaWheel color="#FF0000" />);
    expect(screen.getByTestId('mana-wheel')).toBeInTheDocument();
  });

  it('has role=img', () => {
    render(<ManaWheel />);
    expect(screen.getByRole('img')).toBeInTheDocument();
  });
});
