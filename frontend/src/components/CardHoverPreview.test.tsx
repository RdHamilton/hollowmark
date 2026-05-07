import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import CardHoverPreview from './CardHoverPreview';

const baseProps = {
  name: 'Lightning Bolt',
  position: { x: 100, y: 200 },
};

describe('CardHoverPreview', () => {
  it('renders the card name', () => {
    render(<CardHoverPreview {...baseProps} />);
    expect(screen.getByText('Lightning Bolt')).toBeInTheDocument();
  });

  it('renders card image when imageURL is provided', () => {
    render(<CardHoverPreview {...baseProps} imageURL="https://example.com/bolt.jpg" />);
    const img = screen.getByRole('img', { name: 'Lightning Bolt' });
    expect(img).toBeInTheDocument();
    expect(img).toHaveAttribute('src', 'https://example.com/bolt.jpg');
  });

  it('does not render image element when imageURL is omitted', () => {
    render(<CardHoverPreview {...baseProps} />);
    expect(screen.queryByRole('img')).not.toBeInTheDocument();
  });

  it('renders typeLine when provided', () => {
    render(<CardHoverPreview {...baseProps} typeLine="Instant" />);
    expect(screen.getByText('Instant')).toBeInTheDocument();
  });

  it('renders mana cost when provided', () => {
    render(<CardHoverPreview {...baseProps} manaCost="{R}" />);
    expect(screen.getByText('Mana: {R}')).toBeInTheDocument();
  });

  it('renders power/toughness when both provided', () => {
    render(<CardHoverPreview {...baseProps} power="3" toughness="3" />);
    expect(screen.getByText('3/3')).toBeInTheDocument();
  });

  it('does not render power/toughness when only one is provided', () => {
    render(<CardHoverPreview {...baseProps} power="3" />);
    expect(screen.queryByText(/\/3/)).not.toBeInTheDocument();
  });

  it('renders oracle text when provided', () => {
    render(<CardHoverPreview {...baseProps} text="Deal 3 damage to any target." />);
    expect(screen.getByText('Deal 3 damage to any target.')).toBeInTheDocument();
  });

  it('renders score as percentage when provided', () => {
    render(<CardHoverPreview {...baseProps} score={0.85} />);
    expect(screen.getByText('Score: 85%')).toBeInTheDocument();
  });

  it('renders confidence as percentage when both score and confidence are provided', () => {
    render(<CardHoverPreview {...baseProps} score={0.85} confidence={0.9} />);
    expect(screen.getByText('Confidence: 90%')).toBeInTheDocument();
  });

  it('does not render confidence when score is absent', () => {
    render(<CardHoverPreview {...baseProps} confidence={0.9} />);
    expect(screen.queryByText(/Confidence/)).not.toBeInTheDocument();
  });

  it('renders reasoning when provided', () => {
    render(<CardHoverPreview {...baseProps} reasoning="Strong in aggressive decks." />);
    expect(screen.getByText('Strong in aggressive decks.')).toBeInTheDocument();
  });

  it('renders set code uppercased when provided', () => {
    render(<CardHoverPreview {...baseProps} setCode="m21" />);
    expect(screen.getByText('M21')).toBeInTheDocument();
  });

  it('renders rarity when provided', () => {
    render(<CardHoverPreview {...baseProps} rarity="Rare" />);
    expect(screen.getByText('Rare')).toBeInTheDocument();
  });

  it('renders setCode and rarity separated by bullet when both provided', () => {
    render(<CardHoverPreview {...baseProps} setCode="m21" rarity="Rare" />);
    expect(screen.getByText('M21 • Rare')).toBeInTheDocument();
  });

  it('positions the preview using fixed style with provided coordinates', () => {
    const { container } = render(
      <CardHoverPreview {...baseProps} position={{ x: 50, y: 20 }} />
    );
    const preview = container.querySelector('.card-hover-preview') as HTMLElement;
    expect(preview).toBeInTheDocument();
    expect(preview.style.position).toBe('fixed');
  });
});
