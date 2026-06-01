import type { Meta, StoryObj } from '@storybook/react';
import './DaemonDownload.css';

/**
 * DaemonDownload CTA buttons (MTGA companion).
 *
 * Token migration (#328, Ray ruling): the primary download CTA moved off the
 * raw Twitch-purple `#9147ff` / `#7b2fff` onto the named `--vault-twitch` /
 * `--vault-twitch-dark` tokens (intentional third-party brand reference).
 */
const meta: Meta = {
  title: 'Components/DaemonDownload/Buttons',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const DownloadButtons: Story = {
  render: () => (
    <div className="daemon-download-buttons" style={{ display: 'flex', gap: 16 }}>
      <a className="daemon-download-button daemon-download-button--primary">
        <span className="daemon-download-button-label">Download for macOS</span>
        <span className="daemon-download-button-recommended">Recommended</span>
      </a>
      <a className="daemon-download-button daemon-download-button--secondary">
        <span className="daemon-download-button-label">Download for Windows</span>
      </a>
    </div>
  ),
};
