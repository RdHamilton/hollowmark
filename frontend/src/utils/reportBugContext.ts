/**
 * Build the structured context tags for a Sentry feedback submission.
 *
 * Tags are indexed in Sentry and searchable, so every report automatically
 * carries actionable context: app version, OS/UA, Clerk user_id, and the
 * page URL from which the report was filed.
 */
export function buildContextTags(opts: {
  appVersion: string;
  userAgent: string;
  userId: string;
  pageUrl: string;
}): Record<string, string> {
  return {
    'app.version': opts.appVersion,
    'report.os_ua': opts.userAgent,
    'report.user_id': opts.userId,
    'report.page_url': opts.pageUrl,
  };
}
