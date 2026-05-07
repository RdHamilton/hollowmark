import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import PerformanceMetrics from './PerformanceMetrics';

// ---------------------------------------------------------------------------
// Mock the drafts API
// ---------------------------------------------------------------------------
const mockGetDraftPerformanceMetrics = vi.fn();
vi.mock('@/services/api', () => ({
  drafts: {
    getDraftPerformanceMetrics: (...args: unknown[]) =>
      mockGetDraftPerformanceMetrics(...args),
  },
}));

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------
function makeStats() {
  const latency = {
    mean: 5.5,
    p50: 4.0,
    p95: 12.0,
    p99: 20.0,
    min: 1.0,
    max: 30.0,
    count: 100,
  };
  return {
    parse_latency: { ...latency, count: 50 },
    ratings_latency: { ...latency, count: 50 },
    ui_update_latency: { ...latency, count: 50 },
    end_to_end_latency: latency,
    events_processed: 1234,
    packs_rated: 56,
    api_requests: 200,
    api_errors: 2,
    cache_hits: 180,
    cache_misses: 20,
    cache_hit_rate: 90.0,
    api_success_rate: 99.0,
    uptime: '2h30m',
  };
}

describe('PerformanceMetrics', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetDraftPerformanceMetrics.mockResolvedValue(makeStats());
  });

  it('renders the collapsed "Performance Metrics" header by default', () => {
    render(<PerformanceMetrics />);
    expect(screen.getByText(/Performance Metrics/)).toBeInTheDocument();
  });

  it('shows ▶ when collapsed', () => {
    render(<PerformanceMetrics />);
    expect(screen.getByText(/▶/)).toBeInTheDocument();
  });

  it('does not render metrics content while collapsed', () => {
    render(<PerformanceMetrics />);
    expect(screen.queryByText('System Info')).not.toBeInTheDocument();
  });

  it('expands and loads metrics when header is clicked', async () => {
    await act(async () => {
      render(<PerformanceMetrics />);
    });

    await act(async () => {
      fireEvent.click(screen.getByText(/Performance Metrics/));
    });

    expect(screen.getByText('System Info')).toBeInTheDocument();
    expect(mockGetDraftPerformanceMetrics).toHaveBeenCalledOnce();
  });

  it('shows ▼ when expanded', async () => {
    await act(async () => {
      render(<PerformanceMetrics />);
    });

    await act(async () => {
      fireEvent.click(screen.getByText(/Performance Metrics/));
    });

    expect(screen.getByText(/▼/)).toBeInTheDocument();
  });

  it('renders uptime from API response', async () => {
    await act(async () => {
      render(<PerformanceMetrics />);
    });

    await act(async () => {
      fireEvent.click(screen.getByText(/Performance Metrics/));
    });

    expect(screen.getByText('2h30m')).toBeInTheDocument();
  });

  it('renders events_processed count', async () => {
    await act(async () => {
      render(<PerformanceMetrics />);
    });

    await act(async () => {
      fireEvent.click(screen.getByText(/Performance Metrics/));
    });

    expect(screen.getByText('1,234')).toBeInTheDocument();
  });

  it('renders packs_rated count', async () => {
    await act(async () => {
      render(<PerformanceMetrics />);
    });

    await act(async () => {
      fireEvent.click(screen.getByText(/Performance Metrics/));
    });

    expect(screen.getByText('56')).toBeInTheDocument();
  });

  it('renders End-to-End Latency section when count > 0', async () => {
    await act(async () => {
      render(<PerformanceMetrics />);
    });

    await act(async () => {
      fireEvent.click(screen.getByText(/Performance Metrics/));
    });

    expect(screen.getByText('End-to-End Latency')).toBeInTheDocument();
  });

  it('renders API Statistics section when api_requests > 0', async () => {
    await act(async () => {
      render(<PerformanceMetrics />);
    });

    await act(async () => {
      fireEvent.click(screen.getByText(/Performance Metrics/));
    });

    expect(screen.getByText('API Statistics')).toBeInTheDocument();
  });

  it('renders Cache Statistics section when cache_hits + cache_misses > 0', async () => {
    await act(async () => {
      render(<PerformanceMetrics />);
    });

    await act(async () => {
      fireEvent.click(screen.getByText(/Performance Metrics/));
    });

    expect(screen.getByText('Cache Statistics')).toBeInTheDocument();
  });

  it('shows Reset button when expanded', async () => {
    await act(async () => {
      render(<PerformanceMetrics />);
    });

    await act(async () => {
      fireEvent.click(screen.getByText(/Performance Metrics/));
    });

    expect(screen.getByRole('button', { name: 'Reset' })).toBeInTheDocument();
  });

  it('collapses again when header is clicked a second time', async () => {
    await act(async () => {
      render(<PerformanceMetrics />);
    });

    // Expand
    await act(async () => {
      fireEvent.click(screen.getByText(/Performance Metrics/));
    });

    // Collapse
    await act(async () => {
      fireEvent.click(screen.getByText(/Performance Metrics/));
    });

    expect(screen.queryByText('System Info')).not.toBeInTheDocument();
  });

  it('shows empty-data message when events_processed is 0', async () => {
    mockGetDraftPerformanceMetrics.mockResolvedValue({
      ...makeStats(),
      events_processed: 0,
      end_to_end_latency: { ...makeStats().end_to_end_latency, count: 0 },
      api_requests: 0,
      cache_hits: 0,
      cache_misses: 0,
    });

    await act(async () => {
      render(<PerformanceMetrics />);
    });

    await act(async () => {
      fireEvent.click(screen.getByText(/Performance Metrics/));
    });

    expect(
      screen.getByText(/No performance data collected yet/)
    ).toBeInTheDocument();
  });
});
