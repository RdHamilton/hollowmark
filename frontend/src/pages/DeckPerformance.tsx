import { useState, useEffect } from 'react';
import { RectangleStackIcon } from '@heroicons/react/24/outline';
import { EventsOn } from '@/services/websocketClient';
import { matches } from '@/services/api';
import type { DeckPerformanceRow } from '@/services/api/matches';
import LoadingSpinner from '../components/LoadingSpinner';
import EmptyState from '../components/EmptyState';
import ErrorState from '../components/ErrorState';
import { useAppContext } from '../context/AppContext';
import { normalizeQueueType } from '@/utils/formatNormalization';
import './DeckPerformance.css';

const DeckPerformance = () => {
  const { filters, updateFilters } = useAppContext();
  const { format, sortBy, sortDirection } = filters.deckPerformance;

  const [deckStats, setDeckStats] = useState<DeckPerformanceRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const loadDeckStats = async () => {
      try {
        setLoading(true);
        setError(null);
        const data = await matches.getDeckPerformance();
        setDeckStats(data);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load deck statistics');
        console.error('Error loading deck stats:', err);
      } finally {
        setLoading(false);
      }
    };

    loadDeckStats();
  }, []);

  // Listen for real-time updates
  useEffect(() => {
    const loadDeckStats = async () => {
      try {
        setLoading(true);
        setError(null);
        const data = await matches.getDeckPerformance();
        setDeckStats(data);
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load deck statistics');
        console.error('Error loading deck stats:', err);
      } finally {
        setLoading(false);
      }
    };

    const unsubscribe = EventsOn('stats:updated', () => {
      console.log('Stats updated event received - reloading deck performance data');
      void loadDeckStats();
    });

    return () => {
      if (unsubscribe) {
        unsubscribe();
      }
    };
  }, []);

  const formatWinRate = (wins: number, total: number) => {
    if (total === 0) return '0.0%';
    return `${Math.round((wins / total) * 100 * 10) / 10}%`;
  };

  // Filter by format if selected
  const filteredDecks = deckStats.filter((deck) => {
    if (format === 'all') return true;
    if (format === 'constructed') {
      return ['Ladder', 'Play', 'Standard', 'Historic', 'Explorer', 'Alchemy', 'Timeless', 'Pioneer', 'Modern'].includes(deck.format);
    }
    if (format === 'limited') {
      return deck.format.startsWith('QuickDraft') || deck.format.startsWith('PremierDraft') ||
             deck.format.startsWith('TradDraft') || deck.format.startsWith('SealedDeck');
    }
    return deck.format === format;
  });

  // Sort deck stats
  const sortedDecks = [...filteredDecks].sort((a, b) => {
    let aVal: number | string = 0;
    let bVal: number | string = 0;

    switch (sortBy) {
      case 'winRate': {
        const aRate = a.total_games > 0 ? a.wins / a.total_games : 0;
        const bRate = b.total_games > 0 ? b.wins / b.total_games : 0;
        aVal = aRate;
        bVal = bRate;
        break;
      }
      case 'matches':
        aVal = a.total_games;
        bVal = b.total_games;
        break;
      case 'name':
        aVal = (a.deck_name || '').toLowerCase();
        bVal = (b.deck_name || '').toLowerCase();
        break;
    }

    if (sortDirection === 'asc') {
      return aVal > bVal ? 1 : aVal < bVal ? -1 : 0;
    } else {
      return aVal < bVal ? 1 : aVal > bVal ? -1 : 0;
    }
  });

  return (
    <div className="page-container">
      <div className="deck-performance-header">
        <h1 className="page-title">Deck Performance</h1>

        {/* Filters */}
        <div className="filter-row">
          <div className="filter-group">
            <label className="filter-label">Format</label>
            <select value={format} onChange={(e) => updateFilters('deckPerformance', { format: e.target.value })}>
              <option value="all">All Formats</option>
              <option value="constructed">Constructed</option>
              <option value="limited">Limited</option>
              <option value="Ladder">Ranked (Ladder)</option>
              <option value="Play">Play Queue</option>
            </select>
          </div>

          <div className="filter-group">
            <label className="filter-label">Sort By</label>
            <select value={sortBy} onChange={(e) => updateFilters('deckPerformance', { sortBy: e.target.value as 'winRate' | 'matches' | 'name' })}>
              <option value="winRate">Win Rate</option>
              <option value="matches">Match Count</option>
              <option value="name">Deck Name</option>
            </select>
          </div>

          <div className="filter-group">
            <label className="filter-label">Sort Order</label>
            <select value={sortDirection} onChange={(e) => updateFilters('deckPerformance', { sortDirection: e.target.value as 'asc' | 'desc' })}>
              <option value="desc">Descending</option>
              <option value="asc">Ascending</option>
            </select>
          </div>
        </div>

        {!loading && !error && deckStats.length > 0 && (
          <div className="deck-count">
            {deckStats.length} deck{deckStats.length !== 1 ? 's' : ''} found
          </div>
        )}
      </div>

      {/* Content */}
      {loading && <LoadingSpinner message="Loading deck statistics..." />}

      {error && (
        <ErrorState
          message="Failed to load deck statistics"
          error={error}
          helpText="Check that the database is accessible and try refreshing the page."
        />
      )}

      {!loading && !error && deckStats.length === 0 && (
        <EmptyState
          icon={<RectangleStackIcon className="w-12 h-12" aria-hidden="true" style={{ color: 'var(--vault-fg-muted)' }} />}
          heading="No deck data"
          subtext="Play matches with different decks to see your deck performance statistics."
          variant="no-data"
        />
      )}

      {!loading && !error && sortedDecks.length > 0 && (
        <div className="deck-grid">
          {sortedDecks.map((deck) => (
            <div key={deck.deck_id || deck.deck_name} className="deck-card" data-testid="deck-performance-card">
              <h3 className="deck-name">{deck.deck_name || 'Unknown Deck'}</h3>
              {deck.format && (
                <div className="deck-format-label">{normalizeQueueType(deck.format)}</div>
              )}
              <div className="deck-stats">
                <div className="stat">
                  <span className="stat-label">Win Rate</span>
                  <span className="stat-value win-rate">{formatWinRate(deck.wins, deck.total_games)}</span>
                </div>
                <div className="stat">
                  <span className="stat-label">Games</span>
                  <span className="stat-value">{deck.total_games}</span>
                </div>
                <div className="stat">
                  <span className="stat-label">Wins / Losses</span>
                  <span className="stat-value">{deck.wins}W - {deck.losses}L{deck.draws > 0 ? ` - ${deck.draws}D` : ''}</span>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};

export default DeckPerformance;
