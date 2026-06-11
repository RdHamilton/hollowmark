import './EnvBadge.css';
import { getRuntimeConfig, isRuntimeConfigLoaded } from '../config/runtimeConfig';

/**
 * EnvBadge — renders a small environment chip in non-production builds.
 *
 * ADR-077: envLabel now comes from runtimeConfig.envLabel (runtime value from
 * config.json) instead of VITE_ENV_LABEL (build-time baked). Falls back to
 * import.meta.env.MODE in DEV when runtimeConfig has not yet been loaded
 * (e.g. Storybook, test renders without a full boot sequence).
 *
 * Hidden when envLabel is 'production' (matches the value written by the
 * production deploy workflow into config.json).
 */
const EnvBadge = () => {
  // ADR-077: read envLabel from runtimeConfig at render time.
  // In DEV (Storybook / test renders before loadConfig()), fall back to MODE.
  let label: string;
  if (isRuntimeConfigLoaded()) {
    label = getRuntimeConfig().envLabel;
  } else if (import.meta.env.DEV) {
    label = (import.meta.env.VITE_ENV_LABEL as string | undefined) ?? import.meta.env.MODE;
  } else {
    // Production build, config not yet loaded — hide badge (boot in progress).
    return null;
  }

  if (label === 'production') {
    return null;
  }

  return (
    <span className={`env-badge env-badge--${label}`} data-testid="env-badge">
      {label}
    </span>
  );
};

export default EnvBadge;
