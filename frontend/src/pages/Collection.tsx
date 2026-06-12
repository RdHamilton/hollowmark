import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { trackEvent } from '@/services/analytics';
import { collection, cards as cardsApi } from '@/services/api';
import { gui } from '@/types/models';
import { useDownload } from '@/context/DownloadContext';
import SetCompletionPanel from '../components/SetCompletion';
import WildcardAdvisorPanel from '../components/WildcardAdvisorPanel';
import './Collection.css';

// Color icon mapping
const colorIcons: Record<string, string> = {
  W: 'https://svgs.scryfall.io/card-symbols/W.svg',
  U: 'https://svgs.scryfall.io/card-symbols/U.svg',
  B: 'https://svgs.scryfall.io/card-symbols/B.svg',
  R: 'https://svgs.scryfall.io/card-symbols/R.svg',
  G: 'https://svgs.scryfall.io/card-symbols/G.svg',
};

// Rarity colors
const rarityColors: Record<string, string> = {
  common: '#1a1a1a',
  uncommon: '#6b7c8d',
  rare: '#d4af37',
  mythic: '#e67e22',
};

interface FilterState {
  searchTerm: string;
  setCode: string;
  rarity: string;
  colors: string[];
  ownedOnly: boolean;
  sortBy: string;
  sortDesc: boolean;
}

const ITEMS_PER_PAGE = 50;

export default function Collection() {
  const [cards, setCards] = useState<gui.CollectionCard[]>([]);
  const [sets, setSets] = useState<gui.SetInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  // totalCount = UniqueCards (all owned, no filter) — "Total Cards:" header
  const [totalCount, setTotalCount] = useState(0);
  // filterCount = cards matching current filter — "Cards in Set:" and "Showing X of Y"
  const [filterCount, setFilterCount] = useState(0);
  // totalPages is server-computed
  const [totalPages, setTotalPages] = useState(1);
  const [currentPage, setCurrentPage] = useState(1);
  const [pageJumpInput, setPageJumpInput] = useState<string>('1');
  const [showSetCompletion, setShowSetCompletion] = useState(false);
  const [showWildcardAdvisor, setShowWildcardAdvisor] = useState(false);
  const [collectionValue, setCollectionValue] = useState<{ totalValueUsd: number } | null>(null);

  const { startDownload, updateProgress, completeDownload } = useDownload();
  const autoRefreshRef = useRef<boolean>(false);
  const isLoadingRef = useRef<boolean>(false);
  const isAutoRefreshingRef = useRef<boolean>(false);
  const autoRefreshTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const viewedFiredRef = useRef(false);

  const [filters, setFilters] = useState<FilterState>({
    searchTerm: '',
    setCode: '',
    rarity: '',
    colors: [],
    ownedOnly: true,
    sortBy: 'name',
    sortDesc: false,
  });

  // Debounced search term — debounce before triggering an API call
  const [debouncedSearchTerm, setDebouncedSearchTerm] = useState('');

  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedSearchTerm(filters.searchTerm);
    }, 300);
    return () => clearTimeout(timer);
  }, [filters.searchTerm]);

  const loadCollection = useCallback(async (isAutoRefresh = false) => {
    if (isLoadingRef.current) {
      return;
    }

    isLoadingRef.current = true;
    isAutoRefreshingRef.current = isAutoRefresh;

    if (!isAutoRefresh) {
      setLoading(true);
    }
    setError(null);
    try {
      const apiFilter = {
        set_code: filters.setCode,
        rarity: filters.rarity,
        colors: filters.colors,
        owned_only: filters.ownedOnly,
        // Server-side search + sort (#1325)
        search: debouncedSearchTerm || undefined,
        sort_by: filters.sortBy,
        sort_desc: filters.sortDesc,
        // Pagination (#1325)
        page: currentPage,
        limit: ITEMS_PER_PAGE,
      };

      const response = await collection.getCollectionWithMetadata(apiFilter);
      const normalizedCards = Array.isArray(response?.cards) ? response.cards : [];
      setCards(normalizedCards);
      // totalCount and filterCount come from the server response, not array.length.
      // Defensive fallbacks guard against partial mocks in tests.
      setTotalCount(response?.totalCount ?? 0);
      setFilterCount(response?.filterCount ?? 0);
      setTotalPages(response?.totalPages ?? 1);

      // Analytics: feature_collection_viewed — once per mount when data is non-empty
      if (normalizedCards.length > 0 && !viewedFiredRef.current) {
        viewedFiredRef.current = true;
        trackEvent({
          name: 'feature_collection_viewed',
          properties: { card_count: response.totalCount },
        });
      }

      // Show download progress if cards were fetched from Scryfall
      const unknownFetched = response?.unknownCardsFetched ?? 0;
      const unknownRemaining = response?.unknownCardsRemaining ?? 0;
      const downloadId = 'collection-card-lookup';

      if (unknownFetched > 0 && unknownRemaining > 0) {
        const totalUnknown = unknownRemaining + unknownFetched;
        const progress = Math.round(((totalUnknown - unknownRemaining) / totalUnknown) * 100);

        startDownload(downloadId, `Fetching card info from Scryfall...`);
        updateProgress(downloadId, progress);

        if (!autoRefreshRef.current) {
          autoRefreshRef.current = true;
          autoRefreshTimeoutRef.current = setTimeout(() => {
            autoRefreshRef.current = false;
            autoRefreshTimeoutRef.current = null;
            loadCollection(true);
          }, 500);
        }
      } else if (unknownFetched > 0) {
        completeDownload(downloadId);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load collection');
      console.error('Failed to load collection:', err);
    } finally {
      isLoadingRef.current = false;
      isAutoRefreshingRef.current = false;
      setLoading(false);
    }
  }, [
    filters.setCode, filters.rarity, filters.colors, filters.ownedOnly,
    filters.sortBy, filters.sortDesc,
    debouncedSearchTerm, currentPage,
    startDownload, updateProgress, completeDownload,
  ]);

  const loadSets = useCallback(async () => {
    try {
      const setInfo = await cardsApi.getAllSetInfo();
      setSets(Array.isArray(setInfo) ? setInfo : []);
    } catch (err) {
      console.error('Failed to load sets:', err);
    }
  }, []);

  const loadCollectionValue = useCallback(async () => {
    try {
      const value = await collection.getCollectionValue();
      setCollectionValue(value);
    } catch (err) {
      console.error('Failed to load collection value:', err);
    }
  }, []);

  // Load collection, sets, and value on mount
  useEffect(() => {
    loadCollection();
    loadSets();
    loadCollectionValue();

    return () => {
      if (autoRefreshTimeoutRef.current) {
        clearTimeout(autoRefreshTimeoutRef.current);
        autoRefreshTimeoutRef.current = null;
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Reload collection when filters (server-side ones) or page changes.
  // loadCollection captures all its deps via useCallback — this effect fires
  // whenever any server-side filter or the page number changes.
  const isInitialMount = useRef(true);
  useEffect(() => {
    if (isInitialMount.current) {
      isInitialMount.current = false;
      return;
    }
    loadCollection();
  }, [loadCollection]);

  // Reset to page 1 when filters change (not on page navigation itself)
  useEffect(() => {
    setCurrentPage(1);
  }, [debouncedSearchTerm, filters.setCode, filters.rarity, filters.colors, filters.ownedOnly, filters.sortBy, filters.sortDesc]);

  const handleFilterChange = (key: keyof FilterState, value: string | string[] | boolean) => {
    setFilters((prev) => ({ ...prev, [key]: value }));
  };

  const handleColorToggle = (color: string) => {
    setFilters((prev) => ({
      ...prev,
      colors: prev.colors.includes(color)
        ? prev.colors.filter((c) => c !== color)
        : [...prev.colors, color],
    }));
  };

  // Keep page-jump input in sync when page changes via First/Prev/Next/Last
  useEffect(() => {
    setPageJumpInput(String(currentPage));
  }, [currentPage]);

  const handlePageJump = useCallback(() => {
    const parsed = parseInt(pageJumpInput, 10);
    if (isNaN(parsed) || parsed < 1 || parsed > totalPages) {
      setPageJumpInput(String(currentPage));
      return;
    }
    setCurrentPage(parsed);
  }, [pageJumpInput, currentPage, totalPages]);

  // Build windowed page buttons: show ±2 pages around current (AC standard pattern)
  const windowedPages = useMemo(() => {
    const pages: number[] = [];
    const start = Math.max(1, currentPage - 2);
    const end = Math.min(totalPages, currentPage + 2);
    for (let p = start; p <= end; p++) {
      pages.push(p);
    }
    return pages;
  }, [currentPage, totalPages]);

  if (loading && cards.length === 0) {
    return (
      <div className="collection-page loading-state" data-testid="collection-loading">
        <div className="loading-spinner"></div>
        <p>Loading collection...</p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="collection-page error-state" data-testid="collection-error">
        <div className="error-icon">!</div>
        <h2>Error Loading Collection</h2>
        <p>{error}</p>
        <button onClick={() => loadCollection()} className="retry-button" data-testid="collection-retry-button">
          Retry
        </button>
      </div>
    );
  }

  return (
    <div className="collection-page" data-testid="collection-page">
      {/* Header with stats */}
      <div className="collection-header" data-testid="collection-header">
        <div className="header-title">
          <h1>Collection</h1>
          <div className="collection-stats-summary" data-testid="collection-stats">
            <span className="stat-item">
              <span className="stat-label">Cards in Set:</span>
              <span className="stat-value">{filterCount.toLocaleString()}</span>
            </span>
            <span className="stat-separator">|</span>
            <span className="stat-item">
              <span className="stat-label">Total Cards:</span>
              <span className="stat-value">{totalCount.toLocaleString()}</span>
            </span>
            {collectionValue && collectionValue.totalValueUsd > 0 && (
              <>
                <span className="stat-separator">|</span>
                <span className="stat-item collection-value">
                  <span className="stat-label">Est. Value:</span>
                  <span className="stat-value price-value">
                    ${collectionValue.totalValueUsd.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })}
                  </span>
                </span>
              </>
            )}
          </div>
        </div>
        <button
          className="set-completion-button"
          onClick={() => setShowWildcardAdvisor(!showWildcardAdvisor)}
          data-testid="collection-toggle-wildcard-advisor"
        >
          {showWildcardAdvisor ? 'Hide' : 'Show'} Wildcard Advisor
        </button>
        {filters.setCode && (
          <button
            className="set-completion-button"
            onClick={() => setShowSetCompletion(!showSetCompletion)}
            data-testid="collection-toggle-set-completion"
          >
            {showSetCompletion ? 'Hide' : 'Show'} Set Completion
          </button>
        )}
      </div>

      {/* Filter Controls */}
      <div className="collection-filters">
        <div className="filter-row">
          {/* Search */}
          <div className="filter-group search-group">
            <input
              type="text"
              placeholder="Search by name..."
              value={filters.searchTerm}
              onChange={(e) => handleFilterChange('searchTerm', e.target.value)}
              className="search-input"
              data-testid="collection-search-input"
            />
          </div>

          {/* Set Filter */}
          <div className="filter-group">
            <select
              value={filters.setCode}
              onChange={(e) => handleFilterChange('setCode', e.target.value)}
              className="filter-select"
              data-testid="collection-set-filter"
            >
              <option value="">All Sets</option>
              {sets.map((set) => (
                <option key={set.code} value={set.code}>
                  {set.name} ({set.code.toUpperCase()})
                </option>
              ))}
            </select>
          </div>

          {/* Rarity Filter */}
          <div className="filter-group">
            <select
              value={filters.rarity}
              onChange={(e) => handleFilterChange('rarity', e.target.value)}
              className="filter-select"
              data-testid="collection-rarity-filter"
            >
              <option value="">All Rarities</option>
              <option value="common">Common</option>
              <option value="uncommon">Uncommon</option>
              <option value="rare">Rare</option>
              <option value="mythic">Mythic</option>
            </select>
          </div>

          {/* Sort */}
          <div className="filter-group">
            <select
              value={`${filters.sortBy}-${filters.sortDesc ? 'desc' : 'asc'}`}
              onChange={(e) => {
                const [sortBy, direction] = e.target.value.split('-');
                handleFilterChange('sortBy', sortBy);
                handleFilterChange('sortDesc', direction === 'desc');
              }}
              className="filter-select"
              data-testid="collection-sort-select"
            >
              <option value="name-asc">Name (A-Z)</option>
              <option value="name-desc">Name (Z-A)</option>
              <option value="quantity-desc">Quantity (High)</option>
              <option value="quantity-asc">Quantity (Low)</option>
              <option value="rarity-desc">Rarity (High)</option>
              <option value="rarity-asc">Rarity (Low)</option>
              <option value="cmc-asc">CMC (Low)</option>
              <option value="cmc-desc">CMC (High)</option>
              <option value="price-desc">Price (High)</option>
              <option value="price-asc">Price (Low)</option>
            </select>
          </div>
        </div>

        <div className="filter-row secondary">
          {/* Color Filters */}
          <span className="filter-label">Colors:</span>
          <div className="color-buttons">
            {['W', 'U', 'B', 'R', 'G'].map((color) => (
              <button
                key={color}
                className={`color-button ${filters.colors.includes(color) ? 'active' : ''}`}
                onClick={() => handleColorToggle(color)}
                title={color === 'W' ? 'White' : color === 'U' ? 'Blue' : color === 'B' ? 'Black' : color === 'R' ? 'Red' : 'Green'}
                data-testid={`collection-color-button-${color}`}
              >
                <img src={colorIcons[color]} alt={color} className="color-icon" />
              </button>
            ))}
          </div>

          {/* Owned Only Toggle */}
          <label className="toggle-label">
            <input
              type="checkbox"
              checked={filters.ownedOnly}
              onChange={(e) => handleFilterChange('ownedOnly', e.target.checked)}
              data-testid="collection-owned-only-checkbox"
            />
            Owned only
          </label>

          {/* Result Count */}
          <div className="filter-results">
            Showing {filterCount.toLocaleString()} of {totalCount.toLocaleString()} cards
          </div>
        </div>
      </div>

      {/* Wildcard Advisor Panel */}
      {showWildcardAdvisor && (
        <div className="wildcard-advisor-container" data-testid="collection-wildcard-advisor-container">
          <WildcardAdvisorPanel
            onClose={() => setShowWildcardAdvisor(false)}
          />
        </div>
      )}

      {/* Set Completion Panel */}
      {showSetCompletion && filters.setCode && (
        <div className="set-completion-container">
          <SetCompletionPanel
            setCode={filters.setCode}
            onClose={() => setShowSetCompletion(false)}
          />
        </div>
      )}

      {/* Card Grid */}
      {cards.length === 0 ? (
        <div className="empty-state" data-testid="collection-empty">
          <div className="empty-icon">!</div>
          <h2>No Cards Found</h2>
          <p>
            {filters.searchTerm || filters.setCode || filters.rarity || filters.colors.length > 0
              ? 'Try adjusting your filters'
              : 'Your collection is empty. Start playing to add cards!'}
          </p>
        </div>
      ) : (
        <>
          <div className="card-grid" data-testid="collection-card-grid">
            {cards.map((card) => {
              // Check if we have a real card image (not the card back placeholder)
              const isCardBackPlaceholder = card.imageUri?.includes('back.png');
              const hasImage = card.imageUri && card.imageUri !== '' && !isCardBackPlaceholder;
              return (
                <div
                  key={`${card.cardId}-${card.setCode}`}
                  className={`collection-card ${card.quantity === 0 ? 'not-owned' : ''} ${!hasImage ? 'no-image' : ''}`}
                >
                  {hasImage ? (
                    <>
                      <img
                        src={card.imageUri}
                        alt={card.name || `Card #${card.arenaId}`}
                        style={{ width: '100%', borderRadius: '12px' }}
                        onError={(e) => {
                          const target = e.target as HTMLImageElement;
                          target.style.display = 'none';
                          const parent = target.parentElement;
                          if (parent && !parent.querySelector('.card-info-fallback')) {
                            parent.classList.add('no-image');
                          }
                        }}
                      />
                      {card.priceUsd !== undefined && card.priceUsd > 0 && (
                        <div className="card-price-badge">
                          ${card.priceUsd.toFixed(2)}
                        </div>
                      )}
                    </>
                  ) : (
                    <div className="card-info-fallback">
                      <div className="card-fallback-name">{card.name || 'Unknown Card'}</div>
                      {card.setCode ? (
                        <div className="card-fallback-set">{card.setCode.toUpperCase()}</div>
                      ) : (
                        <div className="card-fallback-hint">
                          Card #{card.arenaId}
                          <br />
                          <span className="download-hint">Download set in Settings</span>
                        </div>
                      )}
                      {card.manaCost && <div className="card-fallback-mana">{card.manaCost}</div>}
                      {card.rarity && (
                        <div
                          className="card-fallback-rarity"
                          style={{ color: rarityColors[card.rarity.toLowerCase()] || '#888' }}
                        >
                          {card.rarity}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>

          {/* Pagination — totalPages is server-computed from filterCount/limit */}
          {totalPages > 1 && (
            <div className="pagination">
              <button
                className="page-button"
                disabled={currentPage === 1}
                onClick={() => setCurrentPage(1)}
                data-testid="collection-pagination-first"
              >
                First
              </button>
              <button
                className="page-button"
                disabled={currentPage === 1}
                onClick={() => setCurrentPage((p) => Math.max(1, p - 1))}
                data-testid="collection-pagination-prev"
              >
                Previous
              </button>

              {/* Windowed page buttons ±2 around current page */}
              {windowedPages[0] > 1 && <span className="page-ellipsis">…</span>}
              {windowedPages.map((p) => (
                <button
                  key={p}
                  className={`page-button${p === currentPage ? ' page-button--active' : ''}`}
                  onClick={() => setCurrentPage(p)}
                  data-testid={p === currentPage ? 'collection-pagination-current' : undefined}
                  aria-current={p === currentPage ? 'page' : undefined}
                >
                  {p}
                </button>
              ))}
              {windowedPages[windowedPages.length - 1] < totalPages && <span className="page-ellipsis">…</span>}

              {/* Page-jump input (AC1/AC2/AC3) */}
              <label className="page-jump-label" htmlFor="collection-page-jump">
                Go to page
                <input
                  id="collection-page-jump"
                  className="page-jump-input"
                  type="number"
                  min={1}
                  max={totalPages}
                  value={pageJumpInput}
                  data-testid="collection-page-jump"
                  onChange={(e) => setPageJumpInput(e.target.value)}
                  onBlur={handlePageJump}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') handlePageJump();
                  }}
                />
              </label>

              <button
                className="page-button"
                disabled={currentPage === totalPages}
                onClick={() => setCurrentPage((p) => Math.min(totalPages, p + 1))}
                data-testid="collection-pagination-next"
              >
                Next
              </button>
              <button
                className="page-button"
                disabled={currentPage === totalPages}
                onClick={() => setCurrentPage(totalPages)}
                data-testid="collection-pagination-last"
              >
                Last
              </button>
            </div>
          )}
        </>
      )}

      {/* Loading overlay for filter changes */}
      {loading && cards.length > 0 && (
        <div className="loading-overlay">
          <div className="loading-spinner small"></div>
        </div>
      )}
    </div>
  );
}
