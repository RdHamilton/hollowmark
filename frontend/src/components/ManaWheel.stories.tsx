/**
 * ManaWheel — five-color MTG pentagon with a center V-mark "vault".
 *
 * Ported from the marketing ui_kit to the SPA in PR #689 for use as the
 * Home command-strip loading and empty-state visual.
 *
 * The component is a pure SVG; no async calls, no router, no Clerk session.
 * Stories cover:
 *   - Default        — 160px, default Vault Sapphire accent (#4A90D9)
 *   - Small          — 80px, for contexts where a compact indicator is needed
 *   - Large          — 240px, full-size loading state as rendered in Home
 *   - CustomColor    — alternate accent (Vault Success green) to verify the
 *                      accent prop propagates to pentagon, V-mark, and halo
 */
import type { Meta, StoryObj } from '@storybook/react';
import ManaWheel from './ManaWheel';

const meta: Meta<typeof ManaWheel> = {
  title: 'Components/ManaWheel',
  component: ManaWheel,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
  argTypes: {
    color: { control: 'color' },
    size: { control: { type: 'range', min: 40, max: 400, step: 8 } },
    ariaLabel: { control: 'text' },
  },
};

export default meta;
type Story = StoryObj<typeof ManaWheel>;

/**
 * Default — 160px at the Vault Sapphire accent (#4A90D9).
 * This is the component's out-of-the-box appearance.
 */
export const Default: Story = {};

/**
 * Small — 80px. Used when a compact indicator is appropriate.
 */
export const Small: Story = {
  args: {
    size: 80,
    ariaLabel: 'VaultMTG mana wheel (small)',
  },
};

/**
 * Large — 240px. Closest to the Home loading state (Home renders at 120px).
 * Chromatic captures the full SVG detail at this size.
 */
export const Large: Story = {
  args: {
    size: 240,
    ariaLabel: 'VaultMTG mana wheel (large)',
  },
};

/**
 * CustomColor — accent set to Vault Success green (var(--vault-success) = #52c41a).
 * Verifies the `color` prop correctly re-tints the pentagon edges, star
 * connections, halo gradient, and center V-mark while the five canonical
 * mana-color orbs (W/U/B/R/G) remain fixed.
 */
export const CustomColor: Story = {
  args: {
    color: '#52c41a',
    size: 160,
    ariaLabel: 'VaultMTG mana wheel (green accent)',
  },
};
