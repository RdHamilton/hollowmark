import { useCallback } from 'react';
import { useUser } from '@clerk/react';
import * as Sentry from '@sentry/react';
import { buildContextTags } from '../utils/reportBugContext';
import './ReportBugButton.css';

/**
 * ReportBugButton — floating "Report a bug" trigger for authenticated users.
 *
 * Opens the Sentry User Feedback dialog, pre-populated with:
 *   - user name + email (from Clerk) so CS gets full identity context
 *   - app version, OS/UA, Clerk user_id, and current page URL as Sentry tags
 *     so every report arrives with actionable, indexed context attached
 *
 * Only rendered when the user is signed in (enforced by the parent Layout).
 */
const ReportBugButton = () => {
  const { user, isSignedIn } = useUser();

  const handleClick = useCallback(async () => {
    if (!isSignedIn || !user) return;

    const primaryEmail = user.emailAddresses?.[0]?.emailAddress ?? '';
    const name = user.fullName ?? [user.firstName, user.lastName].filter(Boolean).join(' ');
    const userId = (user as { id?: string }).id ?? 'unknown';

    const appVersion =
      (import.meta.env.VITE_APP_VERSION as string | undefined) ?? 'unknown';
    const userAgent = navigator.userAgent;
    const pageUrl = window.location.href;

    // getFeedback() returns the feedbackIntegration instance (added in main.tsx).
    // If Sentry is not initialised (dev without VITE_SENTRY_DSN), getFeedback()
    // returns undefined — guard and bail silently.
    const feedback = Sentry.getFeedback();
    if (!feedback) return;

    // Attach context tags to the Sentry scope so they ride along with the
    // feedback event. Tags are searchable and indexed in the Sentry dashboard.
    const tags = buildContextTags({ appVersion, userAgent, userId, pageUrl });
    for (const [key, value] of Object.entries(tags)) {
      Sentry.setTag(key, value);
    }

    // Sentry v10 feedback API: createForm() returns a Promise<FeedbackDialog>.
    // The dialog must be appended to the DOM and then opened. We pre-fill the
    // form via the integration's `useSentryUser` option pattern by passing
    // overrides — see sentry.io/docs/platforms/javascript/user-feedback.
    const dialog = await feedback.createForm({
      useSentryUser: {
        name: name || '',
        email: primaryEmail || '',
      },
      tags,
    });
    dialog.appendToDom();
    dialog.open();
  }, [isSignedIn, user]);

  if (!isSignedIn) return null;

  return (
    <button
      className="report-bug-btn"
      onClick={handleClick}
      aria-label="Report a bug"
      data-testid="report-bug-button"
      type="button"
    >
      Report a bug
    </button>
  );
};

export default ReportBugButton;
