/**
 * Layout.stories — nav-lockup brand coverage test (#1347).
 *
 * Asserts that the stories render the Hollowmark Watermark wordmark as an
 * <img alt="Hollowmark"> inside [data-testid="nav-brand"], not a plain
 * text <span>. Failing means Chromatic would snapshot a text fallback instead
 * of the real SVG brand surface.
 */
import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { SignedIn } from './Layout.stories';

describe('Layout.stories nav-lockup brand coverage (#1347)', () => {
  it('SignedIn story renders the Hollowmark wordmark as an <img>, not a text span', () => {
    if (!SignedIn.render) throw new Error('SignedIn story has no render function');

    // Wrap in MemoryRouter — stories use it via the decorator, but render()
    // strips decorators; we re-wrap manually here for the router context.
    render(<MemoryRouter>{SignedIn.render({}, {} as never)}</MemoryRouter>);

    const brand = screen.getByTestId('nav-brand');
    expect(brand).toBeInTheDocument();

    // Must be an img with the Hollowmark alt text — NOT a plain text span.
    const wordmark = brand.querySelector('img');
    expect(wordmark, 'nav-brand must contain an <img>, not a plain text span').not.toBeNull();
    expect(wordmark).toHaveAttribute('alt', 'Hollowmark');
  });
});
