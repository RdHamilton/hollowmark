import './EnvBadge.css';

/**
 * EnvBadge — renders a small environment chip in non-production builds.
 *
 * Visible when: import.meta.env.MODE !== 'production'
 * Hidden in:    production (app.vaultmtg.app)
 *
 * The label shown is either VITE_ENV_LABEL (if set) or the Vite MODE value.
 * On Vercel preview deployments MODE is 'preview'; on local dev it is
 * 'development'. A custom label like "staging" can be set via VITE_ENV_LABEL.
 */
const EnvBadge = () => {
  const mode = import.meta.env.MODE as string;

  if (mode === 'production') {
    return null;
  }

  const label: string =
    (import.meta.env.VITE_ENV_LABEL as string | undefined) ?? mode;

  return (
    <span className={`env-badge env-badge--${label}`} data-testid="env-badge">
      {label}
    </span>
  );
};

export default EnvBadge;
