/**
 * EnvBadge component tests — ADR-077.
 *
 * ADR-077: EnvBadge now reads envLabel from runtimeConfig instead of
 * VITE_ENV_LABEL (build-time baked). Tests use setRuntimeConfig() to control
 * the label value.
 */
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { screen } from '@testing-library/react';
import { render } from '../test/utils/testUtils';
import EnvBadge from './EnvBadge';
import { setRuntimeConfig, _resetRuntimeConfig } from '../config/runtimeConfig';

const baseConfig = {
  clerkPublishableKey: 'pk_test_dGVzdA',
  bffUrl: 'http://localhost:8080/api/v1',
  sentryEnv: 'test',
  envLabel: 'development',
  daemonUrl: 'http://localhost:9001/api/v1',
  posthogHost: 'https://app.posthog.com',
};

describe('EnvBadge', () => {
  beforeEach(() => {
    setRuntimeConfig(baseConfig);
  });

  afterEach(() => {
    _resetRuntimeConfig();
  });

  describe('non-production environments', () => {
    it('renders the badge in preview mode', () => {
      setRuntimeConfig({ ...baseConfig, envLabel: 'preview' });

      render(<EnvBadge />);

      const badge = screen.getByTestId('env-badge');
      expect(badge).toBeInTheDocument();
      expect(badge).toHaveTextContent('preview');
    });

    it('renders the badge in development mode', () => {
      setRuntimeConfig({ ...baseConfig, envLabel: 'development' });

      render(<EnvBadge />);

      const badge = screen.getByTestId('env-badge');
      expect(badge).toBeInTheDocument();
      expect(badge).toHaveTextContent('development');
    });

    it('renders the badge with staging label', () => {
      setRuntimeConfig({ ...baseConfig, envLabel: 'staging' });

      render(<EnvBadge />);

      const badge = screen.getByTestId('env-badge');
      expect(badge).toBeInTheDocument();
      expect(badge).toHaveTextContent('staging');
    });

    it('applies the correct CSS modifier class based on the label', () => {
      setRuntimeConfig({ ...baseConfig, envLabel: 'preview' });

      render(<EnvBadge />);

      const badge = screen.getByTestId('env-badge');
      expect(badge).toHaveClass('env-badge--preview');
    });

    it('applies staging modifier class when envLabel is staging', () => {
      setRuntimeConfig({ ...baseConfig, envLabel: 'staging' });

      render(<EnvBadge />);

      const badge = screen.getByTestId('env-badge');
      expect(badge).toHaveClass('env-badge--staging');
    });
  });

  describe('production environment', () => {
    it('does not render the badge in production mode', () => {
      setRuntimeConfig({ ...baseConfig, envLabel: 'production' });

      render(<EnvBadge />);

      expect(screen.queryByTestId('env-badge')).not.toBeInTheDocument();
    });

    it('does not render even if envLabel is explicitly production', () => {
      setRuntimeConfig({ ...baseConfig, envLabel: 'production' });

      render(<EnvBadge />);

      expect(screen.queryByTestId('env-badge')).not.toBeInTheDocument();
    });
  });
});
