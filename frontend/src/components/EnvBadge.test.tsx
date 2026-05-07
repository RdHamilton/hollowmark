import { describe, it, expect, vi, afterEach } from 'vitest';
import { screen } from '@testing-library/react';
import { render } from '../test/utils/testUtils';
import EnvBadge from './EnvBadge';

describe('EnvBadge', () => {
  afterEach(() => {
    vi.unstubAllEnvs();
  });

  describe('non-production environments', () => {
    it('renders the badge in preview mode', () => {
      vi.stubEnv('MODE', 'preview');

      render(<EnvBadge />);

      const badge = screen.getByTestId('env-badge');
      expect(badge).toBeInTheDocument();
      expect(badge).toHaveTextContent('preview');
    });

    it('renders the badge in development mode', () => {
      vi.stubEnv('MODE', 'development');

      render(<EnvBadge />);

      const badge = screen.getByTestId('env-badge');
      expect(badge).toBeInTheDocument();
      expect(badge).toHaveTextContent('development');
    });

    it('renders a custom VITE_ENV_LABEL when provided', () => {
      vi.stubEnv('MODE', 'preview');
      vi.stubEnv('VITE_ENV_LABEL', 'staging');

      render(<EnvBadge />);

      const badge = screen.getByTestId('env-badge');
      expect(badge).toBeInTheDocument();
      expect(badge).toHaveTextContent('staging');
    });

    it('applies the correct CSS modifier class based on the label', () => {
      vi.stubEnv('MODE', 'preview');

      render(<EnvBadge />);

      const badge = screen.getByTestId('env-badge');
      expect(badge).toHaveClass('env-badge--preview');
    });

    it('applies staging modifier class when VITE_ENV_LABEL is staging', () => {
      vi.stubEnv('MODE', 'preview');
      vi.stubEnv('VITE_ENV_LABEL', 'staging');

      render(<EnvBadge />);

      const badge = screen.getByTestId('env-badge');
      expect(badge).toHaveClass('env-badge--staging');
    });
  });

  describe('production environment', () => {
    it('does not render the badge in production mode', () => {
      vi.stubEnv('MODE', 'production');

      render(<EnvBadge />);

      expect(screen.queryByTestId('env-badge')).not.toBeInTheDocument();
    });

    it('does not render even if VITE_ENV_LABEL is set in production', () => {
      vi.stubEnv('MODE', 'production');
      vi.stubEnv('VITE_ENV_LABEL', 'staging');

      render(<EnvBadge />);

      expect(screen.queryByTestId('env-badge')).not.toBeInTheDocument();
    });
  });
});
