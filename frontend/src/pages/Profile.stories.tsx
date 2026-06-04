import type { Meta, StoryObj } from '@storybook/react';
import { withRouter } from '../../.storybook/decorators';
import Profile from './Profile';
import './Profile.css';

/**
 * Profile — dedicated page at `/profile` for viewing and editing the
 * authenticated user's identity (display name, avatar, email) (#2025).
 *
 * Auth state is sourced via the `useUserHook` prop (DI pattern per ADR-009)
 * so stories exercise every auth and loading state without a real Clerk
 * session or network call.
 *
 * Decorators:
 *  - withRouter — required because the page calls `useNavigate(-1)` for the
 *    Back button.
 */
const meta: Meta<typeof Profile> = {
  title: 'Organisms/Profile',
  component: Profile,
  decorators: [withRouter],
  parameters: {
    layout: 'fullscreen',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof Profile>;

const mockUser = {
  id: 'user_storybook_mock',
  firstName: 'Planeswalker',
  lastName: 'Mock',
  fullName: 'Planeswalker Mock',
  primaryEmailAddress: { emailAddress: 'planeswalker@vaultmtg.test' },
  imageUrl: '',
  update: async () => {},
  setProfileImage: async () => {},
};

/**
 * Loaded, signed-in user — the full profile editor is visible.
 */
export const Authenticated: Story = {
  args: {
    useUserHook: () => ({
      isLoaded: true,
      isSignedIn: true,
      user: mockUser,
    }),
  },
};

/**
 * Signed-in user with an avatar image URL.
 */
export const AuthenticatedWithAvatar: Story = {
  args: {
    useUserHook: () => ({
      isLoaded: true,
      isSignedIn: true,
      user: {
        ...mockUser,
        imageUrl: 'https://api.dicebear.com/7.x/bottts/svg?seed=vaultmtg',
      },
    }),
  },
};

/**
 * Clerk session is still loading — shows the loading spinner/message.
 */
export const Loading: Story = {
  args: {
    useUserHook: () => ({
      isLoaded: false,
      isSignedIn: undefined,
      user: null,
    }),
  },
};

/**
 * User is not signed in — shows the unauthenticated fallback message.
 * In production this state should not be reachable (ProtectedRoute guards /profile).
 */
export const SignedOut: Story = {
  args: {
    useUserHook: () => ({
      isLoaded: true,
      isSignedIn: false,
      user: null,
    }),
  },
};

// ─── Profile enrichment stories (v0.3.7 anti-slop) ─────────────────────────

// Mock hook that injects enriched user data
function makeEnrichedHook(overrides: Partial<{
  imageUrl: string;
  firstName: string;
}> = {}) {
  return () => ({
    isLoaded: true,
    isSignedIn: true as const,
    user: {
      id: 'user_enriched',
      fullName: 'Alex Stormwind',
      firstName: overrides.firstName ?? 'Alex',
      lastName: 'Stormwind',
      primaryEmailAddress: { emailAddress: 'alex@example.com' },
      imageUrl: overrides.imageUrl ?? 'https://img.clerk.com/default',
      username: 'AlexS',
      update: async () => {},
      setProfileImage: async () => {},
    },
  });
}

export const WithArenaUsername: Story = {
  name: 'Enrichment — With Arena Username',
  render: () => (
    <div style={{ background: '#0D1117', padding: 24, minHeight: '50vh' }}>
      <p style={{ color: '#94A3B8', fontSize: 12, marginBottom: 12 }}>
        Arena username detected from game session.
      </p>
      <div style={{ background: '#161C26', borderRadius: 8, padding: 16 }}>
        <h2 style={{ color: '#F1F5F9', fontFamily: 'sans-serif', fontSize: 13, marginBottom: 8, textTransform: 'uppercase', letterSpacing: '0.1em' }}>Arena Username</h2>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ color: '#F1F5F9', fontFamily: 'sans-serif', fontSize: 15 }}>Planeswalker#12345</span>
          <span style={{ color: '#7890AA', fontSize: 12 }}>Detected from game</span>
        </div>
      </div>
    </div>
  ),
};

export const WithoutArenaUsername: Story = {
  name: 'Enrichment — Without Arena Username (placeholder)',
  render: () => (
    <div style={{ background: '#0D1117', padding: 24, minHeight: '50vh' }}>
      <p style={{ color: '#94A3B8', fontSize: 12, marginBottom: 12 }}>
        No arena username detected yet.
      </p>
      <div style={{ background: '#161C26', borderRadius: 8, padding: 16 }}>
        <h2 style={{ color: '#F1F5F9', fontFamily: 'sans-serif', fontSize: 13, marginBottom: 8, textTransform: 'uppercase', letterSpacing: '0.1em' }}>Arena Username</h2>
        <p style={{ color: '#7890AA', fontSize: 13 }}>Arena account not yet detected. Connect the daemon and play a match.</p>
      </div>
    </div>
  ),
};

export const WithAllTimeStats: Story = {
  name: 'Enrichment — All-Time Stats (142W / 98L / 59.2%)',
  render: () => (
    <div style={{ background: '#0D1117', padding: 24, minHeight: '50vh' }}>
      <div style={{ background: '#161C26', borderRadius: 8, padding: 16 }}>
        <h2 style={{ color: '#F1F5F9', fontFamily: 'sans-serif', fontSize: 15, marginBottom: 12 }}>All-Time Record</h2>
        <div style={{ display: 'flex', gap: 12 }}>
          {[
            { value: '142', label: 'Wins', color: '#22C55E' },
            { value: '98', label: 'Losses', color: '#EF4444' },
            { value: '59.2%', label: 'Win Rate', color: '#4A90D9' },
          ].map(({ value, label, color }) => (
            <div key={label} style={{ background: '#0D1117', border: '1px solid #2A3347', borderRadius: 8, padding: '12px 16px', display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
              <span style={{ fontFamily: 'monospace', fontSize: 20, fontWeight: 600, color }}>{value}</span>
              <span style={{ fontFamily: 'sans-serif', fontSize: 11, color: '#7890AA' }}>{label}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  ),
};

export const WithDefaultAvatar: Story = {
  name: 'Enrichment — VaultMTG Initial Placeholder (Clerk default replaced)',
  args: {
    useUserHook: makeEnrichedHook({ imageUrl: 'https://img.clerk.com/default' }),
  },
};

export const FullyEnriched: Story = {
  name: 'Enrichment — Fully Enriched (all three sections)',
  args: {
    useUserHook: makeEnrichedHook({ imageUrl: 'https://img.clerk.com/default', firstName: 'Alex' }),
  },
};
