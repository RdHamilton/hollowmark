/**
 * ManaPip + ColorIdentity stories
 *
 * Font provenance: mana-font@1.18.0 by Andrew Gioia — MIT license
 * https://mana.andrewgioia.com  |  npm: mana-font
 *
 * Ticket: #690 — replace generic color circles with MTG mana pips (Prof design audit)
 */
import type { Meta, StoryObj } from '@storybook/react';
import ManaPip, { type ManaColor, type ManaPipSize } from './ManaPip';
import ColorIdentity from './ColorIdentity';

// ── ManaPip ──────────────────────────────────────────────────────────────────

const pipMeta: Meta<typeof ManaPip> = {
  title: 'Components/ManaPip',
  component: ManaPip,
  parameters: { layout: 'centered' },
  argTypes: {
    color: {
      control: { type: 'select' },
      options: ['W', 'U', 'B', 'R', 'G', 'C', 'M'] satisfies ManaColor[],
    },
    size: {
      control: { type: 'select' },
      options: ['sm', 'md', 'lg'] satisfies ManaPipSize[],
    },
  },
};

export default pipMeta;
type Story = StoryObj<typeof ManaPip>;

export const White: Story  = { args: { color: 'W', size: 'lg' } };
export const Blue: Story   = { args: { color: 'U', size: 'lg' } };
export const Black: Story  = { args: { color: 'B', size: 'lg' } };
export const Red: Story    = { args: { color: 'R', size: 'lg' } };
export const Green: Story  = { args: { color: 'G', size: 'lg' } };
export const Colorless: Story = { args: { color: 'C', size: 'lg' } };
export const Multicolor: Story = { args: { color: 'M', size: 'lg' } };

/** All WUBRG pips side-by-side in each size tier */
export const AllColorsAllSizes: Story = {
  render: () => {
    const colors: ManaColor[] = ['W', 'U', 'B', 'R', 'G', 'C', 'M'];
    const sizes: ManaPipSize[] = ['sm', 'md', 'lg'];
    return (
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        {sizes.map((size) => (
          <div key={size} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span style={{ color: '#94A3B8', fontSize: 11, width: 24 }}>{size}</span>
            {colors.map((color) => (
              <ManaPip key={color} color={color} size={size} />
            ))}
          </div>
        ))}
      </div>
    );
  },
};

// ── ColorIdentity ─────────────────────────────────────────────────────────────

export const ColorIdentityUW: StoryObj = {
  name: 'ColorIdentity — Azorius (W/U)',
  render: () => <ColorIdentity colors={['W', 'U']} size="md" />,
};

export const ColorIdentityWUBRG: StoryObj = {
  name: 'ColorIdentity — Five-color (WUBRG)',
  render: () => <ColorIdentity colors={['W', 'U', 'B', 'R', 'G']} size="md" />,
};

export const ColorIdentityColorless: StoryObj = {
  name: 'ColorIdentity — Colorless (empty input)',
  render: () => <ColorIdentity colors={[]} size="md" />,
};

export const ColorIdentityStringInput: StoryObj = {
  name: 'ColorIdentity — String input "WUB"',
  render: () => <ColorIdentity colors="WUB" size="md" />,
};

export const ColorIdentitySizes: StoryObj = {
  name: 'ColorIdentity — All sizes',
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {(['sm', 'md', 'lg'] as ManaPipSize[]).map((size) => (
        <div key={size} style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ color: '#94A3B8', fontSize: 11, width: 24 }}>{size}</span>
          <ColorIdentity colors={['W', 'U', 'B', 'R', 'G']} size={size} />
        </div>
      ))}
    </div>
  ),
};
