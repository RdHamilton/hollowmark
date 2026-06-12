import { useState, useCallback, useEffect, useRef } from 'react';
import Toast from './Toast';
import { getReplayState, subscribeToReplayState } from '../App';
import { useSettings } from '../hooks/useSettings';

// Dead colon-vocabulary listeners (stats:updated, rank:updated, quest:updated,
// draft:updated, collection:updated) deleted per ADR-084 §G1 sweep — they had
// zero server-side emitters since the Wails→REST migration.
//
// Toast reintroduction on readmodel.updated requires a Prof PLAYER_VERDICT first
// (ADR-084 §Risks, AC8 of #1369). This component retains the toast infrastructure
// (addToast, showToast) so it is ready when that gate is passed.

interface ToastData {
  id: number;
  message: string;
  type: 'success' | 'info' | 'warning' | 'error';
}

let toastIdCounter = 0;

const ToastContainer = () => {
  const [toasts, setToasts] = useState<ToastData[]>([]);
  // isReplayActiveRef and draftUpdateCountRef/Timer are retained because the
  // toast infrastructure will re-use them once Prof's gate is passed.
  const isReplayActiveRef = useRef(getReplayState().isActive);
  const draftUpdateCountRef = useRef(0);
  const draftUpdateTimerRef = useRef<number | null>(null);
  // AC1/AC2 (#2024): read user's notification preference so success/info toasts
  // from game events can be suppressed. Error and warning toasts always show
  // because they are action feedback, not optional notifications.
  const { showNotifications } = useSettings();
  const showNotificationsRef = useRef(showNotifications);
  showNotificationsRef.current = showNotifications;

  const addToast = useCallback((message: string, type: 'success' | 'info' | 'warning' | 'error' = 'info') => {
    const id = toastIdCounter++;
    setToasts(prev => [...prev, { id, message, type }]);

    // Auto-remove toast after 5 seconds
    setTimeout(() => {
      setToasts(prev => prev.filter(toast => toast.id !== id));
    }, 5000);
  }, []);

  const removeToast = useCallback((id: number) => {
    setToasts(prev => prev.filter(toast => toast.id !== id));
  }, []);

  // Subscribe to replay state changes
  useEffect(() => {
    const unsubscribe = subscribeToReplayState((state) => {
      isReplayActiveRef.current = state.isActive;

      // Clear pending draft updates when replay stops
      if (!state.isActive) {
        draftUpdateCountRef.current = 0;
        if (draftUpdateTimerRef.current) {
          clearTimeout(draftUpdateTimerRef.current);
          draftUpdateTimerRef.current = null;
        }
      }
    });

    return unsubscribe;
  }, []);

  useEffect(() => {
    // Register global toast function — errors and warnings always fire;
    // success and info toasts respect the showNotifications preference.
    showToast.setAddFn((message, type = 'info') => {
      if (type === 'error' || type === 'warning' || showNotificationsRef.current) {
        addToast(message, type);
      }
    });
  }, [addToast]);

  return (
    <div style={{ position: 'fixed', bottom: '20px', right: '20px', zIndex: 10000, display: 'flex', flexDirection: 'column-reverse' }}>
      {toasts.map((toast) => (
        <Toast
          key={toast.id}
          message={toast.message}
          type={toast.type}
          onClose={() => removeToast(toast.id)}
        />
      ))}
    </div>
  );
};

// Export the global toast function for use in other components
// eslint-disable-next-line react-refresh/only-export-components
export const showToast = (() => {
  let addToastFn: ((message: string, type?: 'success' | 'info' | 'warning' | 'error') => void) | null = null;

  return {
    setAddFn: (fn: (message: string, type?: 'success' | 'info' | 'warning' | 'error') => void) => {
      addToastFn = fn;
    },
    show: (message: string, type: 'success' | 'info' | 'warning' | 'error' = 'info') => {
      if (addToastFn) {
        addToastFn(message, type);
      }
    }
  };
})();

export default ToastContainer;
