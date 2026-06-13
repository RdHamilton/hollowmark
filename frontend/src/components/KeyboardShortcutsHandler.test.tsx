import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import KeyboardShortcutsHandler from './KeyboardShortcutsHandler';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async (importOriginal) => {
  const actual = await importOriginal<typeof import('react-router-dom')>();
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

const mockEventsEmit = vi.fn();
vi.mock('@/services/websocketClient', () => ({
  EventsEmit: (...args: unknown[]) => mockEventsEmit(...args),
}));

// ---------------------------------------------------------------------------
// Helper: dispatch a keyboard event on window
// ---------------------------------------------------------------------------
function pressKey(key: string, opts: { metaKey?: boolean; ctrlKey?: boolean } = {}) {
  fireEvent.keyDown(window, { key, ...opts });
}

describe('KeyboardShortcutsHandler', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  function renderComponent() {
    return render(
      <MemoryRouter>
        <KeyboardShortcutsHandler />
      </MemoryRouter>
    );
  }

  it('renders nothing (null)', () => {
    const { container } = renderComponent();
    expect(container.firstChild).toBeNull();
  });

  it('navigates to /match-history on Ctrl+1', () => {
    renderComponent();
    pressKey('1', { ctrlKey: true });
    expect(mockNavigate).toHaveBeenCalledWith('/match-history');
  });

  it('navigates to /quests on Ctrl+2', () => {
    renderComponent();
    pressKey('2', { ctrlKey: true });
    expect(mockNavigate).toHaveBeenCalledWith('/quests');
  });

  it('navigates to /draft on Ctrl+3', () => {
    renderComponent();
    pressKey('3', { ctrlKey: true });
    expect(mockNavigate).toHaveBeenCalledWith('/draft');
  });

  it('navigates to /decks on Ctrl+4', () => {
    renderComponent();
    pressKey('4', { ctrlKey: true });
    expect(mockNavigate).toHaveBeenCalledWith('/decks');
  });

  it('navigates to /charts/win-rate-trend on Ctrl+5', () => {
    renderComponent();
    pressKey('5', { ctrlKey: true });
    expect(mockNavigate).toHaveBeenCalledWith('/charts/win-rate-trend');
  });

  it('navigates to /settings on Ctrl+6', () => {
    renderComponent();
    pressKey('6', { ctrlKey: true });
    expect(mockNavigate).toHaveBeenCalledWith('/settings');
  });

  it('navigates to /settings on Ctrl+,', () => {
    renderComponent();
    pressKey(',', { ctrlKey: true });
    expect(mockNavigate).toHaveBeenCalledWith('/settings');
  });

  it('emits readmodel.updated for all domains on Ctrl+R (ADR-084)', () => {
    renderComponent();
    pressKey('r', { ctrlKey: true });
    expect(mockEventsEmit).toHaveBeenCalledWith('readmodel.updated', {
      domains: ['matches', 'drafts', 'quests', 'collection', 'decks', 'inventory', 'mastery'],
    });
  });

  it('emits readmodel.updated for all domains on Ctrl+Shift+R (uppercase R) (ADR-084)', () => {
    renderComponent();
    pressKey('R', { ctrlKey: true });
    expect(mockEventsEmit).toHaveBeenCalledWith('readmodel.updated', {
      domains: ['matches', 'drafts', 'quests', 'collection', 'decks', 'inventory', 'mastery'],
    });
  });

  it('does not navigate when modifier key is absent', () => {
    renderComponent();
    pressKey('1');
    expect(mockNavigate).not.toHaveBeenCalled();
  });

  it('does not navigate when target is an INPUT element', () => {
    renderComponent();
    const input = document.createElement('input');
    document.body.appendChild(input);
    fireEvent.keyDown(input, { key: '1', ctrlKey: true });
    expect(mockNavigate).not.toHaveBeenCalled();
    document.body.removeChild(input);
  });

  it('does not navigate when target is a TEXTAREA element', () => {
    renderComponent();
    const textarea = document.createElement('textarea');
    document.body.appendChild(textarea);
    fireEvent.keyDown(textarea, { key: '2', ctrlKey: true });
    expect(mockNavigate).not.toHaveBeenCalled();
    document.body.removeChild(textarea);
  });

  it('removes event listener on unmount (no stale navigation after unmount)', () => {
    const { unmount } = renderComponent();
    unmount();
    pressKey('1', { ctrlKey: true });
    expect(mockNavigate).not.toHaveBeenCalled();
  });
});
