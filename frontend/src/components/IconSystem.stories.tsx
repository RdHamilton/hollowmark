/**
 * IconSystem stories — v0.3.7 anti-slop wave.
 *
 * Documents the full icon set used in the SPA after the emoji elimination pass.
 * All icons use Heroicons v2 from @heroicons/react/24/outline (or /solid for
 * the CheckCircle complete-state icon).
 */
import type { Meta, StoryObj } from '@storybook/react';
import {
  UserCircleIcon,
  SignalIcon,
  KeyIcon,
  ComputerDesktopIcon,
  Cog6ToothIcon,
  ArchiveBoxIcon,
  ArrowPathIcon,
  ExclamationTriangleIcon,
  ClipboardDocumentIcon,
  CpuChipIcon,
  WrenchScrewdriverIcon,
  ChartBarIcon,
  ViewfinderCircleIcon,
  TrophyIcon,
  RectangleStackIcon,
  MagnifyingGlassIcon,
  DocumentTextIcon,
  PresentationChartBarIcon,
  SparklesIcon,
  MapPinIcon,
  ClockIcon,
} from '@heroicons/react/24/outline';
import { CheckCircleIcon } from '@heroicons/react/24/solid';
import EmptyState from './EmptyState';

const meta: Meta = {
  title: 'System/IconSystem',
  parameters: {
    layout: 'fullscreen',
    backgrounds: {
      default: 'dark',
      values: [{ name: 'dark', value: '#0D1117' }],
    },
  },
};

export default meta;
type Story = StoryObj;

const ICON_SIZE_MD = 'w-5 h-5';
const MUTED = { color: 'var(--vault-fg-muted, #7890AA)' };

// ─── Settings Section Icons ──────────────────────────────────────────────────

const settingsIcons = [
  { icon: UserCircleIcon, label: 'User Profile' },
  { icon: SignalIcon, label: 'Connection' },
  { icon: KeyIcon, label: 'API Key' },
  { icon: ComputerDesktopIcon, label: 'Connected Devices' },
  { icon: Cog6ToothIcon, label: 'Preferences' },
  { icon: ArchiveBoxIcon, label: 'Export / Decks empty' },
  { icon: ArrowPathIcon, label: 'Data Recovery' },
  { icon: ExclamationTriangleIcon, label: 'Danger Zone' },
  { icon: ClipboardDocumentIcon, label: 'Copy Diagnostics / Quests' },
  { icon: CpuChipIcon, label: 'ML / AI' },
  { icon: WrenchScrewdriverIcon, label: 'Developer Tools' },
];

export const SettingsIconSuite: Story = {
  name: 'Settings Icon Suite (all 11 sections)',
  render: () => (
    <div style={{ padding: 24, background: '#0D1117' }}>
      <h2 style={{ color: '#F1F5F9', fontFamily: 'sans-serif', marginBottom: 16 }}>
        Settings Section Icons (20px, muted color)
      </h2>
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(200px, 1fr))', gap: 16 }}>
        {settingsIcons.map(({ icon: Icon, label }) => (
          <div
            key={label}
            style={{ display: 'flex', alignItems: 'center', gap: 8, background: '#161C26', padding: '8px 12px', borderRadius: 8 }}
          >
            <Icon className={ICON_SIZE_MD} style={MUTED} aria-hidden="true" />
            <span style={{ color: '#94A3B8', fontFamily: 'sans-serif', fontSize: 13 }}>{label}</span>
          </div>
        ))}
      </div>
    </div>
  ),
};

// ─── EmptyState Icon Suite ───────────────────────────────────────────────────

export const EmptyStateIconSuite: Story = {
  name: 'EmptyState Icon Suite (no-data / draft / rank / match)',
  render: () => (
    <div style={{ padding: 24, background: '#0D1117', display: 'flex', flexWrap: 'wrap', gap: 24 }}>
      <div style={{ flex: '1 1 300px' }}>
        <EmptyState
          icon={<ChartBarIcon className="w-12 h-12" aria-hidden="true" style={MUTED} />}
          heading="Not enough data"
          subtext="Play at least 5 matches to see your win rate trends over time."
          variant="no-data"
        />
      </div>
      <div style={{ flex: '1 1 300px' }}>
        <EmptyState
          icon={<ViewfinderCircleIcon className="w-12 h-12" aria-hidden="true" style={MUTED} />}
          heading="No draft data"
          subtext="Start a draft to see your pick analytics."
          variant="no-data"
        />
      </div>
      <div style={{ flex: '1 1 300px' }}>
        <EmptyState
          icon={<TrophyIcon className="w-12 h-12" aria-hidden="true" style={MUTED} />}
          heading="No rank data"
          subtext="Play ranked matches to track your progression."
          variant="no-data"
        />
      </div>
      <div style={{ flex: '1 1 300px' }}>
        <EmptyState
          icon={<RectangleStackIcon className="w-12 h-12" aria-hidden="true" style={MUTED} />}
          heading="No recent matches"
          subtext="Your recent matches will appear here after you play."
          variant="no-data"
        />
      </div>
      <div style={{ flex: '1 1 300px' }}>
        <EmptyState
          icon={<CheckCircleIcon className="w-12 h-12" aria-hidden="true" style={{ color: 'var(--vault-success, #22C55E)' }} />}
          heading="Draft complete"
          subtext="Your draft is done. Build your deck!"
          variant="no-data"
        />
      </div>
    </div>
  ),
};

// ─── DeckBuilder Command Bar Icons ──────────────────────────────────────────

export const DeckBuilderActionIcons: Story = {
  name: 'DeckBuilder Action Icons (Search / Suggestions / AddLands)',
  render: () => (
    <div style={{ padding: 24, background: '#161C26', display: 'inline-flex', gap: 12, borderRadius: 8 }}>
      <button style={{ display: 'flex', alignItems: 'center', gap: 6, background: '#2A3347', color: '#F1F5F9', border: '1px solid #2A3347', borderRadius: 6, padding: '6px 12px', cursor: 'pointer', fontFamily: 'sans-serif', fontSize: 13 }}>
        <MagnifyingGlassIcon className="w-4 h-4" aria-hidden="true" style={{ color: '#94A3B8' }} />
        Add Cards
      </button>
      <button style={{ display: 'flex', alignItems: 'center', gap: 6, background: '#2A3347', color: '#F1F5F9', border: '1px solid #2A3347', borderRadius: 6, padding: '6px 12px', cursor: 'pointer', fontFamily: 'sans-serif', fontSize: 13 }}>
        <SparklesIcon className="w-4 h-4" aria-hidden="true" style={{ color: '#94A3B8' }} />
        Suggestions
      </button>
      <button style={{ display: 'flex', alignItems: 'center', gap: 6, background: '#2A3347', color: '#F1F5F9', border: '1px solid #2A3347', borderRadius: 6, padding: '6px 12px', cursor: 'pointer', fontFamily: 'sans-serif', fontSize: 13 }}>
        <MapPinIcon className="w-4 h-4" aria-hidden="true" style={{ color: '#94A3B8' }} />
        Add Lands
      </button>
      <button style={{ display: 'flex', alignItems: 'center', gap: 6, background: '#2A3347', color: '#F1F5F9', border: '1px solid #2A3347', borderRadius: 6, padding: '6px 12px', cursor: 'pointer', fontFamily: 'sans-serif', fontSize: 13 }}>
        <DocumentTextIcon className="w-4 h-4" aria-hidden="true" style={{ color: '#94A3B8' }} />
        Quests
      </button>
      <button style={{ display: 'flex', alignItems: 'center', gap: 6, background: '#2A3347', color: '#F1F5F9', border: '1px solid #2A3347', borderRadius: 6, padding: '6px 12px', cursor: 'pointer', fontFamily: 'sans-serif', fontSize: 13 }}>
        <PresentationChartBarIcon className="w-4 h-4" aria-hidden="true" style={{ color: '#94A3B8' }} />
        Results
      </button>
      <button style={{ display: 'flex', alignItems: 'center', gap: 6, background: '#2A3347', color: '#F1F5F9', border: '1px solid #2A3347', borderRadius: 6, padding: '6px 12px', cursor: 'pointer', fontFamily: 'sans-serif', fontSize: 13 }}>
        <ClockIcon className="w-4 h-4" aria-hidden="true" style={{ color: '#94A3B8' }} />
        Meta Clock
      </button>
    </div>
  ),
};
