/**
 * ConsentGate — blocks app entry until the COPPA/ToS consent log row is
 * written to the server (COPPA #884).
 *
 * Mounted inside ProtectedRoute.tsx (post-auth concern — only runs for
 * signed-in users; the BFF endpoint requires a Clerk session JWT).
 *
 * States:
 *   idle / done — renders children normally (pass-through for returning users
 *     and for unauthenticated users where the hook returns idle).
 *   loading     — renders a full-screen loading state; children hidden.
 *   error       — renders a compliance error notice with a Retry button;
 *     children hidden (a failed consent write is a compliance failure, not
 *     a soft error to pass through silently).
 */

import LoadingSpinner from './LoadingSpinner';
import { useSignupConsentRecorder } from '@/hooks/useSignupConsentRecorder';
import './ConsentGate.css';

interface ConsentGateProps {
  children: React.ReactNode;
}

const ConsentGate = ({ children }: ConsentGateProps) => {
  const { status, retry } = useSignupConsentRecorder();

  if (status === 'loading') {
    return (
      <div className="consent-gate-loading" data-testid="consent-gate-loading">
        <LoadingSpinner message="Completing account setup..." />
      </div>
    );
  }

  if (status === 'error') {
    return (
      <div className="consent-gate-error" data-testid="consent-gate-error">
        <div className="consent-gate-error-card">
          <p className="consent-gate-error-title">Account setup incomplete</p>
          <p className="consent-gate-error-body">
            We could not record your Terms of Service acceptance. Please retry —
            access requires this step for compliance.
          </p>
          <button
            className="consent-gate-retry-btn"
            data-testid="consent-gate-retry-btn"
            onClick={retry}
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  // 'idle' or 'done' — render children normally
  return <>{children}</>;
};

export default ConsentGate;
