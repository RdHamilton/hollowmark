import { useMemo } from 'react';
import { trackEvent } from '@/services/analytics';
import { useFeatureFlag } from '@/hooks/useFeatureFlag';
import { useDaemonRelease } from '@/hooks/useDaemonRelease';
import { daemonArtifactChannelInfix } from '@/services/daemonRelease';
import './DaemonDownload.css';

const WAITLIST_URL = 'https://vaultmtg.app/#waitlist';

/** Common stem shared by every channel-suffixed daemon artifact. */
const DAEMON_STEM = 'vaultmtg-daemon';

interface DownloadOption {
  label: string;
  /**
   * Artifact filename (without extension) as it appears on the GitHub release,
   * expressed as the part that follows the `vaultmtg-daemon` stem (including the
   * leading hyphen). When `channelAware` is true the channel infix
   * (`-staging` on staging, `` on stable) is spliced in between the stem and
   * this suffix so the URL resolves to the real per-channel asset name.
   */
  artifactSuffix: string;
  /**
   * Whether the published asset name carries the channel infix. macOS .pkg is
   * channel-parameterized (`vaultmtg-daemon-staging-darwin-universal.pkg` on
   * staging); the bare Windows binary is NOT — it is published under the same
   * fixed name in both channels (.goreleaser.yml archives id: windows-amd64).
   */
  channelAware: boolean;
  /** Logical platform key used for OS detection matching. */
  platform: 'windows' | 'macos';
  ext: string;
  description: string;
}

const DOWNLOAD_OPTIONS: DownloadOption[] = [
  {
    label: 'Windows (64-bit)',
    artifactSuffix: '-windows-amd64',
    channelAware: false,
    platform: 'windows',
    ext: 'exe',
    description: 'Windows 10/11 64-bit',
  },
  {
    label: 'macOS (Universal)',
    artifactSuffix: '-darwin-universal',
    channelAware: true,
    platform: 'macos',
    ext: 'pkg',
    description: 'macOS 12+ — Apple Silicon and Intel',
  },
];

const GETTING_STARTED_STEPS = [
  {
    number: 1,
    title: 'Download',
    description: 'Download the daemon binary for your operating system using the button above.',
  },
  {
    number: 2,
    title: 'Run the installer',
    description:
      'On macOS: open the .pkg installer and follow the prompts. On Windows: run the .exe installer and follow the Next → Next → Finish prompts.',
  },
  {
    number: 3,
    title: 'Launch MTGA Arena',
    description: 'Start MTG Arena as you normally would. The daemon will detect it automatically.',
  },
  {
    number: 4,
    title: 'Open the companion app',
    description:
      'With the daemon running, open the VaultMTG web app. Your match history and draft data will begin syncing.',
  },
];

function detectPlatform(): 'windows' | 'macos' {
  const ua = navigator.userAgent.toLowerCase();
  const platform =
    typeof navigator.platform === 'string'
      ? navigator.platform.toLowerCase()
      : '';

  if (platform.includes('win') || ua.includes('windows')) {
    return 'windows';
  }
  // Default to macOS (covers Mac + unknown)
  return 'macos';
}

/**
 * Resolve the full artifact filename (without extension) for the current
 * release channel. The channel infix is spliced between the `vaultmtg-daemon`
 * stem and the platform suffix for channel-aware artifacts so the URL points at
 * the real per-channel asset (`vaultmtg-daemon-staging-darwin-universal` on
 * staging, `vaultmtg-daemon-darwin-universal` on stable).
 */
function resolveArtifactName(option: DownloadOption): string {
  const infix = option.channelAware ? daemonArtifactChannelInfix() : '';
  return `${DAEMON_STEM}${infix}${option.artifactSuffix}`;
}

function buildDownloadUrl(option: DownloadOption, downloadBase: string): string {
  return `${downloadBase}/${resolveArtifactName(option)}.${option.ext}`;
}

/** Skeleton placeholder shown while the PostHog feature flag loads. */
function DownloadButtonsSkeleton() {
  return (
    <div
      className="daemon-download-skeleton"
      data-testid="daemon-download-skeleton"
      aria-label="Loading download options"
      aria-busy="true"
    >
      <div className="daemon-download-skeleton-bar" />
      <div className="daemon-download-skeleton-bar" />
      <div className="daemon-download-skeleton-bar" />
    </div>
  );
}

/** CTA rendered when the daemon_download_enabled flag is off. */
function DownloadComingSoon() {
  return (
    <div
      className="daemon-download-coming-soon"
      data-testid="daemon-download-coming-soon"
    >
      <p className="daemon-download-coming-soon-message">
        The daemon installer will be available at beta launch.{' '}
        <a
          href={WAITLIST_URL}
          className="daemon-download-coming-soon-link"
          data-testid="daemon-download-waitlist-link"
          target="_blank"
          rel="noopener noreferrer"
        >
          Join the waitlist to get notified.
        </a>
      </p>
    </div>
  );
}

const DaemonDownload = () => {
  const detectedPlatform = useMemo(() => detectPlatform(), []);
  const { enabled: downloadEnabled } = useFeatureFlag('daemon_download_enabled');
  const { downloadBase } = useDaemonRelease();

  return (
    <section className="daemon-download" data-testid="daemon-download-section">
      <div className="daemon-download-header">
        <h1 className="daemon-download-title" data-testid="daemon-download-title">
          Get Started with VaultMTG
        </h1>
        <p className="daemon-download-subtitle">
          Download the daemon for your platform to start tracking your MTG Arena matches,
          drafts, and collection in real time.
        </p>
      </div>

      {downloadEnabled === null && <DownloadButtonsSkeleton />}

      {downloadEnabled === true && (
        <div className="daemon-download-buttons" data-testid="daemon-download-buttons">
          {DOWNLOAD_OPTIONS.map((option) => {
            const isDetected = option.platform === detectedPlatform;
            const href = buildDownloadUrl(option, downloadBase);
            // Channel-stable identity for the DOM key, test selector and the
            // analytics `os` property: the stem + platform suffix WITHOUT the
            // channel infix. This keeps selectors and funnel analytics identical
            // across the staging and stable channels (only the href differs).
            const stableId = `${DAEMON_STEM}${option.artifactSuffix}`;
            return (
              <a
                key={stableId}
                href={href}
                className={`daemon-download-button ${isDetected ? 'daemon-download-button--primary' : 'daemon-download-button--secondary'}`}
                data-testid={`download-link-${stableId}`}
                download
                onClick={() => {
                  trackEvent({
                    name: 'funnel_daemon_download_started',
                    properties: {
                      os: stableId,
                      download_source: 'download_page',
                    },
                  });
                }}
              >
                <span className="daemon-download-button-label">{option.label}</span>
                {isDetected && (
                  <span className="daemon-download-button-recommended">Recommended</span>
                )}
                <span className="daemon-download-button-desc">{option.description}</span>
              </a>
            );
          })}
        </div>
      )}

      {downloadEnabled === false && <DownloadComingSoon />}

      <div className="daemon-getting-started" data-testid="daemon-getting-started">
        <h2 className="daemon-getting-started-title">Getting Started</h2>
        <ol className="daemon-getting-started-steps">
          {GETTING_STARTED_STEPS.map((step) => (
            <li
              key={step.number}
              className="daemon-getting-started-step"
              data-testid={`getting-started-step-${step.number}`}
            >
              <div className="step-number" aria-hidden="true">
                {step.number}
              </div>
              <div className="step-content">
                <h3 className="step-title">{step.title}</h3>
                <p className="step-description">{step.description}</p>
              </div>
            </li>
          ))}
        </ol>
        <div className="daemon-uninstall" data-testid="daemon-uninstall">
          <h3 className="daemon-uninstall-title">Need to remove the daemon?</h3>
          <p className="daemon-uninstall-description">
            On macOS, run the bundled uninstall script with:
          </p>
          <pre className="daemon-uninstall-command" data-testid="daemon-uninstall-command">
            <code>sudo /usr/local/share/vaultmtg/uninstall.sh</code>
          </pre>
          <p className="daemon-uninstall-description">
            On Windows, open Add/Remove Programs and select VaultMTG Daemon, or run{' '}
            <code>Uninstall.exe</code> from your install directory.
          </p>
        </div>
      </div>
    </section>
  );
};

export default DaemonDownload;
