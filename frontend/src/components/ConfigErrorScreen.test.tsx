/**
 * Tests for ConfigErrorScreen component — ADR-077.
 *
 * Per Tim's spec (hollowmark-docs/engineering/design/specs/adr-077-config-load-error-screen.md)
 * and the dispatch conditions (C1–C5).
 *
 * Tests:
 * 1. Branch 'network' with onRetry — correct copy, retry button present, clicking calls onRetry
 * 2. Branch 'network' without onRetry — retry button absent
 * 3. Branch 'parse' — correct copy, no retry button
 * 4. Branch 'missing-fields' — correct copy, no retry button
 * 5. appVersion present — version footnote rendered
 * 6. appVersion absent — version footnote absent
 * 7. Focus management — headline has focus after mount
 * 8. role="alert" and aria-live="assertive" on root element
 * 9. Icons have aria-hidden="true"
 */

import { describe, it, expect, vi } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import ConfigErrorScreen from './ConfigErrorScreen';

describe('ConfigErrorScreen', () => {
  // -------------------------------------------------------------------------
  // Branch: network — with onRetry
  // -------------------------------------------------------------------------
  describe('branch="network" with onRetry', () => {
    it('renders the config-error-screen root element', () => {
      render(<ConfigErrorScreen branch="network" onRetry={vi.fn()} />);
      expect(screen.getByTestId('config-error-screen')).toBeInTheDocument();
    });

    it('shows headline "Could not reach VaultMTG"', () => {
      render(<ConfigErrorScreen branch="network" onRetry={vi.fn()} />);
      expect(screen.getByTestId('config-error-screen-headline')).toHaveTextContent(
        'Could not reach VaultMTG',
      );
    });

    it('shows correct body copy for network branch', () => {
      render(<ConfigErrorScreen branch="network" onRetry={vi.fn()} />);
      const body = screen.getByTestId('config-error-screen-body');
      expect(body.textContent).toMatch(/Check your network connection/);
    });

    it('renders the retry button when onRetry is provided', () => {
      render(<ConfigErrorScreen branch="network" onRetry={vi.fn()} />);
      expect(screen.getByTestId('config-error-screen-retry')).toBeInTheDocument();
    });

    it('retry button has label "Try Again"', () => {
      render(<ConfigErrorScreen branch="network" onRetry={vi.fn()} />);
      expect(screen.getByTestId('config-error-screen-retry')).toHaveTextContent('Try Again');
    });

    it('clicking retry button calls onRetry callback', async () => {
      const onRetry = vi.fn();
      render(<ConfigErrorScreen branch="network" onRetry={onRetry} />);
      const user = userEvent.setup();
      await user.click(screen.getByTestId('config-error-screen-retry'));
      expect(onRetry).toHaveBeenCalledOnce();
    });

    it('retry button has class "config-error-retry" for focus-visible ring (spec §6.3)', () => {
      // The :focus-visible box-shadow rule targets .config-error-retry in the scoped
      // <style> block. This assertion pins the class-to-style wiring so a future
      // refactor cannot silently drop the focus ring by removing the class.
      render(<ConfigErrorScreen branch="network" onRetry={vi.fn()} />);
      expect(screen.getByTestId('config-error-screen-retry')).toHaveClass('config-error-retry');
    });
  });

  // -------------------------------------------------------------------------
  // Branch: network — without onRetry
  // -------------------------------------------------------------------------
  describe('branch="network" without onRetry', () => {
    it('does NOT render retry button when onRetry is absent', () => {
      render(<ConfigErrorScreen branch="network" />);
      expect(screen.queryByTestId('config-error-screen-retry')).not.toBeInTheDocument();
    });
  });

  // -------------------------------------------------------------------------
  // Branch: parse
  // -------------------------------------------------------------------------
  describe('branch="parse"', () => {
    it('shows headline "VaultMTG has a setup problem"', () => {
      render(<ConfigErrorScreen branch="parse" />);
      expect(screen.getByTestId('config-error-screen-headline')).toHaveTextContent(
        'VaultMTG has a setup problem',
      );
    });

    it('shows correct body copy for parse branch', () => {
      render(<ConfigErrorScreen branch="parse" />);
      const body = screen.getByTestId('config-error-screen-body');
      expect(body.textContent).toMatch(/This is not your fault/);
    });

    it('does NOT render retry button for parse branch', () => {
      render(<ConfigErrorScreen branch="parse" />);
      expect(screen.queryByTestId('config-error-screen-retry')).not.toBeInTheDocument();
    });
  });

  // -------------------------------------------------------------------------
  // Branch: missing-fields
  // -------------------------------------------------------------------------
  describe('branch="missing-fields"', () => {
    it('shows headline "VaultMTG has a setup problem"', () => {
      render(<ConfigErrorScreen branch="missing-fields" />);
      expect(screen.getByTestId('config-error-screen-headline')).toHaveTextContent(
        'VaultMTG has a setup problem',
      );
    });

    it('shows correct body copy for missing-fields branch', () => {
      render(<ConfigErrorScreen branch="missing-fields" />);
      const body = screen.getByTestId('config-error-screen-body');
      expect(body.textContent).toMatch(/This is not your fault/);
    });

    it('does NOT render retry button for missing-fields branch', () => {
      render(<ConfigErrorScreen branch="missing-fields" />);
      expect(screen.queryByTestId('config-error-screen-retry')).not.toBeInTheDocument();
    });
  });

  // -------------------------------------------------------------------------
  // appVersion prop
  // -------------------------------------------------------------------------
  describe('appVersion prop', () => {
    it('renders version footnote when appVersion is provided', () => {
      render(<ConfigErrorScreen branch="network" appVersion="0.4.3" />);
      const footnote = screen.getByTestId('config-error-screen-version');
      expect(footnote).toBeInTheDocument();
      expect(footnote.textContent).toContain('0.4.3');
    });

    it('does NOT render version footnote when appVersion is absent', () => {
      render(<ConfigErrorScreen branch="network" />);
      expect(screen.queryByTestId('config-error-screen-version')).not.toBeInTheDocument();
    });

    it('does NOT render version footnote when appVersion is empty string', () => {
      render(<ConfigErrorScreen branch="network" appVersion="" />);
      expect(screen.queryByTestId('config-error-screen-version')).not.toBeInTheDocument();
    });
  });

  // -------------------------------------------------------------------------
  // Focus management (Tim spec §6.2)
  // -------------------------------------------------------------------------
  describe('focus management', () => {
    it('headline element has focus after mount', async () => {
      await act(async () => {
        render(<ConfigErrorScreen branch="network" />);
      });
      const headline = screen.getByTestId('config-error-screen-headline');
      expect(document.activeElement).toBe(headline);
    });
  });

  // -------------------------------------------------------------------------
  // Accessibility (Tim spec §6.1)
  // -------------------------------------------------------------------------
  describe('accessibility', () => {
    it('root element has role="alert"', () => {
      render(<ConfigErrorScreen branch="network" />);
      const root = screen.getByTestId('config-error-screen');
      expect(root).toHaveAttribute('role', 'alert');
    });

    it('root element has aria-live="assertive"', () => {
      render(<ConfigErrorScreen branch="network" />);
      const root = screen.getByTestId('config-error-screen');
      expect(root).toHaveAttribute('aria-live', 'assertive');
    });

    it('root element has aria-atomic="true"', () => {
      render(<ConfigErrorScreen branch="network" />);
      const root = screen.getByTestId('config-error-screen');
      expect(root).toHaveAttribute('aria-atomic', 'true');
    });

    it('icon has aria-hidden="true"', () => {
      render(<ConfigErrorScreen branch="network" />);
      const iconWrapper = screen.getByTestId('config-error-screen-icon');
      // The icon itself is inside the wrapper — find SVG elements and check aria-hidden
      const svgOrIcon = iconWrapper.querySelector('svg') ?? iconWrapper;
      expect(svgOrIcon).toHaveAttribute('aria-hidden', 'true');
    });

    it('headline has tabIndex={-1} (programmatically focusable)', () => {
      render(<ConfigErrorScreen branch="network" />);
      const headline = screen.getByTestId('config-error-screen-headline');
      expect(headline).toHaveAttribute('tabIndex', '-1');
    });
  });

  // -------------------------------------------------------------------------
  // data-testid completeness (Tim spec §7)
  // -------------------------------------------------------------------------
  describe('data-testid completeness', () => {
    it('all six data-testids are present for network branch with all props', () => {
      render(
        <ConfigErrorScreen
          branch="network"
          onRetry={vi.fn()}
          appVersion="0.4.3"
        />,
      );
      expect(screen.getByTestId('config-error-screen')).toBeInTheDocument();
      expect(screen.getByTestId('config-error-screen-icon')).toBeInTheDocument();
      expect(screen.getByTestId('config-error-screen-headline')).toBeInTheDocument();
      expect(screen.getByTestId('config-error-screen-body')).toBeInTheDocument();
      expect(screen.getByTestId('config-error-screen-retry')).toBeInTheDocument();
      expect(screen.getByTestId('config-error-screen-version')).toBeInTheDocument();
    });
  });
});
