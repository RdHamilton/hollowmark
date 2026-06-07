import { useState, useCallback, useEffect, useRef } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useAuth } from '@clerk/react';
import Footer from './Footer';
import AuthBar from './AuthBar';
import DaemonHealthIndicator, { type DaemonHealthState } from './DaemonHealthIndicator';
import { OnboardingModal } from './OnboardingModal';
import { usePostHogIdentity } from '@/hooks/usePostHogIdentity';
import { useDaemonOnboarding, type AccountDataState, SESSION_HAS_ACCOUNT_DATA_KEY } from '@/hooks/useDaemonOnboarding';
import ReportBugButton from './ReportBugButton';
import vaultMark from '@/assets/logo-vaultmtg-mark.svg';
import { getHomeSummary } from '@/services/api/bffHomeSummary';
import './Layout.css';

interface LayoutProps {
  children: React.ReactNode;
}

const Layout = ({ children }: LayoutProps) => {
  const location = useLocation();
  const { isSignedIn, getToken } = useAuth();
  // Identify signed-in user with PostHog and fire funnel_sign_up_completed once per session.
  usePostHogIdentity();

  // Track daemon health status from the indicator so the onboarding hook can use it.
  const [daemonStatus, setDaemonStatus] = useState<DaemonHealthState>('loading');

  // Tri-state account data check.
  //
  // 'pending'  — fetch not yet started or in flight; blocks the modal (fail-closed)
  // 'has-data' — summary confirmed all_time.matches > 0; returning user, never show modal
  // 'empty'    — summary positively confirmed 0 matches; genuine new user candidate
  //
  // Seeded from sessionStorage so that route navigation does not re-fetch on every
  // Layout mount. Sign-out clears the session entry (see effect below).
  const [accountDataState, setAccountDataState] = useState<AccountDataState>(() => {
    try {
      return sessionStorage.getItem(SESSION_HAS_ACCOUNT_DATA_KEY) === 'true'
        ? 'has-data'
        : 'pending';
    } catch {
      return 'pending';
    }
  });

  // Guards the one-per-session fetch so it never fires twice (regardless of
  // how many times Layout re-renders or the user navigates).
  const dataCheckDoneRef = useRef<boolean>(
    (() => {
      try {
        return sessionStorage.getItem(SESSION_HAS_ACCOUNT_DATA_KEY) === 'true';
      } catch {
        return false;
      }
    })()
  );

  // Fire getHomeSummary as soon as the user is signed in — do NOT gate on
  // daemonStatus.  Gating on daemonStatus reintroduces the original async race:
  // daemonStatus can resolve before or after the summary fetch, making the
  // 'pending' guard unreliable.
  useEffect(() => {
    if (!isSignedIn || dataCheckDoneRef.current) return;
    dataCheckDoneRef.current = true;

    const checkAccountData = async () => {
      try {
        const token = await getToken();
        if (!token) {
          // No token yet — keep 'pending' so the modal stays blocked.
          return;
        }
        const summary = await getHomeSummary(token);
        if (summary.all_time.matches > 0) {
          // Returning user confirmed — persist to sessionStorage so navigation
          // does not re-race on the next Layout mount.
          try {
            sessionStorage.setItem(SESSION_HAS_ACCOUNT_DATA_KEY, 'true');
          } catch {
            // ignore storage errors
          }
          setAccountDataState('has-data');
        } else {
          // Positively confirmed zero — genuine new-user candidate.
          // Do NOT write sessionStorage for 'empty'; only 'has-data' is cached.
          setAccountDataState('empty');
        }
      } catch {
        // Network error or BFF unavailable → stay 'pending' (fail-closed).
        // Never show the modal when account data state is uncertain.
        // The user can still manually open via the daemon status indicator.
      }
    };

    void checkAccountData();
  }, [isSignedIn, getToken]);

  // Reset on sign-out: if the user signs out within the same tab, clear the
  // ref and the session entry so that a subsequent sign-in re-fetches instead
  // of inheriting the prior session's state.
  //
  // setAccountDataState('pending') is intentionally omitted here — calling
  // setState synchronously inside an effect triggers react-hooks/set-state-in-effect.
  // It is also unnecessary: autoShow requires isSignedIn === true, so a stale
  // signed-out accountDataState value cannot trigger the modal.  dataCheckDoneRef
  // reset guarantees the next sign-in re-fetches from scratch.
  useEffect(() => {
    if (isSignedIn) return; // only act on the transition to signed-out
    dataCheckDoneRef.current = false;
    try {
      sessionStorage.removeItem(SESSION_HAS_ACCOUNT_DATA_KEY);
    } catch {
      // ignore storage errors
    }
  }, [isSignedIn]);

  // Onboarding modal logic: autoShow fires ONLY when accountDataState === 'empty'
  // AND daemonStatus === 'disconnected'.  Both 'pending' and 'has-data' block it.
  const { isOpen: onboardingOpen, open: openOnboarding, dismiss: dismissOnboarding, complete: completeOnboarding } =
    useDaemonOnboarding(daemonStatus, isSignedIn ?? false, accountDataState);

  const handleDaemonStatusChange = useCallback((status: DaemonHealthState) => {
    setDaemonStatus(status);
  }, []);

  const isActive = (path: string) => location.pathname === path;

  // Derive activeTab from current route (computed value, not state)
  const getActiveTab = (): 'home' | 'match-history' | 'quests' | 'draft' | 'decks' | 'collection' | 'meta' | 'charts' | 'download' | 'profile' => {
    if (location.pathname === '/home' || location.pathname === '/') {
      return 'home';
    } else if (location.pathname === '/match-history') {
      return 'match-history';
    } else if (location.pathname === '/quests') {
      return 'quests';
    } else if (
      location.pathname === '/draft' ||
      location.pathname === '/draft-analytics' ||
      location.pathname === '/draft/live' ||
      location.pathname === '/history/drafts'
    ) {
      return 'draft';
    } else if (location.pathname === '/decks' || location.pathname.startsWith('/deck-builder')) {
      return 'decks';
    } else if (location.pathname === '/collection') {
      return 'collection';
    } else if (location.pathname === '/meta') {
      return 'meta';
    } else if (location.pathname.startsWith('/charts/')) {
      return 'charts';
    } else if (location.pathname === '/download') {
      return 'download';
    } else if (location.pathname === '/profile') {
      return 'profile';
    }
    return 'match-history';
  };

  const activeTab = getActiveTab();



  return (
    <div className="app-container" data-testid="app-container">
      {/* Top Navigation Tabs */}
      <div className="tab-bar" data-testid="nav-tab-bar">
        <div className="tab-bar-left">
          <Link to="/home" className="nav-brand" data-testid="nav-brand" aria-label="Hollowmark home">
            {/* #1020: Hollowmark orb mark ≥32px per design spec */}
            <img src={vaultMark} alt="" width={32} height={32} className="nav-brand-mark" />
            <span className="nav-brand-wordmark">Hollowmark</span>
          </Link>
          <div className="tab-links">
          <Link
            to="/home"
            className={`tab ${activeTab === 'home' ? 'active' : ''}`}
            data-testid="nav-tab-home"
          >
            Home
          </Link>
          <Link
            to="/match-history"
            className={`tab ${activeTab === 'match-history' ? 'active' : ''}`}
            data-testid="nav-tab-match-history"
          >
            Match History
          </Link>
          <Link
            to="/quests"
            className={`tab ${activeTab === 'quests' ? 'active' : ''}`}
            data-testid="nav-tab-quests"
          >
            Quests
          </Link>
          <Link
            to="/draft"
            className={`tab ${activeTab === 'draft' ? 'active' : ''}`}
            data-testid="nav-tab-draft"
          >
            Draft
          </Link>
          <Link
            to="/decks"
            className={`tab ${activeTab === 'decks' ? 'active' : ''}`}
            data-testid="nav-tab-decks"
          >
            Decks
          </Link>
          <Link
            to="/collection"
            className={`tab ${activeTab === 'collection' ? 'active' : ''}`}
            data-testid="nav-tab-collection"
          >
            Collection
          </Link>
          <Link
            to="/meta"
            className={`tab ${activeTab === 'meta' ? 'active' : ''}`}
            data-testid="nav-tab-meta"
          >
            Meta
          </Link>
          <Link
            to="/charts/win-rate-trend"
            className={`tab ${activeTab === 'charts' ? 'active' : ''}`}
            data-testid="nav-tab-charts"
          >
            Charts
          </Link>
          <Link
            to="/download"
            className={`tab ${activeTab === 'download' ? 'active' : ''}`}
            data-testid="nav-tab-download"
          >
            Download
          </Link>
          <Link
            to="/profile"
            className={`tab ${activeTab === 'profile' ? 'active' : ''}`}
            data-testid="nav-tab-profile"
          >
            Profile
          </Link>
          <Link
            to="/settings"
            className={`tab ${isActive('/settings') ? 'active' : ''}`}
            data-testid="nav-tab-settings"
          >
            Settings
          </Link>
          </div>
        </div>
        <div className="tab-bar-right">
          {isSignedIn && <ReportBugButton />}
          <AuthBar />
          <div className="connection-status-indicator">
            <DaemonHealthIndicator
              onOpenOnboarding={openOnboarding}
              onStatusChange={handleDaemonStatusChange}
            />
          </div>
        </div>
      </div>

      {/* Sub-navigation for Draft */}
      {activeTab === 'draft' && (
        <div className="sub-tab-bar" data-testid="draft-sub-tab-bar">
          <Link
            to="/draft"
            className={`sub-tab ${isActive('/draft') ? 'active' : ''}`}
            data-testid="sub-tab-current-draft"
          >
            Current Draft
          </Link>
          <Link
            to="/draft-analytics"
            className={`sub-tab ${isActive('/draft-analytics') ? 'active' : ''}`}
            data-testid="sub-tab-analytics"
          >
            Analytics
          </Link>
        </div>
      )}

      {/* Sub-navigation for Charts */}
      {activeTab === 'charts' && (
        <div className="sub-tab-bar" data-testid="charts-sub-tab-bar">
          <Link
            to="/charts/win-rate-trend"
            className={`sub-tab ${isActive('/charts/win-rate-trend') ? 'active' : ''}`}
          >
            Win Rate Trend
          </Link>
          <Link
            to="/charts/deck-performance"
            className={`sub-tab ${isActive('/charts/deck-performance') ? 'active' : ''}`}
          >
            Deck Performance
          </Link>
          <Link
            to="/charts/rank-progression"
            className={`sub-tab ${isActive('/charts/rank-progression') ? 'active' : ''}`}
          >
            Rank Progression
          </Link>
          <Link
            to="/charts/format-distribution"
            className={`sub-tab ${isActive('/charts/format-distribution') ? 'active' : ''}`}
          >
            Format Distribution
          </Link>
          <Link
            to="/charts/result-breakdown"
            className={`sub-tab ${isActive('/charts/result-breakdown') ? 'active' : ''}`}
          >
            Result Breakdown
          </Link>
        </div>
      )}

      {/* Main Content */}
      <div className="content" data-testid="main-content">
        {children}
      </div>

      {/* Footer with Stats */}
      <Footer />

      {/* Daemon onboarding modal — shown on first login if daemon not connected
          and account has no existing data (accountDataState === 'empty').
          'pending' and 'has-data' both suppress the modal. */}
      <OnboardingModal
        isOpen={onboardingOpen}
        onDismiss={dismissOnboarding}
        onComplete={completeOnboarding}
      />
    </div>
  );
};

export default Layout;
