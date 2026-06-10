/**
 * Profile.email.test.tsx — Email-change state machine tests (#888)
 *
 * Tests the three-step Clerk email-verification flow added by #888:
 *   Step 1 — "idle"     : Edit Email button visible, read-only email display
 *   Step 2 — "pending"  : New-email input + Submit button; triggers createEmailAddress
 *                         + prepareVerification after successful entry
 *   Step 3 — "verifying": OTP code input + Verify button; triggers attemptVerification
 *                         then user.update(primaryEmailAddressId) + patchAccountProfile()
 *
 * Also tests:
 *   - PostHog disclosure text (AC4) — always-visible, not gated on flow state
 *   - BFF adapter called after successful Clerk mutation (non-blocking)
 *   - Error states in each step
 *   - Cancel returns to idle from both pending and verifying steps
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor, fireEvent, act } from '@testing-library/react';
import { renderWithRouter } from '@/test/utils/testUtils';
import Profile from './Profile';
import type { ProfilePageProps } from './Profile';

// ---------------------------------------------------------------------------
// Mock the BFF adapter (non-blocking call after Clerk mutation)
// ---------------------------------------------------------------------------

vi.mock('../services/api/accountProfile', () => ({
  patchAccountProfile: vi.fn().mockResolvedValue(undefined),
}));

import { patchAccountProfile } from '../services/api/accountProfile';
const mockPatchAccountProfile = vi.mocked(patchAccountProfile);

// ---------------------------------------------------------------------------
// Helpers — user stub factory
// ---------------------------------------------------------------------------

type UserStub = NonNullable<ReturnType<NonNullable<ProfilePageProps['useUserHook']>>['user']>;

/** Minimal EmailAddressResource stub for the test scenarios. */
const makeEmailAddressStub = (overrides: Partial<{
  id: string;
  emailAddress: string;
  prepareVerification: () => Promise<unknown>;
  attemptVerification: (p: { code: string }) => Promise<unknown>;
}> = {}) => ({
  id: 'email_new_123',
  emailAddress: 'newemail@example.com',
  prepareVerification: vi.fn().mockResolvedValue(undefined),
  attemptVerification: vi.fn().mockResolvedValue({ id: 'email_new_123', emailAddress: 'newemail@example.com' }),
  ...overrides,
});

const makeUser = (overrides: Partial<UserStub> = {}): UserStub => ({
  id: 'user_test_123',
  fullName: 'Jane Doe',
  firstName: 'Jane',
  lastName: 'Doe',
  primaryEmailAddress: { emailAddress: 'jane@example.com' },
  imageUrl: 'https://example.com/avatar.png',
  update: vi.fn().mockResolvedValue(undefined),
  setProfileImage: vi.fn().mockResolvedValue(undefined),
  // Clerk's createEmailAddress uses { email }, not { emailAddress }
  createEmailAddress: vi.fn().mockResolvedValue(makeEmailAddressStub()),
  ...overrides,
});

const signedInHook = (userOverrides: Partial<UserStub> = {}) =>
  (): ReturnType<NonNullable<ProfilePageProps['useUserHook']>> => ({
    isLoaded: true,
    isSignedIn: true,
    user: makeUser(userOverrides),
  });

beforeEach(() => {
  vi.clearAllMocks();
});

// ---------------------------------------------------------------------------
// AC4: PostHog disclosure text — always visible on the profile page
// ---------------------------------------------------------------------------

describe('Profile — PostHog analytics disclosure (AC4)', () => {
  it('shows the PostHog disclosure text when signed in', () => {
    renderWithRouter(<Profile useUserHook={signedInHook()} />);
    expect(
      screen.getByTestId('profile-posthog-disclosure'),
    ).toBeInTheDocument();
  });

  it('disclosure text contains both required sentences', () => {
    renderWithRouter(<Profile useUserHook={signedInHook()} />);
    const disclosure = screen.getByTestId('profile-posthog-disclosure');
    expect(disclosure.textContent).toContain(
      'Historical analytics events cannot be retroactively corrected',
    );
    expect(disclosure.textContent).toContain(
      'no new events will be recorded with the old value after rectification',
    );
  });

  it('disclosure is visible even when NOT in email-edit mode', () => {
    renderWithRouter(<Profile useUserHook={signedInHook()} />);
    // Not in edit mode — disclosure still present
    expect(screen.queryByTestId('profile-email-input')).not.toBeInTheDocument();
    expect(screen.getByTestId('profile-posthog-disclosure')).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Step 1 — idle state
// ---------------------------------------------------------------------------

describe('Profile — email section idle state (AC2)', () => {
  it('renders the current email address', () => {
    renderWithRouter(<Profile useUserHook={signedInHook()} />);
    expect(screen.getByTestId('profile-email-value')).toHaveTextContent('jane@example.com');
  });

  it('renders the Edit Email button in idle state', () => {
    renderWithRouter(<Profile useUserHook={signedInHook()} />);
    expect(screen.getByTestId('profile-edit-email-button')).toBeInTheDocument();
  });

  it('does NOT render the email input in idle state', () => {
    renderWithRouter(<Profile useUserHook={signedInHook()} />);
    expect(screen.queryByTestId('profile-email-input')).not.toBeInTheDocument();
  });

  it('does NOT render the verification code input in idle state', () => {
    renderWithRouter(<Profile useUserHook={signedInHook()} />);
    expect(screen.queryByTestId('profile-email-code-input')).not.toBeInTheDocument();
  });

  it('does NOT render the old read-only email note', () => {
    // The old "Email is managed by your Clerk account..." note is replaced
    // by the interactive flow.
    renderWithRouter(<Profile useUserHook={signedInHook()} />);
    expect(screen.queryByText(/Email is managed by your Clerk account/)).not.toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Step 2 — pending (entering new email)
// ---------------------------------------------------------------------------

describe('Profile — email entry step (pending)', () => {
  it('clicking Edit Email shows the new-email input', () => {
    renderWithRouter(<Profile useUserHook={signedInHook()} />);
    fireEvent.click(screen.getByTestId('profile-edit-email-button'));
    expect(screen.getByTestId('profile-email-input')).toBeInTheDocument();
  });

  it('new-email input is initially empty', () => {
    renderWithRouter(<Profile useUserHook={signedInHook()} />);
    fireEvent.click(screen.getByTestId('profile-edit-email-button'));
    expect(screen.getByTestId('profile-email-input')).toHaveValue('');
  });

  it('shows a Submit/Send-code button in pending state', () => {
    renderWithRouter(<Profile useUserHook={signedInHook()} />);
    fireEvent.click(screen.getByTestId('profile-edit-email-button'));
    expect(screen.getByTestId('profile-email-submit-button')).toBeInTheDocument();
  });

  it('shows a Cancel button in pending state', () => {
    renderWithRouter(<Profile useUserHook={signedInHook()} />);
    fireEvent.click(screen.getByTestId('profile-edit-email-button'));
    expect(screen.getByTestId('profile-email-cancel-button')).toBeInTheDocument();
  });

  it('Cancel in pending state returns to idle', () => {
    renderWithRouter(<Profile useUserHook={signedInHook()} />);
    fireEvent.click(screen.getByTestId('profile-edit-email-button'));
    expect(screen.getByTestId('profile-email-input')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('profile-email-cancel-button'));
    expect(screen.queryByTestId('profile-email-input')).not.toBeInTheDocument();
    expect(screen.getByTestId('profile-edit-email-button')).toBeInTheDocument();
  });

  it('clicking Submit calls user.createEmailAddress with the entered address', async () => {
    const user = makeUser();
    renderWithRouter(<Profile useUserHook={() => ({ isLoaded: true, isSignedIn: true, user })} />);

    fireEvent.click(screen.getByTestId('profile-edit-email-button'));
    fireEvent.change(screen.getByTestId('profile-email-input'), {
      target: { value: 'newemail@example.com' },
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('profile-email-submit-button'));
    });

    // Clerk's createEmailAddress uses { email }, not { emailAddress }
    expect(user.createEmailAddress).toHaveBeenCalledWith({
      email: 'newemail@example.com',
    });
  });

  it('after createEmailAddress, calls prepareVerification on the returned object', async () => {
    const emailStub = makeEmailAddressStub();
    const user = makeUser({ createEmailAddress: vi.fn().mockResolvedValue(emailStub) });
    renderWithRouter(<Profile useUserHook={() => ({ isLoaded: true, isSignedIn: true, user })} />);

    fireEvent.click(screen.getByTestId('profile-edit-email-button'));
    fireEvent.change(screen.getByTestId('profile-email-input'), {
      target: { value: 'newemail@example.com' },
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('profile-email-submit-button'));
    });

    expect(emailStub.prepareVerification).toHaveBeenCalledWith({
      strategy: 'email_code',
    });
  });

  it('advances to verifying step after successful createEmailAddress + prepareVerification', async () => {
    const user = makeUser();
    renderWithRouter(<Profile useUserHook={() => ({ isLoaded: true, isSignedIn: true, user })} />);

    fireEvent.click(screen.getByTestId('profile-edit-email-button'));
    fireEvent.change(screen.getByTestId('profile-email-input'), {
      target: { value: 'newemail@example.com' },
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('profile-email-submit-button'));
    });

    await waitFor(() => {
      expect(screen.getByTestId('profile-email-code-input')).toBeInTheDocument();
    });
  });

  it('shows an error when createEmailAddress rejects', async () => {
    const user = makeUser({
      createEmailAddress: vi.fn().mockRejectedValue(new Error('Email already in use')),
    });
    renderWithRouter(<Profile useUserHook={() => ({ isLoaded: true, isSignedIn: true, user })} />);

    fireEvent.click(screen.getByTestId('profile-edit-email-button'));
    fireEvent.change(screen.getByTestId('profile-email-input'), {
      target: { value: 'taken@example.com' },
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('profile-email-submit-button'));
    });

    await waitFor(() => {
      expect(screen.getByTestId('profile-email-error')).toHaveTextContent('Email already in use');
    });
    // Stays in pending state so user can correct the address
    expect(screen.getByTestId('profile-email-input')).toBeInTheDocument();
  });

  it('submit button is disabled while the request is in flight', async () => {
    let resolve!: () => void;
    const pendingPromise = new Promise<ReturnType<typeof makeEmailAddressStub>>((res) => {
      resolve = () => res(makeEmailAddressStub());
    });
    const user = makeUser({ createEmailAddress: vi.fn().mockReturnValue(pendingPromise) });
    renderWithRouter(<Profile useUserHook={() => ({ isLoaded: true, isSignedIn: true, user })} />);

    fireEvent.click(screen.getByTestId('profile-edit-email-button'));
    fireEvent.change(screen.getByTestId('profile-email-input'), {
      target: { value: 'newemail@example.com' },
    });

    fireEvent.click(screen.getByTestId('profile-email-submit-button'));

    // Button is disabled while awaiting
    expect(screen.getByTestId('profile-email-submit-button')).toBeDisabled();

    await act(async () => { resolve(); });
  });
});

// ---------------------------------------------------------------------------
// Step 3 — verifying (OTP code entry)
// ---------------------------------------------------------------------------

describe('Profile — email verification step (verifying)', () => {
  /** Helper that drives the component to the verifying step. */
  async function advanceToVerifying(user: UserStub) {
    renderWithRouter(<Profile useUserHook={() => ({ isLoaded: true, isSignedIn: true, user })} />);

    fireEvent.click(screen.getByTestId('profile-edit-email-button'));
    fireEvent.change(screen.getByTestId('profile-email-input'), {
      target: { value: 'newemail@example.com' },
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('profile-email-submit-button'));
    });

    await waitFor(() => {
      expect(screen.getByTestId('profile-email-code-input')).toBeInTheDocument();
    });
  }

  it('shows the OTP code input in verifying state', async () => {
    const user = makeUser();
    await advanceToVerifying(user);
    expect(screen.getByTestId('profile-email-code-input')).toBeInTheDocument();
  });

  it('shows a Verify button in verifying state', async () => {
    const user = makeUser();
    await advanceToVerifying(user);
    expect(screen.getByTestId('profile-email-verify-button')).toBeInTheDocument();
  });

  it('shows a Cancel button in verifying state', async () => {
    const user = makeUser();
    await advanceToVerifying(user);
    expect(screen.getByTestId('profile-email-cancel-button')).toBeInTheDocument();
  });

  it('Cancel in verifying state returns to idle', async () => {
    const user = makeUser();
    await advanceToVerifying(user);

    fireEvent.click(screen.getByTestId('profile-email-cancel-button'));
    expect(screen.queryByTestId('profile-email-code-input')).not.toBeInTheDocument();
    expect(screen.getByTestId('profile-edit-email-button')).toBeInTheDocument();
  });

  it('clicking Verify calls attemptVerification with the entered code', async () => {
    const emailStub = makeEmailAddressStub();
    const user = makeUser({ createEmailAddress: vi.fn().mockResolvedValue(emailStub) });
    await advanceToVerifying(user);

    fireEvent.change(screen.getByTestId('profile-email-code-input'), {
      target: { value: '123456' },
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('profile-email-verify-button'));
    });

    expect(emailStub.attemptVerification).toHaveBeenCalledWith({ code: '123456' });
  });

  it('after successful verification, calls user.update with primaryEmailAddressId', async () => {
    // The emailStub must also be returned by attemptVerification with the same id
    const emailStub = makeEmailAddressStub({
      id: 'email_new_999',
      attemptVerification: vi.fn().mockResolvedValue({ id: 'email_new_999', emailAddress: 'newemail@example.com' }),
    });
    const user = makeUser({ createEmailAddress: vi.fn().mockResolvedValue(emailStub) });
    await advanceToVerifying(user);

    fireEvent.change(screen.getByTestId('profile-email-code-input'), {
      target: { value: '123456' },
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('profile-email-verify-button'));
    });

    await waitFor(() => {
      expect(user.update).toHaveBeenCalledWith({
        primaryEmailAddressId: 'email_new_999',
      });
    });
  });

  it('after successful verification, calls patchAccountProfile with the new email (non-blocking)', async () => {
    const emailStub = makeEmailAddressStub({ emailAddress: 'newemail@example.com' });
    const user = makeUser({ createEmailAddress: vi.fn().mockResolvedValue(emailStub) });
    await advanceToVerifying(user);

    fireEvent.change(screen.getByTestId('profile-email-code-input'), {
      target: { value: '654321' },
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('profile-email-verify-button'));
    });

    await waitFor(() => {
      expect(mockPatchAccountProfile).toHaveBeenCalledWith({
        email: 'newemail@example.com',
      });
    });
  });

  it('shows success and returns to idle after successful verification', async () => {
    const user = makeUser();
    await advanceToVerifying(user);

    fireEvent.change(screen.getByTestId('profile-email-code-input'), {
      target: { value: '999999' },
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('profile-email-verify-button'));
    });

    await waitFor(() => {
      expect(screen.getByTestId('profile-email-success')).toBeInTheDocument();
    });
    // Returned to idle
    expect(screen.queryByTestId('profile-email-code-input')).not.toBeInTheDocument();
  });

  it('shows error when attemptVerification rejects (wrong code)', async () => {
    const emailStub = makeEmailAddressStub({
      attemptVerification: vi.fn().mockRejectedValue(new Error('Invalid code')),
    });
    const user = makeUser({ createEmailAddress: vi.fn().mockResolvedValue(emailStub) });
    await advanceToVerifying(user);

    fireEvent.change(screen.getByTestId('profile-email-code-input'), {
      target: { value: '000000' },
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('profile-email-verify-button'));
    });

    await waitFor(() => {
      expect(screen.getByTestId('profile-email-error')).toHaveTextContent('Invalid code');
    });
    // Stays in verifying state so user can retry
    expect(screen.getByTestId('profile-email-code-input')).toBeInTheDocument();
  });

  it('patchAccountProfile failure does NOT surface an error to the user (non-blocking)', async () => {
    mockPatchAccountProfile.mockRejectedValue(new Error('BFF offline'));
    const user = makeUser();
    await advanceToVerifying(user);

    fireEvent.change(screen.getByTestId('profile-email-code-input'), {
      target: { value: '111111' },
    });

    await act(async () => {
      fireEvent.click(screen.getByTestId('profile-email-verify-button'));
    });

    // Success still shown — BFF failure is silently swallowed
    await waitFor(() => {
      expect(screen.getByTestId('profile-email-success')).toBeInTheDocument();
    });
    expect(screen.queryByTestId('profile-email-error')).not.toBeInTheDocument();
  });
});
