package scraper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// goldfishBaseURL is the MTGGoldfish target host. It is a compile-time constant
// — not caller-controlled, not config-derived, not DB-derived (control SS-1).
// The only override is in tests, which point BaseURL at an httptest.Server.
const goldfishBaseURL = "https://www.mtggoldfish.com"

// maxResponseBytes caps the HTTP response body read before HTML parsing
// (control HP-1). External HTML is untrusted; a 10 MB cap bounds Lambda memory.
const maxResponseBytes = 10 << 20 // 10 MB

// defaultMaxDecklistFetches is the per-format cap on decklist page fetches
// (#384). The metagame page lists up to 60 archetypes; fetching every one at
// 1 req/s × 10 formats = 600 s — tight against the 900 s Lambda timeout. Capping
// at 25 per format (= 250 fetches total) keeps the worst-case run under 310 s
// while still covering every Tier 1 / Tier 2 archetype the wildcard advisor needs.
const defaultMaxDecklistFetches = 25

// decklistInputPattern matches the hidden textarea used by the MTGGoldfish
// "copy deck" form. The value attribute contains a plain-text decklist:
//
//	N CardName\n…\nsideboard\nN CardName\n…
//
// Both single and double quotes are handled by the alternation. HTML entities
// (e.g. &#39; for apostrophe) are present in card names and are unescaped after
// extraction.
var decklistInputPattern = regexp.MustCompile(
	`<input[^>]*\bname=["']deck_input\[deck\]["'][^>]*\bvalue=["']([\s\S]*?)["']`,
)

// GoldfishClient fetches meta data from MTGGoldfish.
type GoldfishClient struct {
	httpClient         *http.Client
	baseURL            string
	cache              *MetaCache
	cacheTTL           time.Duration
	rateLimiter        *time.Ticker
	lastRequest        time.Time
	maxDecklistFetches int
	mu                 sync.Mutex
}

// GoldfishConfig configures the Goldfish client.
type GoldfishConfig struct {
	// BaseURL is the MTGGoldfish base URL.
	BaseURL string

	// CacheTTL is how long to cache meta data.
	CacheTTL time.Duration

	// RequestTimeout is the HTTP request timeout.
	RequestTimeout time.Duration

	// RateLimitMs is minimum milliseconds between requests.
	RateLimitMs int

	// MaxDecklistFetches is the maximum number of individual archetype decklist
	// pages to fetch per format scrape (#384). 0 means use defaultMaxDecklistFetches.
	// Set to a negative value to disable decklist fetching entirely (cards will be
	// empty, which is the pre-#384 behaviour).
	MaxDecklistFetches int
}

// DefaultGoldfishConfig returns default configuration.
func DefaultGoldfishConfig() *GoldfishConfig {
	return &GoldfishConfig{
		BaseURL:            goldfishBaseURL,
		CacheTTL:           4 * time.Hour,
		RequestTimeout:     30 * time.Second,
		RateLimitMs:        1000,
		MaxDecklistFetches: defaultMaxDecklistFetches,
	}
}

// MetaDeck represents a deck in the meta.
type MetaDeck struct {
	Name           string     `json:"name"`
	ArchetypeName  string     `json:"archetype_name"`
	Format         string     `json:"format"`
	Tier           int        `json:"tier"` // 1, 2, 3, or 0 for untiered
	MetaShare      float64    `json:"meta_share"`
	WinRate        float64    `json:"win_rate,omitempty"`
	MatchCount     int        `json:"match_count,omitempty"`
	Colors         []string   `json:"colors"`
	MainboardCards []DeckCard `json:"mainboard,omitempty"`
	SideboardCards []DeckCard `json:"sideboard,omitempty"`
	URL            string     `json:"url,omitempty"`
	LastUpdated    time.Time  `json:"last_updated"`
}

// DeckCard represents a card in a deck list.
type DeckCard struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	ArenaID  int    `json:"arena_id,omitempty"`
}

// FormatMeta represents the meta for a specific format.
type FormatMeta struct {
	Format      string      `json:"format"`
	Decks       []*MetaDeck `json:"decks"`
	TotalDecks  int         `json:"total_decks"`
	LastUpdated time.Time   `json:"last_updated"`
	Source      string      `json:"source"`
}

// MetaCache caches meta data.
type MetaCache struct {
	data map[string]*CacheEntry
	mu   sync.RWMutex
}

// CacheEntry represents a cached meta entry.
type CacheEntry struct {
	Meta      *FormatMeta
	ExpiresAt time.Time
}

// NewGoldfishClient creates a new MTGGoldfish client.
func NewGoldfishClient(config *GoldfishConfig) *GoldfishClient {
	if config == nil {
		config = DefaultGoldfishConfig()
	}

	maxDecklist := config.MaxDecklistFetches
	if maxDecklist == 0 {
		maxDecklist = defaultMaxDecklistFetches
	}

	return &GoldfishClient{
		httpClient: &http.Client{
			Timeout: config.RequestTimeout,
		},
		baseURL:            config.BaseURL,
		cacheTTL:           config.CacheTTL,
		rateLimiter:        time.NewTicker(time.Duration(config.RateLimitMs) * time.Millisecond),
		maxDecklistFetches: maxDecklist,
		cache: &MetaCache{
			data: make(map[string]*CacheEntry),
		},
	}
}

// GetMeta retrieves meta data for a format.
func (c *GoldfishClient) GetMeta(ctx context.Context, format string) (*FormatMeta, error) {
	// Check cache first
	if cached := c.getFromCache(format); cached != nil {
		return cached, nil
	}

	// Rate limit
	c.waitForRateLimit()

	// Fetch from MTGGoldfish
	meta, err := c.fetchMeta(ctx, format)
	if err != nil {
		return nil, err
	}

	// Cache result
	c.setCache(format, meta)

	return meta, nil
}

// GetTopDecks returns the top N decks for a format.
func (c *GoldfishClient) GetTopDecks(ctx context.Context, format string, limit int) ([]*MetaDeck, error) {
	meta, err := c.GetMeta(ctx, format)
	if err != nil {
		return nil, err
	}

	if limit <= 0 || limit > len(meta.Decks) {
		return meta.Decks, nil
	}

	return meta.Decks[:limit], nil
}

// GetDeckByArchetype finds a deck by archetype name.
func (c *GoldfishClient) GetDeckByArchetype(ctx context.Context, format, archetype string) (*MetaDeck, error) {
	meta, err := c.GetMeta(ctx, format)
	if err != nil {
		return nil, err
	}

	archetypeLower := strings.ToLower(archetype)
	for _, deck := range meta.Decks {
		if strings.ToLower(deck.ArchetypeName) == archetypeLower ||
			strings.ToLower(deck.Name) == archetypeLower {
			return deck, nil
		}
	}

	return nil, fmt.Errorf("archetype not found: %s", archetype)
}

// GetMetaShare returns the meta share percentage for an archetype.
func (c *GoldfishClient) GetMetaShare(ctx context.Context, format, archetype string) (float64, error) {
	deck, err := c.GetDeckByArchetype(ctx, format, archetype)
	if err != nil {
		return 0, err
	}
	return deck.MetaShare, nil
}

// fetchMeta fetches meta data from MTGGoldfish.
func (c *GoldfishClient) fetchMeta(ctx context.Context, format string) (*FormatMeta, error) {
	// Map format names to MTGGoldfish URLs
	formatURLs := map[string]string{
		"standard": "/metagame/standard/full",
		"historic": "/metagame/historic/full",
		"explorer": "/metagame/explorer/full",
		"pioneer":  "/metagame/pioneer/full",
		"modern":   "/metagame/modern/full",
		"legacy":   "/metagame/legacy/full",
		"vintage":  "/metagame/vintage/full",
		"pauper":   "/metagame/pauper/full",
		"alchemy":  "/metagame/alchemy/full",
		"timeless": "/metagame/timeless/full",
	}

	urlPath, ok := formatURLs[strings.ToLower(format)]
	if !ok {
		return nil, fmt.Errorf("unsupported format: %s", format)
	}

	url := c.baseURL + urlPath

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	// HP-1: cap the response body at 10 MB before parsing untrusted HTML.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse the HTML response (name, meta share, archetype URL path).
	meta := c.parseMetaPage(string(body), format)
	meta.Source = "mtggoldfish"
	meta.LastUpdated = time.Now()

	// Fetch per-archetype decklist pages and populate card lists (#384).
	// Disabled when maxDecklistFetches < 0 (pre-#384 / test-only behaviour).
	if c.maxDecklistFetches >= 0 {
		limit := c.maxDecklistFetches
		if limit == 0 {
			limit = defaultMaxDecklistFetches
		}
		for i, deck := range meta.Decks {
			if i >= limit {
				break
			}
			if deck.URL == "" {
				continue
			}
			c.waitForRateLimit()
			mainboard, sideboard, dlErr := c.fetchDecklistPage(ctx, deck.URL)
			if dlErr != nil {
				// A decklist fetch failure is non-fatal: we keep the archetype
				// row with an empty card list rather than aborting the whole
				// format scrape (mirrors AC3 source-failure resilience).
				continue
			}
			deck.MainboardCards = mainboard
			deck.SideboardCards = sideboard
		}
	}

	return meta, nil
}

// archetypeTilePattern matches archetype tiles containing name, URL path, and
// meta-share percentage. Group layout:
//
//	[1] archetype URL path   e.g. /archetype/standard-izzet-prowess-woe
//	[2] deck name            e.g. Izzet Prowess
//	[3] meta share           e.g. 11.1
//
// The URL is captured from the first <a href="..."> inside the archetype-tile-title
// div; the #online / #paper fragment is stripped by the caller. Both single and
// double quoted class attributes are handled via ['\"].
var archetypeTilePattern = regexp.MustCompile(
	`(?s)<div[^>]*class=['""][^'"]*archetype-tile[^'"]*['""][^>]*>` +
		`.*?<div[^>]*class=['""][^'"]*archetype-tile-title[^'"]*['""][^>]*>` +
		`.*?<a[^>]*href=["']([^"'#]+)[^"']*["'][^>]*>([^<]+)</a>` +
		`.*?<div[^>]*class=['""][^'"]*archetype-tile-statistic-value[^'"]*['""][^>]*>\s*(\d+\.?\d*)%`,
)

// archetypeFallbackPattern is used when archetypeTilePattern finds no matches.
// It does not capture the URL path (the old behaviour) — it is retained as a
// safety net for markup changes that would break the primary pattern.
var archetypeFallbackPattern = regexp.MustCompile(
	`(?s)<div[^>]*class=['""][^'"]*archetype-tile-title[^'"]*['""][^>]*>` +
		`.*?<a[^>]*href=['""][^'"]*['""][^>]*>([^<]+)</a>` +
		`.*?<div[^>]*class=['""][^'"]*metagame-percentage[^'"]*['""][^>]*>` +
		`.*?<div[^>]*class=['""][^'"]*archetype-tile-statistic-value[^'"]*['""][^>]*>\s*(\d+\.?\d*)%`,
)

// archetypeTablePattern matches the older table-layout metagame page.
var archetypeTablePattern = regexp.MustCompile(
	`(?s)<tr[^>]*>.*?<a[^>]*href="/archetype/([^"#]+)[^"]*"[^>]*>([^<]+)</a>.*?<td[^>]*>(\d+\.?\d*)%</td>`,
)

// parseMetaPage parses the MTGGoldfish meta page HTML.
//
// MTGGoldfish structure (as of 2025):
//
//	<div class='archetype-tile' id='28086'>
//	  <div class='archetype-tile-title'>
//	    <a href="/archetype/standard-izzet-prowess-woe#online">Izzet Prowess</a>
//	  </div>
//	  <div class='archetype-tile-statistic metagame-percentage'>
//	    <div class='archetype-tile-statistic-value'>11.1%</div>
//	  </div>
//	</div>
//
// The URL path (stripped of the fragment) is stored in MetaDeck.URL and used
// by fetchDecklistPage to populate card lists (#384).
func (c *GoldfishClient) parseMetaPage(html, format string) *FormatMeta {
	meta := &FormatMeta{
		Format: format,
		Decks:  make([]*MetaDeck, 0),
	}

	// tileMatch wraps a raw regexp submatch for uniform access regardless of
	// which pattern fired. Fields: urlPath, name, shareStr.
	type tileMatch struct {
		urlPath  string
		name     string
		shareStr string
	}

	// Try primary tile pattern first (captures URL path).
	rawMatches := archetypeTilePattern.FindAllStringSubmatch(html, -1)
	primary := len(rawMatches) > 0
	tiles := make([]tileMatch, 0, len(rawMatches))
	for _, m := range rawMatches {
		if len(m) < 4 {
			continue
		}
		tiles = append(tiles, tileMatch{
			urlPath:  strings.TrimSpace(m[1]),
			name:     strings.TrimSpace(m[2]),
			shareStr: strings.TrimSpace(m[3]),
		})
	}

	// Fallback: old tile pattern (no URL path).
	if !primary {
		rawFallback := archetypeFallbackPattern.FindAllStringSubmatch(html, -1)
		for _, m := range rawFallback {
			if len(m) < 3 {
				continue
			}
			tiles = append(tiles, tileMatch{
				name:     strings.TrimSpace(m[1]),
				shareStr: strings.TrimSpace(m[2]),
			})
		}
	}

	// Fallback: table format (URL path in group 1 with no /archetype/ prefix).
	if len(tiles) == 0 {
		rawTable := archetypeTablePattern.FindAllStringSubmatch(html, -1)
		for _, m := range rawTable {
			if len(m) < 4 {
				continue
			}
			tiles = append(tiles, tileMatch{
				urlPath:  "/archetype/" + strings.TrimSpace(m[1]),
				name:     strings.TrimSpace(m[2]),
				shareStr: strings.TrimSpace(m[3]),
			})
		}
	}

	tierThresholds := []float64{5.0, 2.0, 0.5} // Tier 1 > 5%, Tier 2 > 2%, Tier 3 > 0.5%

	for i, t := range tiles {
		shareStr := strings.TrimSuffix(t.shareStr, "%")
		share, err := strconv.ParseFloat(shareStr, 64)
		if err != nil {
			continue
		}

		// Determine tier from meta share.
		tier := len(tierThresholds) + 1
		for j, threshold := range tierThresholds {
			if share >= threshold {
				tier = j + 1
				break
			}
		}

		deck := &MetaDeck{
			Name:          t.name,
			ArchetypeName: c.normalizeArchetypeName(t.name),
			Format:        format,
			Tier:          tier,
			MetaShare:     share,
			Colors:        c.extractColorsFromName(t.name),
			URL:           t.urlPath,
			LastUpdated:   time.Now(),
		}

		meta.Decks = append(meta.Decks, deck)

		// Limit to top 50 decks.
		if i >= 49 {
			break
		}
	}

	meta.TotalDecks = len(meta.Decks)

	return meta
}

// fetchDecklistPage fetches an individual archetype page and parses the embedded
// "deck_input[deck]" hidden input value, which MTGGoldfish renders as a plain-text
// decklist in the format:
//
//	N CardName\n…\nsideboard\nN CardName\n…
//
// Returned slices are nil when no decklist input is found on the page. A non-200
// response returns an error without side-effects (the caller treats it as a
// non-fatal skip, per AC3 source-failure resilience). HTML entities in card names
// (e.g. &#39; → ') are decoded before the card names are stored.
func (c *GoldfishClient) fetchDecklistPage(ctx context.Context, urlPath string) (mainboard, sideboard []DeckCard, err error) {
	url := c.baseURL + urlPath
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build decklist request for %q: %w", urlPath, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch decklist %q: %w", urlPath, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("decklist %q: status %d", urlPath, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("read decklist body %q: %w", urlPath, err)
	}

	return parseDecklistPage(string(body))
}

// parseDecklistPage extracts mainboard and sideboard card lists from an
// MTGGoldfish archetype page. It looks for the hidden input whose name
// attribute is "deck_input[deck]" and parses its value.
//
// Lines before "sideboard" are mainboard entries; lines after are sideboard.
// Each entry is in the form "N Card Name" where N is an integer quantity.
// Empty lines and lines that do not start with a digit are skipped.
// HTML entities (&#39;, &amp;, &quot;) are decoded before parsing.
func parseDecklistPage(html string) (mainboard, sideboard []DeckCard, err error) {
	m := decklistInputPattern.FindStringSubmatch(html)
	if m == nil {
		return nil, nil, nil // page has no deck list — not an error
	}

	raw := decodeHTMLEntities(m[1])

	const sideboardMarker = "sideboard"
	inSideboard := false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.ToLower(line) == sideboardMarker {
			inSideboard = true
			continue
		}
		card, parseErr := parseDeckListLine(line)
		if parseErr != nil {
			continue // skip malformed lines silently
		}
		if inSideboard {
			sideboard = append(sideboard, card)
		} else {
			mainboard = append(mainboard, card)
		}
	}

	return mainboard, sideboard, nil
}

// parseDeckListLine parses a single "N Card Name" line from a decklist.
// Returns an error for lines that don't match this format.
func parseDeckListLine(line string) (DeckCard, error) {
	spaceIdx := strings.IndexByte(line, ' ')
	if spaceIdx < 1 {
		return DeckCard{}, fmt.Errorf("no space in line %q", line)
	}
	qty, err := strconv.Atoi(line[:spaceIdx])
	if err != nil || qty <= 0 {
		return DeckCard{}, fmt.Errorf("non-integer quantity in %q", line)
	}
	name := strings.TrimSpace(line[spaceIdx+1:])
	if name == "" {
		return DeckCard{}, fmt.Errorf("empty card name in %q", line)
	}
	return DeckCard{Name: name, Quantity: qty}, nil
}

// decodeHTMLEntities replaces the small set of HTML entities that appear in
// MTGGoldfish decklist values (card names containing apostrophes, ampersands,
// and quotes). Using strings.Replacer instead of html.UnescapeString keeps the
// import graph clean (no html package dependency in this file).
func decodeHTMLEntities(s string) string {
	r := strings.NewReplacer(
		"&#39;", "'",
		"&amp;", "&",
		"&quot;", `"`,
		"&lt;", "<",
		"&gt;", ">",
		"&#34;", `"`,
		"&#38;", "&",
		"&#60;", "<",
		"&#62;", ">",
	)
	return r.Replace(s)
}

// extractColorsFromName attempts to extract color identity from a deck name.
func (c *GoldfishClient) extractColorsFromName(name string) []string {
	nameLower := strings.ToLower(name)
	colors := make([]string, 0)

	// Color word mappings
	colorMappings := map[string]string{
		"white": "W", "mono-white": "W", "mono white": "W",
		"blue": "U", "mono-blue": "U", "mono blue": "U",
		"black": "B", "mono-black": "B", "mono black": "B",
		"red": "R", "mono-red": "R", "mono red": "R",
		"green": "G", "mono-green": "G", "mono green": "G",
		// Guild names
		"azorius": "WU", "dimir": "UB", "rakdos": "BR",
		"gruul": "RG", "selesnya": "WG", "orzhov": "WB",
		"izzet": "UR", "golgari": "BG", "boros": "WR",
		"simic": "UG",
		// Shard/Wedge names
		"esper": "WUB", "grixis": "UBR", "jund": "BRG",
		"naya": "WRG", "bant": "WUG", "abzan": "WBG",
		"jeskai": "WUR", "sultai": "UBG", "mardu": "WBR",
		"temur": "URG",
		// 4-color
		"glint": "UBRG", "dune": "WBRG", "ink": "WURG",
		"witch": "WUBG", "yore": "WUBR",
		// 5-color
		"five-color": "WUBRG", "5-color": "WUBRG", "5c": "WUBRG",
	}

	for word, colorStr := range colorMappings {
		if strings.Contains(nameLower, word) {
			for _, c := range colorStr {
				color := string(c)
				found := false
				for _, existing := range colors {
					if existing == color {
						found = true
						break
					}
				}
				if !found {
					colors = append(colors, color)
				}
			}
			break
		}
	}

	return colors
}

// normalizeArchetypeName normalizes an archetype name for comparison.
func (c *GoldfishClient) normalizeArchetypeName(name string) string {
	// Remove common suffixes and prefixes
	normalized := strings.ToLower(name)
	normalized = strings.TrimSpace(normalized)

	// Remove format prefixes
	prefixes := []string{"standard ", "historic ", "explorer ", "pioneer ", "modern "}
	for _, prefix := range prefixes {
		normalized = strings.TrimPrefix(normalized, prefix)
	}

	return normalized
}

// waitForRateLimit waits for rate limiting.
func (c *GoldfishClient) waitForRateLimit() {
	c.mu.Lock()
	defer c.mu.Unlock()

	<-c.rateLimiter.C
	c.lastRequest = time.Now()
}

// getFromCache retrieves meta from cache if not expired.
func (c *GoldfishClient) getFromCache(format string) *FormatMeta {
	c.cache.mu.RLock()
	defer c.cache.mu.RUnlock()

	entry, exists := c.cache.data[strings.ToLower(format)]
	if !exists {
		return nil
	}

	if time.Now().After(entry.ExpiresAt) {
		return nil
	}

	return entry.Meta
}

// setCache stores meta in cache.
func (c *GoldfishClient) setCache(format string, meta *FormatMeta) {
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	c.cache.data[strings.ToLower(format)] = &CacheEntry{
		Meta:      meta,
		ExpiresAt: time.Now().Add(c.cacheTTL),
	}
}

// ClearCache clears the meta cache.
func (c *GoldfishClient) ClearCache() {
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	c.cache.data = make(map[string]*CacheEntry)
}

// RefreshMeta forces a refresh of meta data for a format.
func (c *GoldfishClient) RefreshMeta(ctx context.Context, format string) (*FormatMeta, error) {
	// Clear cache for this format
	c.cache.mu.Lock()
	delete(c.cache.data, strings.ToLower(format))
	c.cache.mu.Unlock()

	return c.GetMeta(ctx, format)
}

// GetCacheStatus returns cache status for a format.
func (c *GoldfishClient) GetCacheStatus(format string) (cached bool, expiresAt time.Time) {
	c.cache.mu.RLock()
	defer c.cache.mu.RUnlock()

	entry, exists := c.cache.data[strings.ToLower(format)]
	if !exists {
		return false, time.Time{}
	}

	return true, entry.ExpiresAt
}

// Serialize serializes meta data to JSON.
func (m *FormatMeta) Serialize() ([]byte, error) {
	return json.Marshal(m)
}

// DeserializeFormatMeta deserializes meta data from JSON.
func DeserializeFormatMeta(data []byte) (*FormatMeta, error) {
	var meta FormatMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}
