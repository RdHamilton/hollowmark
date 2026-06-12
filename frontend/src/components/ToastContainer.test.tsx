/**
 * ToastContainer component tests — updated for ADR-084.
 *
 * The dead colon-vocabulary SSE listeners (stats:updated, rank:updated,
 * quest:updated, draft:updated, collection:updated) were removed per ADR-084 §G1.
 * Toast reintroduction on readmodel.updated requires a Prof PLAYER_VERDICT first
 * (AC8 of #1369).
 *
 * This file tests:
 *  1. The showToast imperative API still works for all toast types.
 *  2. Dead event vocabulary no longer triggers toasts (regression guard).
 *  3. The component mounts/unmounts without errors.
 *  4. Notification preference (showNotifications=false) suppresses success/info toasts.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { render } from '../test/utils/testUtils';
import ToastContainer from './ToastContainer';
import { mockEventEmitter } from '@/test/mocks/websocketMock';

// Control showNotifications per-test (#2024).
const mockUseSettings = vi.fn(() => ({ showNotifications: true }));
vi.mock('../hooks/useSettings', () => ({
  useSettings: () => mockUseSettings(),
}));

const mockReplayState = {
  isActive: false,
  isPaused: false,
  progress: null,
};

interface ReplayState {
  isActive: boolean;
  isPaused: boolean;
  progress: null | unknown;
}

const mockSubscribers: Array<(state: ReplayState) => void> = [];

vi.mock('../App', () => ({
  getReplayState: vi.fn(() => mockReplayState),
  subscribeToReplayState: vi.fn((callback) => {
    mockSubscribers.push(callback);
    return () => {
      const index = mockSubscribers.indexOf(callback);
      if (index > -1) {
        mockSubscribers.splice(index, 1);
      }
    };
  }),
}));

describe('ToastContainer Component', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockEventEmitter.clear();
    mockSubscribers.length = 0;
    mockReplayState.isActive = false;
    mockReplayState.isPaused = false;
    mockUseSettings.mockReturnValue({ showNotifications: true });
  });

  describe('Initial state', () => {
    it('should not display toasts initially', () => {
      render(<ToastContainer />);
      expect(screen.queryByText(/match detected/i)).not.toBeInTheDocument();
    });

    it('renders the toast container div', () => {
      render(<ToastContainer />);
      const container = document.querySelector('[style*="position: fixed"]');
      expect(container).toBeInTheDocument();
    });
  });

  describe('showToast imperative API (primary toast mechanism)', () => {
    it('displays a success toast via showToast.show', async () => {
      const { showToast } = await import('./ToastContainer');
      render(<ToastContainer />);
      await new Promise(resolve => setTimeout(resolve, 50));

      showToast.show('Match recorded!', 'success');

      await waitFor(() => {
        expect(screen.getByText('Match recorded!')).toBeInTheDocument();
      });
    });

    it('displays an info toast via showToast.show', async () => {
      const { showToast } = await import('./ToastContainer');
      render(<ToastContainer />);
      await new Promise(resolve => setTimeout(resolve, 50));

      showToast.show('Data refreshed', 'info');

      await waitFor(() => {
        expect(screen.getByText('Data refreshed')).toBeInTheDocument();
      });
    });

    it('displays an error toast via showToast.show', async () => {
      const { showToast } = await import('./ToastContainer');
      render(<ToastContainer />);
      await new Promise(resolve => setTimeout(resolve, 50));

      showToast.show('Critical error occurred', 'error');

      await waitFor(() => {
        expect(screen.getByText('Critical error occurred')).toBeInTheDocument();
      });
    });

    it('displays a warning toast via showToast.show', async () => {
      const { showToast } = await import('./ToastContainer');
      render(<ToastContainer />);
      await new Promise(resolve => setTimeout(resolve, 50));

      showToast.show('Warning message', 'warning');

      await waitFor(() => {
        expect(screen.getByText('Warning message')).toBeInTheDocument();
      });
    });
  });

  describe('Dead event vocabulary no longer triggers toasts (ADR-084 §G1)', () => {
    // These guard against reintroduction of the dead event listeners.
    // The readmodel.updated event itself does NOT trigger toasts yet — Prof's
    // PLAYER_VERDICT is required first (AC8 of #1369).

    it('does NOT show a toast on stats:updated (listener removed)', async () => {
      render(<ToastContainer />);
      mockEventEmitter.emit('stats:updated', { matches: 1, games: 2 });
      await new Promise(resolve => setTimeout(resolve, 100));
      expect(screen.queryByText(/match detected/i)).not.toBeInTheDocument();
    });

    it('does NOT show a toast on rank:updated (listener removed)', async () => {
      render(<ToastContainer />);
      mockEventEmitter.emit('rank:updated', { format: 'Standard', tier: 'Gold', step: '3' });
      await new Promise(resolve => setTimeout(resolve, 100));
      expect(screen.queryByText(/rank updated/i)).not.toBeInTheDocument();
    });

    it('does NOT show a toast on quest:updated (listener removed)', async () => {
      render(<ToastContainer />);
      mockEventEmitter.emit('quest:updated', { completed: 1, count: 1 });
      await new Promise(resolve => setTimeout(resolve, 100));
      expect(screen.queryByText(/quest/i)).not.toBeInTheDocument();
    });

    it('does NOT show a toast on draft:updated (listener removed)', async () => {
      render(<ToastContainer />);
      mockEventEmitter.emit('draft:updated', { count: 1, picks: 5 });
      await new Promise(resolve => setTimeout(resolve, 100));
      expect(screen.queryByText(/draft session/i)).not.toBeInTheDocument();
    });

    it('does NOT show a toast on collection:updated (listener removed)', async () => {
      render(<ToastContainer />);
      mockEventEmitter.emit('collection:updated', { newCards: 5, cardsAdded: 10 });
      await new Promise(resolve => setTimeout(resolve, 100));
      expect(screen.queryByText(/collection updated/i)).not.toBeInTheDocument();
    });

    it('does NOT show a toast on readmodel.updated (Prof gate required for toast, AC8)', async () => {
      render(<ToastContainer />);
      mockEventEmitter.emit('readmodel.updated', { domains: ['matches', 'quests'] });
      await new Promise(resolve => setTimeout(resolve, 100));
      expect(screen.queryByText(/match/i)).not.toBeInTheDocument();
      expect(screen.queryByText(/quest/i)).not.toBeInTheDocument();
    });
  });

  describe('showNotifications=false (AC1/AC2 #2024)', () => {
    beforeEach(() => {
      mockUseSettings.mockReturnValue({ showNotifications: false });
    });

    it('still shows error toasts from showToast when notifications disabled', async () => {
      const { showToast } = await import('./ToastContainer');
      render(<ToastContainer />);
      await new Promise(resolve => setTimeout(resolve, 50));

      showToast.show('Critical error occurred', 'error');

      await waitFor(() => {
        expect(screen.getByText('Critical error occurred')).toBeInTheDocument();
      });
    });

    it('still shows warning toasts from showToast when notifications disabled', async () => {
      const { showToast } = await import('./ToastContainer');
      render(<ToastContainer />);
      await new Promise(resolve => setTimeout(resolve, 50));

      showToast.show('Warning message', 'warning');

      await waitFor(() => {
        expect(screen.getByText('Warning message')).toBeInTheDocument();
      });
    });

    it('suppresses info toasts from showToast when notifications disabled', async () => {
      const { showToast } = await import('./ToastContainer');
      render(<ToastContainer />);
      await new Promise(resolve => setTimeout(resolve, 50));

      showToast.show('Info message', 'info');

      await new Promise(resolve => setTimeout(resolve, 100));
      expect(screen.queryByText('Info message')).not.toBeInTheDocument();
    });

    it('suppresses success toasts from showToast when notifications disabled', async () => {
      const { showToast } = await import('./ToastContainer');
      render(<ToastContainer />);
      await new Promise(resolve => setTimeout(resolve, 50));

      showToast.show('Success message', 'success');

      await new Promise(resolve => setTimeout(resolve, 100));
      expect(screen.queryByText('Success message')).not.toBeInTheDocument();
    });
  });

  describe('Cleanup', () => {
    it('does not crash on unmount', () => {
      const { unmount } = render(<ToastContainer />);
      expect(() => unmount()).not.toThrow();
    });
  });
});
