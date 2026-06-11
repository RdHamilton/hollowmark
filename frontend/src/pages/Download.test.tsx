import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import Download from './Download';
import { setRuntimeConfig, _resetRuntimeConfig } from '@/config/runtimeConfig';

// ADR-077: DaemonDownload calls daemonChannel() → getRuntimeConfig().sentryEnv
const testRuntimeConfig = {
  clerkPublishableKey: 'pk_test_dGVzdA',
  bffUrl: 'http://localhost:8080/api/v1',
  sentryEnv: 'production',
  envLabel: 'test',
  daemonUrl: 'http://localhost:9001/api/v1',
  posthogHost: 'https://app.posthog.com',
};

describe('Download Page', () => {
  beforeEach(() => {
    setRuntimeConfig(testRuntimeConfig);
  });

  afterEach(() => {
    _resetRuntimeConfig();
  });

  it('should render the download page container', () => {
    render(<Download />);
    expect(screen.getByTestId('download-page')).toBeInTheDocument();
  });

  it('should render the DaemonDownload section', () => {
    render(<Download />);
    expect(screen.getByTestId('daemon-download-section')).toBeInTheDocument();
  });

  it('should display the page title', () => {
    render(<Download />);
    expect(screen.getByText('Get Started with VaultMTG')).toBeInTheDocument();
  });
});
