import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import MissingCards from './MissingCards';

/**
 * MissingCards component notes:
 * - Returns null while loading, on error, or when analysis has no missing cards.
 * - The internal getMissingCardsForDraft always returns TotalMissing=0 (not implemented in REST API).
 * - As a result, the component renders nothing for any props combination.
 * - Tests verify this null-render contract and key guard conditions.
 */

describe('MissingCards', () => {
  it('renders nothing when sessionID is empty', () => {
    const { container } = render(
      <MissingCards sessionID="" packNumber={1} pickNumber={2} />
    );
    expect(container.firstChild).toBeNull();
  });

  it('renders nothing on P1P1 (pickNumber <= 1)', () => {
    const { container } = render(
      <MissingCards sessionID="session-abc" packNumber={1} pickNumber={1} />
    );
    expect(container.firstChild).toBeNull();
  });

  it('renders nothing when pickNumber is 0', () => {
    const { container } = render(
      <MissingCards sessionID="session-abc" packNumber={1} pickNumber={0} />
    );
    expect(container.firstChild).toBeNull();
  });

  it('renders nothing when REST API returns zero missing cards (not implemented)', async () => {
    // getMissingCardsForDraft is a stub that always returns TotalMissing=0.
    // Even with a valid session and pick > 1, the component shows nothing.
    const { container } = render(
      <MissingCards sessionID="session-abc" packNumber={2} pickNumber={5} />
    );
    // Flush microtasks so the useEffect async call completes
    await new Promise((r) => setTimeout(r, 0));
    expect(container.firstChild).toBeNull();
  });

  it('does not render missing-cards-container in DOM', async () => {
    render(<MissingCards sessionID="session-abc" packNumber={1} pickNumber={3} />);
    await new Promise((r) => setTimeout(r, 0));
    expect(document.querySelector('.missing-cards-container')).not.toBeInTheDocument();
  });

  it('does not throw when re-rendered with new props', () => {
    const { rerender } = render(
      <MissingCards sessionID="s1" packNumber={1} pickNumber={2} />
    );
    expect(() => {
      rerender(<MissingCards sessionID="s2" packNumber={2} pickNumber={4} />);
    }).not.toThrow();
  });
});
