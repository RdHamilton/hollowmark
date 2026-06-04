import { useState, useRef, useEffect } from 'react';
import { useUser, useAuth } from '@clerk/react';
import { useNavigate } from 'react-router-dom';
import { getHomeSummary } from '../services/api/bffHomeSummary';
import type { HomeSummaryResponse } from '../services/api/bffHomeSummary';
import { winRateColor } from './homeUtils';
import './Profile.css';

/**
 * Profile page — dedicated route at /profile for viewing and editing the
 * authenticated Clerk user's identity (display name, avatar, email).
 *
 * Auth state is sourced exclusively from useUser() per ADR-009 and CLAUDE.md.
 * Mutations use user.update() and user.setProfileImage() from the Clerk SDK.
 * No auth state is duplicated in Redux / Context / Zustand.
 */

export interface ProfilePageProps {
  /** Dependency-injected hook for tests — defaults to useUser() from @clerk/react. */
  useUserHook?: () => {
    isLoaded: boolean;
    isSignedIn: boolean | undefined;
    user: {
      id: string;
      fullName: string | null;
      firstName: string | null;
      lastName: string | null;
      primaryEmailAddress?: { emailAddress: string } | null;
      imageUrl: string;
      update: (params: { firstName?: string; lastName?: string }) => Promise<unknown>;
      setProfileImage: (params: { file: File | null }) => Promise<unknown>;
    } | null;
  };
}


/** Interface for format breakdown entry */
interface FormatBreakdownEntry {
  format: string;
  win_rate: number;
  matches: number;
}

/**
 * Detect whether imageUrl is the Clerk default avatar.
 */
function isClerkDefaultAvatar(url: string): boolean {
  if (!url) return true;
  return (
    url.includes('gravatar.com') ||
    url.includes('img.clerk.com') ||
    url.includes('clerk.dev/default') ||
    url.includes('clerk.com/default')
  );
}

function useDefaultUser() {
  const { isLoaded, isSignedIn, user } = useUser();
  // Coerce undefined → null so the return type matches the useUserHook prop shape,
  // which only allows null (not undefined) for user. useUser() returns undefined
  // while loading but our prop interface uses null as the "no user" sentinel.
  return { isLoaded, isSignedIn, user: user ?? null };
}

const Profile = ({ useUserHook = useDefaultUser }: ProfilePageProps) => {
  const navigate = useNavigate();
  const { isLoaded, isSignedIn, user } = useUserHook();
  const { getToken } = useAuth();

  // Display name editing state
  const [isEditingName, setIsEditingName] = useState(false);
  const [firstName, setFirstName] = useState('');
  const [lastName, setLastName] = useState('');
  const [nameSaving, setNameSaving] = useState(false);
  const [nameError, setNameError] = useState<string | null>(null);
  const [nameSuccess, setNameSuccess] = useState(false);

  // Avatar upload state
  const [avatarUploading, setAvatarUploading] = useState(false);
  const [avatarError, setAvatarError] = useState<string | null>(null);
  const [avatarSuccess, setAvatarSuccess] = useState(false);
  const avatarInputRef = useRef<HTMLInputElement>(null);

  // Enrichment data state
  const [summaryData, setSummaryData] = useState<HomeSummaryResponse | null>(null);
  const [summaryLoading, setSummaryLoading] = useState(false);
  const [summaryTimedOut, setSummaryTimedOut] = useState(false);

  // Auto-dismiss success banners after 3 s; clear timer on unmount to prevent
  // state updates on an unmounted component.
  useEffect(() => {
    if (!nameSuccess) return;
    const timerId = setTimeout(() => setNameSuccess(false), 3000);
    return () => clearTimeout(timerId);
  }, [nameSuccess]);

  useEffect(() => {
    if (!avatarSuccess) return;
    const timerId = setTimeout(() => setAvatarSuccess(false), 3000);
    return () => clearTimeout(timerId);
  }, [avatarSuccess]);


  // Load enrichment data — all-time stats for the stats chips
  useEffect(() => {
    if (!isSignedIn) return;
    let cancelled = false;
    const timeoutId = setTimeout(() => {
      if (!cancelled) setSummaryTimedOut(true);
    }, 2000);

    setSummaryLoading(true);
    getToken()
      .then((token) => {
        if (!token || cancelled) return null;
        return getHomeSummary(token);
      })
      .then((data) => {
        if (!cancelled && data) {
          setSummaryData(data);
        }
      })
      .catch(() => {
        // Non-fatal — show no-data state
      })
      .finally(() => {
        if (!cancelled) setSummaryLoading(false);
      });

    return () => {
      cancelled = true;
      clearTimeout(timeoutId);
    };
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isSignedIn]);

  const handleEditNameStart = () => {
    setFirstName(user?.firstName ?? '');
    setLastName(user?.lastName ?? '');
    setNameError(null);
    setNameSuccess(false);
    setIsEditingName(true);
  };

  const handleEditNameCancel = () => {
    setIsEditingName(false);
    setNameError(null);
  };

  const handleSaveName = async () => {
    if (!user) return;
    setNameSaving(true);
    setNameError(null);
    try {
      await user.update({ firstName: firstName.trim(), lastName: lastName.trim() });
      setIsEditingName(false);
      setNameSuccess(true);
    } catch (err) {
      setNameError(err instanceof Error ? err.message : 'Failed to update display name.');
    } finally {
      setNameSaving(false);
    }
  };

  const handleAvatarChange = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file || !user) return;

    setAvatarUploading(true);
    setAvatarError(null);
    setAvatarSuccess(false);
    try {
      await user.setProfileImage({ file });
      setAvatarSuccess(true);
    } catch (err) {
      setAvatarError(err instanceof Error ? err.message : 'Failed to upload avatar.');
    } finally {
      setAvatarUploading(false);
      // Reset file input so the same file can be picked again if needed
      if (avatarInputRef.current) {
        avatarInputRef.current.value = '';
      }
    }
  };

  // --- Loading state ---
  if (!isLoaded) {
    return (
      <div className="page-container profile-page" data-testid="profile-page">
        <div
          className="profile-loading"
          data-testid="profile-loading"
          aria-live="polite"
          aria-busy="true"
        >
          Loading profile…
        </div>
      </div>
    );
  }

  // --- Unauthenticated (should not happen — route is protected) ---
  if (!isSignedIn || !user) {
    return (
      <div className="page-container profile-page" data-testid="profile-page">
        <div className="profile-unauthenticated" data-testid="profile-unauthenticated">
          You must be signed in to view your profile.
        </div>
      </div>
    );
  }

  return (
    <div className="page-container profile-page" data-testid="profile-page">
      <div className="profile-header">
        <button
          className="profile-back-button"
          data-testid="profile-back-button"
          onClick={() => navigate(-1)}
          aria-label="Go back"
        >
          ← Back
        </button>
        <h1 className="page-title" data-testid="profile-title">
          Profile
        </h1>
      </div>

      <div className="profile-content">
        {/* --- Avatar section --- */}
        <section className="profile-section" data-testid="profile-avatar-section">
          <h2 className="profile-section-title">Avatar</h2>
          <div className="profile-avatar-container">
            {user.imageUrl && !isClerkDefaultAvatar(user.imageUrl) ? (
              <img
                className="profile-avatar"
                data-testid="profile-avatar"
                src={user.imageUrl}
                alt={user.fullName ?? 'User avatar'}
              />
            ) : (
              /* VaultMTG branded initial-letter placeholder — replaces Clerk default */
              <div
                className="profile-avatar-vault-initial"
                data-testid="profile-avatar-vault-initial"
                aria-label={`Avatar for ${user.firstName ?? user.fullName ?? 'user'}`}
                style={{
                  width: 64,
                  height: 64,
                  borderRadius: '50%',
                  background: 'var(--vault-sapphire)',
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  flexShrink: 0,
                }}
              >
                <span
                  style={{
                    fontFamily: 'var(--font-display-vault)',
                    fontSize: 28,
                    fontWeight: 700,
                    color: '#0D1117',
                    textTransform: 'uppercase',
                    lineHeight: 1,
                  }}
                >
                  {(user.firstName?.[0] ?? user.fullName?.[0] ?? '?').toUpperCase()}
                </span>
              </div>
            )}
            <div className="profile-avatar-actions">
              <button
                className="secondary-button profile-avatar-upload-button"
                data-testid="profile-avatar-upload-button"
                onClick={() => avatarInputRef.current?.click()}
                disabled={avatarUploading}
                aria-label="Upload new avatar"
              >
                {avatarUploading ? 'Uploading…' : 'Change Avatar'}
              </button>
              <input
                ref={avatarInputRef}
                type="file"
                accept="image/*"
                className="profile-avatar-input"
                data-testid="profile-avatar-input"
                onChange={handleAvatarChange}
                aria-label="Select avatar image"
              />
            </div>
          </div>
          {avatarError && (
            <div className="profile-error" data-testid="profile-avatar-error" role="alert">
              {avatarError}
            </div>
          )}
          {avatarSuccess && (
            <div className="profile-success" data-testid="profile-avatar-success" role="status">
              Avatar updated successfully!
            </div>
          )}
        </section>

        {/* --- Display name section --- */}
        <section className="profile-section" data-testid="profile-name-section">
          <h2 className="profile-section-title">Display Name</h2>
          {isEditingName ? (
            <div className="profile-name-form" data-testid="profile-name-form">
              <div className="profile-name-fields">
                <label className="profile-field-label" htmlFor="profile-first-name">
                  First Name
                  <input
                    id="profile-first-name"
                    className="profile-field-input"
                    data-testid="profile-first-name-input"
                    type="text"
                    value={firstName}
                    onChange={(e) => setFirstName(e.target.value)}
                    placeholder="First name"
                    autoFocus
                  />
                </label>
                <label className="profile-field-label" htmlFor="profile-last-name">
                  Last Name
                  <input
                    id="profile-last-name"
                    className="profile-field-input"
                    data-testid="profile-last-name-input"
                    type="text"
                    value={lastName}
                    onChange={(e) => setLastName(e.target.value)}
                    placeholder="Last name"
                  />
                </label>
              </div>
              <div className="profile-name-actions">
                <button
                  className="primary-button"
                  data-testid="profile-save-name-button"
                  onClick={handleSaveName}
                  disabled={nameSaving}
                >
                  {nameSaving ? 'Saving…' : 'Save'}
                </button>
                <button
                  className="secondary-button"
                  data-testid="profile-cancel-name-button"
                  onClick={handleEditNameCancel}
                  disabled={nameSaving}
                >
                  Cancel
                </button>
              </div>
              {nameError && (
                <div className="profile-error" data-testid="profile-name-error" role="alert">
                  {nameError}
                </div>
              )}
            </div>
          ) : (
            <div className="profile-name-display" data-testid="profile-name-display">
              <span className="profile-name-value" data-testid="profile-name-value">
                {user.fullName ?? '—'}
              </span>
              <button
                className="secondary-button profile-edit-button"
                data-testid="profile-edit-name-button"
                onClick={handleEditNameStart}
                aria-label="Edit display name"
              >
                Edit
              </button>
            </div>
          )}
          {nameSuccess && (
            <div className="profile-success" data-testid="profile-name-success" role="status">
              Display name updated successfully!
            </div>
          )}
        </section>

        {/* --- Email section --- */}
        <section className="profile-section" data-testid="profile-email-section">
          <h2 className="profile-section-title">Email</h2>
          <div className="profile-email-display" data-testid="profile-email-display">
            <span className="profile-email-value" data-testid="profile-email-value">
              {user.primaryEmailAddress?.emailAddress ?? '—'}
            </span>
            <p className="profile-email-note">
              Email is managed by your Clerk account and cannot be changed here.
            </p>
          </div>
        </section>


        {/* --- Arena username section --- */}
        <section className="profile-section" data-testid="profile-arena-section">
          <h2 className="profile-section-title" style={{ fontSize: 'var(--text-xs)', textTransform: 'uppercase', letterSpacing: '0.1em', color: 'var(--vault-fg-secondary)' }}>
            Arena Username
          </h2>
          {(() => {
            const arenaUsername = (summaryData as (HomeSummaryResponse & { arena_username?: string }) | null)?.arena_username;
            if (arenaUsername) {
              return (
                <div className="profile-arena-username" data-testid="profile-arena-username">
                  <span style={{ fontSize: 'var(--text-base)', color: 'var(--vault-fg)', fontFamily: 'var(--font-body)' }}>
                    {arenaUsername}
                  </span>
                  <span style={{ fontSize: 'var(--text-xs)', color: 'var(--vault-fg-muted)', marginLeft: 8 }}>
                    Detected from game
                  </span>
                </div>
              );
            }
            return (
              <p style={{ fontSize: 'var(--text-sm)', color: 'var(--vault-fg-muted)' }} data-testid="profile-arena-placeholder">
                Arena account not yet detected. Connect the daemon and play a match.
              </p>
            );
          })()}
        </section>

        {/* --- All-time stats section --- */}
        <section className="profile-section" data-testid="profile-stats-section">
          <h2 className="profile-section-title">All-Time Record</h2>
          {summaryLoading && !summaryTimedOut ? (
            <p style={{ fontSize: 'var(--text-sm)', color: 'var(--vault-fg-muted)' }}>Loading stats…</p>
          ) : summaryData && summaryData.all_time?.matches > 0 ? (
            <div className="profile-stats-chips" data-testid="profile-stats-chips" style={{ display: 'flex', gap: 'var(--space-3)', flexWrap: 'wrap' }}>
              <div style={{ background: 'var(--vault-bg-raised)', border: '1px solid var(--vault-border)', borderRadius: 'var(--radius-md)', padding: 'var(--space-3) var(--space-4)', display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: 'var(--text-xl)', fontWeight: 600, color: 'var(--vault-success)' }} data-testid="profile-stat-wins">
                  {summaryData.all_time.wins}
                </span>
                <span style={{ fontFamily: 'var(--font-body)', fontSize: 'var(--text-xs)', color: 'var(--vault-fg-muted)' }}>Wins</span>
              </div>
              <div style={{ background: 'var(--vault-bg-raised)', border: '1px solid var(--vault-border)', borderRadius: 'var(--radius-md)', padding: 'var(--space-3) var(--space-4)', display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: 'var(--text-xl)', fontWeight: 600, color: 'var(--vault-danger)' }} data-testid="profile-stat-losses">
                  {summaryData.all_time.losses}
                </span>
                <span style={{ fontFamily: 'var(--font-body)', fontSize: 'var(--text-xs)', color: 'var(--vault-fg-muted)' }}>Losses</span>
              </div>
              <div style={{ background: 'var(--vault-bg-raised)', border: '1px solid var(--vault-border)', borderRadius: 'var(--radius-md)', padding: 'var(--space-3) var(--space-4)', display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: 'var(--text-xl)', fontWeight: 600, color: winRateColor(summaryData.all_time.win_rate) }} data-testid="profile-stat-winrate">
                  {(summaryData.all_time.win_rate * 100).toFixed(1)}%
                </span>
                <span style={{ fontFamily: 'var(--font-body)', fontSize: 'var(--text-xs)', color: 'var(--vault-fg-muted)' }}>Win Rate</span>
              </div>
            </div>
          ) : (
            <p style={{ fontSize: 'var(--text-sm)', color: 'var(--vault-fg-muted)' }} data-testid="profile-stats-empty">
              No match data yet.
            </p>
          )}
        </section>

        {/* --- Favorite format section — only shown when format_breakdown is available --- */}
        {(() => {
          const formatBreakdown = (summaryData as (HomeSummaryResponse & { format_breakdown?: FormatBreakdownEntry[] }) | null)?.format_breakdown;
          if (!formatBreakdown || formatBreakdown.length === 0) return null;
          const topFormat = formatBreakdown.reduce((a, b) => (b.matches > a.matches ? b : a));
          return (
            <section className="profile-section" data-testid="profile-favorite-format-section">
              <h2 className="profile-section-title">Favorite Format</h2>
              <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-2)' }}>
                <span
                  data-testid="profile-format-badge"
                  style={{
                    background: 'var(--vault-bg-overlay)',
                    border: '1px solid var(--vault-border)',
                    borderRadius: 'var(--radius-sm)',
                    padding: '2px 8px',
                    fontSize: 'var(--text-xs)',
                    color: 'var(--vault-fg-secondary)',
                    fontFamily: 'var(--font-body)',
                  }}
                >
                  {topFormat.format}
                </span>
                <span style={{ fontSize: 'var(--text-xs)', color: 'var(--vault-fg-muted)', marginLeft: 8 }}>
                  {topFormat.matches} matches
                </span>
              </div>
            </section>
          );
        })()}
      </div>
    </div>
  );
};

export default Profile;
