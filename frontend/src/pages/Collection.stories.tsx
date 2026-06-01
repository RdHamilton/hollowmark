import type { Meta, StoryObj } from '@storybook/react';
import './Collection.css';

/**
 * Collection price accents.
 *
 * Token migration (#328): `.price-value` and `.card-price-badge` text moved off
 * the raw `#90ee90` light-green onto the on-brand `--win` token
 * (= `--vault-sapphire`). Rendered as the migrated classes for a stable
 * Chromatic snapshot (the full Collection page is API/router-bound).
 */
const meta: Meta = {
  title: 'Pages/Collection/PriceAccents',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const PriceValue: Story = {
  render: () => (
    <div className="collection-value" style={{ display: 'flex', gap: 24, alignItems: 'center' }}>
      <span className="price-value">$1,240.50</span>
      <span className="card-price-badge" style={{ position: 'static' }}>
        $4.99
      </span>
    </div>
  ),
};
