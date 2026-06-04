import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import ReportBugButton, { buildContextTags } from './ReportBugButton';

// Hoist mocks so they are available when vi.mock factories run
const { mockCreateForm, mockAppendToDom, mockOpen, mockGetFeedback, mockUseUser, mockSetTag } = vi.hoisted(() => ({
  mockCreateForm: vi.fn(),
  mockAppendToDom: vi.fn(),
  mockOpen: vi.fn(),
  mockGetFeedback: vi.fn(),
  mockUseUser: vi.fn(),
  mockSetTag: vi.fn(),
}));

// Mock @sentry/react
vi.mock('@sentry/react', () => ({
  getFeedback: mockGetFeedback,
  setTag: mockSetTag,
}));

// Mock @clerk/react
vi.mock('@clerk/react', () => ({
  useUser: () => mockUseUser(),
}));

// ---------------------------------------------------------------------------
// buildContextTags unit tests
// ---------------------------------------------------------------------------

describe('buildContextTags', () => {
  it('returns all four context keys', () => {
    const tags = buildContextTags({
      appVersion: '0.3.8',
      userAgent: 'Mozilla/5.0 (Macintosh)',
      userId: 'user_abc123',
      pageUrl: 'https://app.vaultmtg.app/draft',
    });
    expect(tags['app.version']).toBe('0.3.8');
    expect(tags['report.os_ua']).toBe('Mozilla/5.0 (Macintosh)');
    expect(tags['report.user_id']).toBe('user_abc123');
    expect(tags['report.page_url']).toBe('https://app.vaultmtg.app/draft');
  });

  it('passes through unknown/empty values without error', () => {
    const tags = buildContextTags({
      appVersion: 'unknown',
      userAgent: '',
      userId: 'unknown',
      pageUrl: '',
    });
    expect(tags['app.version']).toBe('unknown');
    expect(tags['report.user_id']).toBe('unknown');
    expect(tags['report.os_ua']).toBe('');
    expect(tags['report.page_url']).toBe('');
  });
});

// ---------------------------------------------------------------------------
// ReportBugButton component tests
// ---------------------------------------------------------------------------

describe('ReportBugButton', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Sentry v10 feedback API: createForm() returns Promise<FeedbackDialog>.
    mockCreateForm.mockResolvedValue({
      appendToDom: mockAppendToDom,
      open: mockOpen,
    });
    mockGetFeedback.mockReturnValue({ createForm: mockCreateForm });
  });

  describe('when user is signed in', () => {
    beforeEach(() => {
      mockUseUser.mockReturnValue({
        isSignedIn: true,
        user: {
          id: 'user_clerk_abc',
          emailAddresses: [{ emailAddress: 'ray@example.com' }],
          fullName: 'Ray Hamilton',
          firstName: 'Ray',
          lastName: 'Hamilton',
        },
      });
    });

    it('renders the button', () => {
      render(<ReportBugButton />);
      expect(screen.getByTestId('report-bug-button')).toBeInTheDocument();
      expect(screen.getByText('Report a bug')).toBeInTheDocument();
    });

    it('calls Sentry.setTag for all four context fields before opening the dialog', async () => {
      render(<ReportBugButton />);
      fireEvent.click(screen.getByTestId('report-bug-button'));
      await waitFor(() => expect(mockCreateForm).toHaveBeenCalledTimes(1));

      // setTag should have been called for all four context keys
      const setTagCalls = mockSetTag.mock.calls as [string, string][];
      const tagMap = Object.fromEntries(setTagCalls);
      expect(tagMap['report.user_id']).toBe('user_clerk_abc');
      expect(tagMap['app.version']).toBeDefined();
      expect(tagMap['report.os_ua']).toBeDefined();
      expect(tagMap['report.page_url']).toBeDefined();
    });

    it('calls createForm with user name, email, and tags containing context', async () => {
      render(<ReportBugButton />);
      fireEvent.click(screen.getByTestId('report-bug-button'));
      await waitFor(() => expect(mockCreateForm).toHaveBeenCalledTimes(1));

      const callArg = mockCreateForm.mock.calls[0][0] as {
        useSentryUser: { name: string; email: string };
        tags: Record<string, string>;
      };
      expect(callArg.useSentryUser).toEqual({ name: 'Ray Hamilton', email: 'ray@example.com' });
      // tags must contain all four context keys
      expect(callArg.tags['report.user_id']).toBe('user_clerk_abc');
      expect(callArg.tags['app.version']).toBeDefined();
      expect(callArg.tags['report.os_ua']).toBeDefined();
      expect(callArg.tags['report.page_url']).toBeDefined();
    });

    it('appends and opens the dialog after createForm resolves', async () => {
      render(<ReportBugButton />);
      fireEvent.click(screen.getByTestId('report-bug-button'));
      await waitFor(() => expect(mockAppendToDom).toHaveBeenCalledTimes(1));
      expect(mockOpen).toHaveBeenCalledTimes(1);
    });

    it('passes empty string for name when fullName is empty', async () => {
      mockUseUser.mockReturnValue({
        isSignedIn: true,
        user: {
          id: 'user_clerk_abc',
          emailAddresses: [{ emailAddress: 'ray@example.com' }],
          fullName: '',
          firstName: '',
          lastName: '',
        },
      });
      render(<ReportBugButton />);
      fireEvent.click(screen.getByTestId('report-bug-button'));
      await waitFor(() => expect(mockCreateForm).toHaveBeenCalled());
      const callArg = mockCreateForm.mock.calls[0][0] as { useSentryUser: { name: string; email: string } };
      expect(callArg.useSentryUser.name).toBe('');
      expect(callArg.useSentryUser.email).toBe('ray@example.com');
    });

    it('passes empty string for email when no email addresses', async () => {
      mockUseUser.mockReturnValue({
        isSignedIn: true,
        user: {
          id: 'user_clerk_abc',
          emailAddresses: [],
          fullName: 'Ray Hamilton',
          firstName: 'Ray',
          lastName: 'Hamilton',
        },
      });
      render(<ReportBugButton />);
      fireEvent.click(screen.getByTestId('report-bug-button'));
      await waitFor(() => expect(mockCreateForm).toHaveBeenCalled());
      const callArg = mockCreateForm.mock.calls[0][0] as { useSentryUser: { name: string; email: string } };
      expect(callArg.useSentryUser.email).toBe('');
    });

    it('uses "unknown" as userId when user.id is absent', async () => {
      mockUseUser.mockReturnValue({
        isSignedIn: true,
        user: {
          // no id field
          emailAddresses: [{ emailAddress: 'ray@example.com' }],
          fullName: 'Ray Hamilton',
          firstName: 'Ray',
          lastName: 'Hamilton',
        },
      });
      render(<ReportBugButton />);
      fireEvent.click(screen.getByTestId('report-bug-button'));
      await waitFor(() => expect(mockCreateForm).toHaveBeenCalled());
      const callArg = mockCreateForm.mock.calls[0][0] as { tags: Record<string, string> };
      expect(callArg.tags['report.user_id']).toBe('unknown');
    });

    it('does nothing when Sentry feedback integration is unavailable', async () => {
      mockGetFeedback.mockReturnValue(undefined);
      render(<ReportBugButton />);
      // Should not throw
      fireEvent.click(screen.getByTestId('report-bug-button'));
      expect(mockCreateForm).not.toHaveBeenCalled();
      expect(mockAppendToDom).not.toHaveBeenCalled();
      expect(mockOpen).not.toHaveBeenCalled();
    });
  });

  describe('when user is not signed in', () => {
    beforeEach(() => {
      mockUseUser.mockReturnValue({
        isSignedIn: false,
        user: null,
      });
    });

    it('renders nothing', () => {
      render(<ReportBugButton />);
      expect(screen.queryByTestId('report-bug-button')).not.toBeInTheDocument();
    });
  });
});
