import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import ManaPip from './ManaPip';
import ColorIdentity from './ColorIdentity';

// mana-font imports a webfont CSS — the test env doesn't render it but the
// class names must be present so we assert on them directly.

describe('ManaPip', () => {
  it('renders a White pip with correct mana-font class and aria-label', () => {
    render(<ManaPip color="W" />);
    const pip = screen.getByRole('img', { name: 'White' });
    expect(pip).toBeInTheDocument();
    expect(pip).toHaveClass('ms-w');
    expect(pip).toHaveClass('ms-cost');
  });

  it('renders a Blue pip', () => {
    render(<ManaPip color="U" />);
    const pip = screen.getByRole('img', { name: 'Blue' });
    expect(pip).toHaveClass('ms-u');
  });

  it('renders a Black pip', () => {
    render(<ManaPip color="B" />);
    const pip = screen.getByRole('img', { name: 'Black' });
    expect(pip).toHaveClass('ms-b');
  });

  it('renders a Red pip', () => {
    render(<ManaPip color="R" />);
    const pip = screen.getByRole('img', { name: 'Red' });
    expect(pip).toHaveClass('ms-r');
  });

  it('renders a Green pip', () => {
    render(<ManaPip color="G" />);
    const pip = screen.getByRole('img', { name: 'Green' });
    expect(pip).toHaveClass('ms-g');
  });

  it('renders a Colorless pip', () => {
    render(<ManaPip color="C" />);
    const pip = screen.getByRole('img', { name: 'Colorless' });
    expect(pip).toHaveClass('ms-c');
  });

  it('renders a Multicolor pip', () => {
    render(<ManaPip color="M" />);
    const pip = screen.getByRole('img', { name: 'Multicolor' });
    expect(pip).toHaveClass('ms-gold');
  });

  it('applies the sm size as 14px font-size', () => {
    render(<ManaPip color="W" size="sm" />);
    const pip = screen.getByTestId('mana-pip-w');
    expect(pip).toHaveStyle({ fontSize: '14px' });
  });

  it('applies the md size as 18px font-size (default)', () => {
    render(<ManaPip color="W" />);
    const pip = screen.getByTestId('mana-pip-w');
    expect(pip).toHaveStyle({ fontSize: '18px' });
  });

  it('applies the lg size as 24px font-size', () => {
    render(<ManaPip color="W" size="lg" />);
    const pip = screen.getByTestId('mana-pip-w');
    expect(pip).toHaveStyle({ fontSize: '24px' });
  });

  it('passes down additional className', () => {
    render(<ManaPip color="R" className="extra" />);
    expect(screen.getByTestId('mana-pip-r')).toHaveClass('extra');
  });
});

describe('ColorIdentity', () => {
  it('renders correct pips for an array of colors', () => {
    render(<ColorIdentity colors={['W', 'U']} />);
    expect(screen.getByTestId('mana-pip-w')).toBeInTheDocument();
    expect(screen.getByTestId('mana-pip-u')).toBeInTheDocument();
  });

  it('renders correct pips for a concatenated string', () => {
    render(<ColorIdentity colors="WUB" />);
    expect(screen.getByTestId('mana-pip-w')).toBeInTheDocument();
    expect(screen.getByTestId('mana-pip-u')).toBeInTheDocument();
    expect(screen.getByTestId('mana-pip-b')).toBeInTheDocument();
  });

  it('renders a Colorless pip for an empty array', () => {
    render(<ColorIdentity colors={[]} />);
    expect(screen.getByTestId('mana-pip-c')).toBeInTheDocument();
  });

  it('renders a Colorless pip for undefined', () => {
    render(<ColorIdentity colors={undefined} />);
    expect(screen.getByTestId('mana-pip-c')).toBeInTheDocument();
  });

  it('renders a Colorless pip for null', () => {
    render(<ColorIdentity colors={null} />);
    expect(screen.getByTestId('mana-pip-c')).toBeInTheDocument();
  });

  it('renders a Colorless pip for an empty string', () => {
    render(<ColorIdentity colors="" />);
    expect(screen.getByTestId('mana-pip-c')).toBeInTheDocument();
  });

  it('ignores invalid characters and falls back to colorless when all invalid', () => {
    render(<ColorIdentity colors={['X', 'Y']} />);
    // All invalid → fallback colorless
    expect(screen.getByTestId('mana-pip-c')).toBeInTheDocument();
  });

  it('filters out invalid chars when mixed with valid ones', () => {
    render(<ColorIdentity colors={['W', 'X']} />);
    expect(screen.getByTestId('mana-pip-w')).toBeInTheDocument();
    expect(screen.queryByTestId('mana-pip-c')).not.toBeInTheDocument();
  });

  it('renders the wrapper with data-testid color-identity', () => {
    render(<ColorIdentity colors={['G']} />);
    expect(screen.getByTestId('color-identity')).toBeInTheDocument();
  });

  it('accepts lowercase color strings and normalizes them', () => {
    render(<ColorIdentity colors={['r', 'g']} />);
    expect(screen.getByTestId('mana-pip-r')).toBeInTheDocument();
    expect(screen.getByTestId('mana-pip-g')).toBeInTheDocument();
  });
});
