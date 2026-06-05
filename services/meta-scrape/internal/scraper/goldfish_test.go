package scraper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNewGoldfishClient(t *testing.T) {
	t.Run("with nil config uses defaults", func(t *testing.T) {
		client := NewGoldfishClient(nil)
		if client == nil {
			t.Fatal("expected non-nil client")
		}
		if client.baseURL != "https://www.mtggoldfish.com" {
			t.Errorf("expected default base URL, got %s", client.baseURL)
		}
		if client.cacheTTL != 4*time.Hour {
			t.Errorf("expected 4 hour cache TTL, got %v", client.cacheTTL)
		}
	})

	t.Run("with custom config", func(t *testing.T) {
		config := &GoldfishConfig{
			BaseURL:        "https://custom.url",
			CacheTTL:       2 * time.Hour,
			RequestTimeout: 10 * time.Second,
			RateLimitMs:    500,
		}
		client := NewGoldfishClient(config)
		if client.baseURL != "https://custom.url" {
			t.Errorf("expected custom base URL, got %s", client.baseURL)
		}
		if client.cacheTTL != 2*time.Hour {
			t.Errorf("expected 2 hour cache TTL, got %v", client.cacheTTL)
		}
	})
}

func TestDefaultGoldfishConfig(t *testing.T) {
	config := DefaultGoldfishConfig()

	if config.BaseURL != "https://www.mtggoldfish.com" {
		t.Errorf("unexpected BaseURL: %s", config.BaseURL)
	}
	if config.CacheTTL != 4*time.Hour {
		t.Errorf("unexpected CacheTTL: %v", config.CacheTTL)
	}
	if config.RequestTimeout != 30*time.Second {
		t.Errorf("unexpected RequestTimeout: %v", config.RequestTimeout)
	}
	if config.RateLimitMs != 1000 {
		t.Errorf("unexpected RateLimitMs: %d", config.RateLimitMs)
	}
}

func TestGoldfishClient_GetMeta(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return mock HTML with archetype data (current MTGGoldfish format)
		html := `
		<html>
		<body>
		<div class='archetype-tile' id='1'>
		<div class='archetype-tile-title'>
			<a href="/archetype/mono-red">Mono Red Aggro</a>
		</div>
		<div class='archetype-tile-statistic metagame-percentage'>
			<div class='archetype-tile-statistic-value'>15.5%</div>
		</div>
		</div>
		<div class='archetype-tile' id='2'>
		<div class='archetype-tile-title'>
			<a href="/archetype/azorius">Azorius Control</a>
		</div>
		<div class='archetype-tile-statistic metagame-percentage'>
			<div class='archetype-tile-statistic-value'>12.3%</div>
		</div>
		</div>
		<div class='archetype-tile' id='3'>
		<div class='archetype-tile-title'>
			<a href="/archetype/golgari">Golgari Midrange</a>
		</div>
		<div class='archetype-tile-statistic metagame-percentage'>
			<div class='archetype-tile-statistic-value'>8.7%</div>
		</div>
		</div>
		</body>
		</html>
		`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	config := &GoldfishConfig{
		BaseURL:        server.URL,
		CacheTTL:       1 * time.Hour,
		RequestTimeout: 5 * time.Second,
		RateLimitMs:    10, // Fast rate limit for tests
	}
	client := NewGoldfishClient(config)

	ctx := context.Background()
	meta, err := client.GetMeta(ctx, "standard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta == nil {
		t.Fatal("expected non-nil meta")
	}
	if meta.Format != "standard" {
		t.Errorf("expected format 'standard', got %s", meta.Format)
	}
	if meta.Source != "mtggoldfish" {
		t.Errorf("expected source 'mtggoldfish', got %s", meta.Source)
	}
	if len(meta.Decks) != 3 {
		t.Errorf("expected 3 decks, got %d", len(meta.Decks))
	}
}

func TestGoldfishClient_GetMeta_UnsupportedFormat(t *testing.T) {
	config := &GoldfishConfig{
		BaseURL:        "https://test.url",
		CacheTTL:       1 * time.Hour,
		RequestTimeout: 5 * time.Second,
		RateLimitMs:    10,
	}
	client := NewGoldfishClient(config)

	ctx := context.Background()
	_, err := client.GetMeta(ctx, "unsupported")
	if err == nil {
		t.Error("expected error for unsupported format")
	}
}

func TestGoldfishClient_GetTopDecks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `
		<html>
		<body>
		<div class='archetype-tile' id='1'>
		<div class='archetype-tile-title'><a href="/archetype/deck1">Deck One</a></div>
		<div class='archetype-tile-statistic metagame-percentage'>
			<div class='archetype-tile-statistic-value'>20.0%</div>
		</div>
		</div>
		<div class='archetype-tile' id='2'>
		<div class='archetype-tile-title'><a href="/archetype/deck2">Deck Two</a></div>
		<div class='archetype-tile-statistic metagame-percentage'>
			<div class='archetype-tile-statistic-value'>15.0%</div>
		</div>
		</div>
		<div class='archetype-tile' id='3'>
		<div class='archetype-tile-title'><a href="/archetype/deck3">Deck Three</a></div>
		<div class='archetype-tile-statistic metagame-percentage'>
			<div class='archetype-tile-statistic-value'>10.0%</div>
		</div>
		</div>
		</body>
		</html>
		`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	config := &GoldfishConfig{
		BaseURL:        server.URL,
		CacheTTL:       1 * time.Hour,
		RequestTimeout: 5 * time.Second,
		RateLimitMs:    10,
	}
	client := NewGoldfishClient(config)

	ctx := context.Background()
	decks, err := client.GetTopDecks(ctx, "standard", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(decks) != 2 {
		t.Errorf("expected 2 decks, got %d", len(decks))
	}
}

func TestGoldfishClient_GetDeckByArchetype(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `
		<html>
		<body>
		<div class='archetype-tile' id='1'>
		<div class='archetype-tile-title'><a href="/archetype/mono-red">Mono Red Aggro</a></div>
		<div class='archetype-tile-statistic metagame-percentage'>
			<div class='archetype-tile-statistic-value'>15.0%</div>
		</div>
		</div>
		<div class='archetype-tile' id='2'>
		<div class='archetype-tile-title'><a href="/archetype/azorius">Azorius Control</a></div>
		<div class='archetype-tile-statistic metagame-percentage'>
			<div class='archetype-tile-statistic-value'>10.0%</div>
		</div>
		</div>
		</body>
		</html>
		`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	config := &GoldfishConfig{
		BaseURL:        server.URL,
		CacheTTL:       1 * time.Hour,
		RequestTimeout: 5 * time.Second,
		RateLimitMs:    10,
	}
	client := NewGoldfishClient(config)

	ctx := context.Background()

	t.Run("found archetype", func(t *testing.T) {
		deck, err := client.GetDeckByArchetype(ctx, "standard", "mono red aggro")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if deck.Name != "Mono Red Aggro" {
			t.Errorf("expected 'Mono Red Aggro', got %s", deck.Name)
		}
	})

	t.Run("not found archetype", func(t *testing.T) {
		_, err := client.GetDeckByArchetype(ctx, "standard", "nonexistent")
		if err == nil {
			t.Error("expected error for nonexistent archetype")
		}
	})
}

func TestGoldfishClient_GetMetaShare(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `
		<html>
		<body>
		<div class='archetype-tile' id='1'>
		<div class='archetype-tile-title'><a href="/archetype/test">Test Deck</a></div>
		<div class='archetype-tile-statistic metagame-percentage'>
			<div class='archetype-tile-statistic-value'>25.5%</div>
		</div>
		</div>
		</body>
		</html>
		`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	config := &GoldfishConfig{
		BaseURL:        server.URL,
		CacheTTL:       1 * time.Hour,
		RequestTimeout: 5 * time.Second,
		RateLimitMs:    10,
	}
	client := NewGoldfishClient(config)

	ctx := context.Background()
	share, err := client.GetMetaShare(ctx, "standard", "test deck")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if share != 25.5 {
		t.Errorf("expected meta share 25.5, got %f", share)
	}
}

func TestGoldfishClient_Cache(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		html := `
		<html>
		<body>
		<div class='archetype-tile' id='1'>
		<div class='archetype-tile-title'><a href="/archetype/cached">Cached Deck</a></div>
		<div class='archetype-tile-statistic metagame-percentage'>
			<div class='archetype-tile-statistic-value'>10.0%</div>
		</div>
		</div>
		</body>
		</html>
		`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	config := &GoldfishConfig{
		BaseURL:            server.URL,
		CacheTTL:           1 * time.Hour,
		RequestTimeout:     5 * time.Second,
		RateLimitMs:        10,
		MaxDecklistFetches: -1, // disable decklist fetches so request count is predictable
	}
	client := NewGoldfishClient(config)

	ctx := context.Background()

	// First request should hit server
	_, err := client.GetMeta(ctx, "standard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestCount != 1 {
		t.Errorf("expected 1 request, got %d", requestCount)
	}

	// Second request should use cache
	_, err = client.GetMeta(ctx, "standard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestCount != 1 {
		t.Errorf("expected still 1 request (cached), got %d", requestCount)
	}
}

func TestGoldfishClient_ClearCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		html := `
		<html>
		<body>
		<div class='archetype-tile' id='1'>
		<div class='archetype-tile-title'><a href="/archetype/test">Test</a></div>
		<div class='archetype-tile-statistic metagame-percentage'>
			<div class='archetype-tile-statistic-value'>10.0%</div>
		</div>
		</div>
		</body>
		</html>
		`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	config := &GoldfishConfig{
		BaseURL:        server.URL,
		CacheTTL:       1 * time.Hour,
		RequestTimeout: 5 * time.Second,
		RateLimitMs:    10,
	}
	client := NewGoldfishClient(config)

	ctx := context.Background()

	// Populate cache
	_, _ = client.GetMeta(ctx, "standard")

	// Check cache status
	cached, _ := client.GetCacheStatus("standard")
	if !cached {
		t.Error("expected cache to be populated")
	}

	// Clear cache
	client.ClearCache()

	// Check cache status after clear
	cached, _ = client.GetCacheStatus("standard")
	if cached {
		t.Error("expected cache to be cleared")
	}
}

func TestGoldfishClient_RefreshMeta(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		html := `
		<html>
		<body>
		<div class='archetype-tile' id='1'>
		<div class='archetype-tile-title'><a href="/archetype/refresh">Refreshed Deck</a></div>
		<div class='archetype-tile-statistic metagame-percentage'>
			<div class='archetype-tile-statistic-value'>10.0%</div>
		</div>
		</div>
		</body>
		</html>
		`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	config := &GoldfishConfig{
		BaseURL:            server.URL,
		CacheTTL:           1 * time.Hour,
		RequestTimeout:     5 * time.Second,
		RateLimitMs:        10,
		MaxDecklistFetches: -1, // disable decklist fetches so request count is predictable
	}
	client := NewGoldfishClient(config)

	ctx := context.Background()

	// First request
	_, _ = client.GetMeta(ctx, "standard")

	// Refresh should bypass cache
	_, err := client.RefreshMeta(ctx, "standard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if requestCount != 2 {
		t.Errorf("expected 2 requests after refresh, got %d", requestCount)
	}
}

func TestGoldfishClient_ExtractColorsFromName(t *testing.T) {
	client := NewGoldfishClient(nil)

	tests := []struct {
		name     string
		expected []string
	}{
		{"Mono Red Aggro", []string{"R"}},
		{"Azorius Control", []string{"W", "U"}},
		{"Golgari Midrange", []string{"B", "G"}},
		{"Esper Control", []string{"W", "U", "B"}},
		{"Jund Sacrifice", []string{"B", "R", "G"}},
		{"Unknown Deck", []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			colors := client.extractColorsFromName(tt.name)
			if len(colors) != len(tt.expected) {
				t.Errorf("expected %d colors, got %d", len(tt.expected), len(colors))
				return
			}
			for i, c := range tt.expected {
				if colors[i] != c {
					t.Errorf("expected color %s at position %d, got %s", c, i, colors[i])
				}
			}
		})
	}
}

func TestGoldfishClient_NormalizeArchetypeName(t *testing.T) {
	client := NewGoldfishClient(nil)

	tests := []struct {
		input    string
		expected string
	}{
		{"Standard Mono Red", "mono red"},
		{"Historic Control", "control"},
		{"  Trimmed Name  ", "trimmed name"},
		{"Pioneer Aggro", "aggro"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := client.normalizeArchetypeName(tt.input)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestFormatMeta_Serialize(t *testing.T) {
	meta := &FormatMeta{
		Format: "standard",
		Decks: []*MetaDeck{
			{
				Name:      "Test Deck",
				MetaShare: 15.5,
			},
		},
		TotalDecks:  1,
		LastUpdated: time.Now(),
		Source:      "mtggoldfish",
	}

	data, err := meta.Serialize()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(data) == 0 {
		t.Error("expected non-empty serialized data")
	}

	// Deserialize and verify
	restored, err := DeserializeFormatMeta(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if restored.Format != meta.Format {
		t.Errorf("expected format %s, got %s", meta.Format, restored.Format)
	}
	if len(restored.Decks) != 1 {
		t.Errorf("expected 1 deck, got %d", len(restored.Decks))
	}
}

func TestDeserializeFormatMeta_InvalidJSON(t *testing.T) {
	_, err := DeserializeFormatMeta([]byte("invalid json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestGoldfishClient_ParseMetaPageTableFormat(t *testing.T) {
	client := NewGoldfishClient(nil)

	html := `
	<html>
	<body>
	<table>
	<tr>
		<td><a href="/archetype/mono-red#paper">Mono Red</a></td>
		<td>18.5%</td>
	</tr>
	<tr>
		<td><a href="/archetype/control#paper">Blue Control</a></td>
		<td>12.3%</td>
	</tr>
	</table>
	</body>
	</html>
	`

	meta := client.parseMetaPage(html, "standard")

	if len(meta.Decks) < 2 {
		t.Errorf("expected at least 2 decks from table format, got %d", len(meta.Decks))
	}
}

func TestGoldfishClient_GetCacheStatus_NotCached(t *testing.T) {
	client := NewGoldfishClient(nil)

	cached, expiresAt := client.GetCacheStatus("nonexistent")
	if cached {
		t.Error("expected not cached")
	}
	if !expiresAt.IsZero() {
		t.Error("expected zero time for nonexistent cache entry")
	}
}

func TestGoldfishClient_ParseMetaPageCurrentFormat(t *testing.T) {
	// Test with actual current MTGGoldfish HTML structure (as of 2024)
	client := NewGoldfishClient(nil)

	// This HTML matches the real MTGGoldfish structure with single quotes
	html := `
<div class='archetype-tile' id='28086'>
<div class='archetype-tile-image'>
<div aria-label='Image of Kaito' class='card-tile' role='img'>
</div>
</div>
<div class='archetype-tile-description-wrapper'>
<div class='archetype-tile-description'>
<div class='archetype-tile-title'>
<span class='deck-price-online'>
<a href="/archetype/standard-dimir-midrange-woe#online">Dimir Midrange</a>
</span>
</div>
</div>
<div class='archetype-tile-statistics'>
<div class='archetype-tile-statistics-left'>
<div class='archetype-tile-statistic metagame-percentage'>
<div class='archetype-tile-statistic-name'>META%</div>
<div class='archetype-tile-statistic-value'>
21.3%
<span class='archetype-tile-statistic-value-extra-data'>(385)</span>
</div>
</div>
</div>
</div>
</div>
</div>

<div class='archetype-tile' id='28249'>
<div class='archetype-tile-description-wrapper'>
<div class='archetype-tile-description'>
<div class='archetype-tile-title'>
<span class='deck-price-online'>
<a href="/archetype/standard-simic-aggro-woe#online">Simic Aggro</a>
</span>
</div>
</div>
<div class='archetype-tile-statistics'>
<div class='archetype-tile-statistics-left'>
<div class='archetype-tile-statistic metagame-percentage'>
<div class='archetype-tile-statistic-name'>META%</div>
<div class='archetype-tile-statistic-value'>
11.2%
</div>
</div>
</div>
</div>
</div>
</div>

<div class='archetype-tile' id='28080'>
<div class='archetype-tile-description-wrapper'>
<div class='archetype-tile-description'>
<div class='archetype-tile-title'>
<span class='deck-price-online'>
<a href="/archetype/standard-jeskai-control-woe#online">Jeskai Control</a>
</span>
</div>
</div>
<div class='archetype-tile-statistics'>
<div class='archetype-tile-statistics-left'>
<div class='archetype-tile-statistic metagame-percentage'>
<div class='archetype-tile-statistic-value'>
8.5%
</div>
</div>
</div>
</div>
</div>
</div>
`

	meta := client.parseMetaPage(html, "standard")

	if meta == nil {
		t.Fatal("Expected meta to not be nil")
	}

	if meta.Format != "standard" {
		t.Errorf("Expected format 'standard', got '%s'", meta.Format)
	}

	if len(meta.Decks) == 0 {
		t.Fatal("Expected at least one deck to be parsed from current HTML format")
	}

	t.Logf("Parsed %d decks from current format HTML", len(meta.Decks))
	for _, deck := range meta.Decks {
		t.Logf("  - %s: %.1f%%", deck.Name, deck.MetaShare)
	}

	// Verify we got the expected decks
	expectedDecks := map[string]float64{
		"Dimir Midrange": 21.3,
		"Simic Aggro":    11.2,
		"Jeskai Control": 8.5,
	}

	for _, deck := range meta.Decks {
		if expectedShare, ok := expectedDecks[deck.Name]; ok {
			if deck.MetaShare != expectedShare {
				t.Errorf("Expected %s meta share %.1f, got %.1f", deck.Name, expectedShare, deck.MetaShare)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// New parse-focused tests (#175 AC4: >=10 parse tests for the Goldfish scraper).
// All use parseMetaPage directly or httptest.Server -- zero live network.
// ---------------------------------------------------------------------------

// TestGoldfishParse_SingleArchetypeTile verifies a minimal single-tile page
// parses one deck with the correct name and share.
func TestGoldfishParse_SingleArchetypeTile(t *testing.T) {
	client := NewGoldfishClient(nil)
	html := `
	<div class='archetype-tile' id='1'>
	<div class='archetype-tile-title'><a href="/archetype/x">Boros Energy</a></div>
	<div class='archetype-tile-statistic metagame-percentage'>
	<div class='archetype-tile-statistic-value'>9.4%</div>
	</div>
	</div>`
	meta := client.parseMetaPage(html, "standard")
	if len(meta.Decks) != 1 {
		t.Fatalf("expected 1 deck, got %d", len(meta.Decks))
	}
	if meta.Decks[0].Name != "Boros Energy" {
		t.Errorf("expected name 'Boros Energy', got %q", meta.Decks[0].Name)
	}
	if meta.Decks[0].MetaShare != 9.4 {
		t.Errorf("expected share 9.4, got %v", meta.Decks[0].MetaShare)
	}
}

// TestGoldfishParse_TierAssignment confirms the >5/>2/>0.5 thresholds map to
// tiers 1/2/3/4 respectively.
func TestGoldfishParse_TierAssignment(t *testing.T) {
	client := NewGoldfishClient(nil)
	html := `
	<div class='archetype-tile' id='1'><div class='archetype-tile-title'><a href="/a">T1 Deck</a></div><div class='archetype-tile-statistic-value'>8.0%</div></div>
	<div class='archetype-tile' id='2'><div class='archetype-tile-title'><a href="/a">T2 Deck</a></div><div class='archetype-tile-statistic-value'>3.0%</div></div>
	<div class='archetype-tile' id='3'><div class='archetype-tile-title'><a href="/a">T3 Deck</a></div><div class='archetype-tile-statistic-value'>1.0%</div></div>
	<div class='archetype-tile' id='4'><div class='archetype-tile-title'><a href="/a">T4 Deck</a></div><div class='archetype-tile-statistic-value'>0.1%</div></div>`
	meta := client.parseMetaPage(html, "standard")
	byName := map[string]int{}
	for _, d := range meta.Decks {
		byName[d.Name] = d.Tier
	}
	cases := map[string]int{"T1 Deck": 1, "T2 Deck": 2, "T3 Deck": 3, "T4 Deck": 4}
	for name, wantTier := range cases {
		if byName[name] != wantTier {
			t.Errorf("%s: expected tier %d, got %d", name, wantTier, byName[name])
		}
	}
}

// TestGoldfishParse_DoubleQuotedClasses verifies the regex handles double-quoted
// class attributes (the ['\"] alternation).
func TestGoldfishParse_DoubleQuotedClasses(t *testing.T) {
	client := NewGoldfishClient(nil)
	html := `
	<div class="archetype-tile" id="1">
	<div class="archetype-tile-title"><a href="/archetype/y">Gruul Aggro</a></div>
	<div class="archetype-tile-statistic metagame-percentage">
	<div class="archetype-tile-statistic-value">7.2%</div>
	</div>
	</div>`
	meta := client.parseMetaPage(html, "standard")
	if len(meta.Decks) != 1 || meta.Decks[0].Name != "Gruul Aggro" {
		t.Fatalf("expected Gruul Aggro from double-quoted HTML, got %+v", meta.Decks)
	}
}

// TestGoldfishParse_IntegerPercentage verifies a share with no decimal point
// (e.g. "5%") parses correctly.
func TestGoldfishParse_IntegerPercentage(t *testing.T) {
	client := NewGoldfishClient(nil)
	html := `
	<div class='archetype-tile' id='1'><div class='archetype-tile-title'><a href="/a">Mono White</a></div><div class='archetype-tile-statistic-value'>5%</div></div>`
	meta := client.parseMetaPage(html, "standard")
	if len(meta.Decks) != 1 {
		t.Fatalf("expected 1 deck, got %d", len(meta.Decks))
	}
	if meta.Decks[0].MetaShare != 5.0 {
		t.Errorf("expected share 5.0, got %v", meta.Decks[0].MetaShare)
	}
}

// TestGoldfishParse_EmptyPage returns zero decks for HTML with no archetypes.
func TestGoldfishParse_EmptyPage(t *testing.T) {
	client := NewGoldfishClient(nil)
	meta := client.parseMetaPage(`<html><body><p>No data</p></body></html>`, "standard")
	if len(meta.Decks) != 0 {
		t.Errorf("expected 0 decks for empty page, got %d", len(meta.Decks))
	}
	if meta.TotalDecks != 0 {
		t.Errorf("expected TotalDecks 0, got %d", meta.TotalDecks)
	}
}

// TestGoldfishParse_TableFallbackShareAndColors verifies the table fallback
// pattern extracts the name and that colors are derived from the name.
func TestGoldfishParse_TableFallbackShareAndColors(t *testing.T) {
	client := NewGoldfishClient(nil)
	html := `
	<table>
	<tr><td><a href="/archetype/izzet-phoenix#paper">Izzet Phoenix</a></td><td>14.0%</td></tr>
	</table>`
	meta := client.parseMetaPage(html, "modern")
	if len(meta.Decks) != 1 {
		t.Fatalf("expected 1 deck from table fallback, got %d", len(meta.Decks))
	}
	d := meta.Decks[0]
	if d.Name != "Izzet Phoenix" {
		t.Errorf("expected 'Izzet Phoenix', got %q", d.Name)
	}
	// Izzet -> UR
	if len(d.Colors) != 2 || d.Colors[0] != "U" || d.Colors[1] != "R" {
		t.Errorf("expected colors [U R] for Izzet, got %v", d.Colors)
	}
}

// TestGoldfishParse_FieldsPopulated verifies Format and ArchetypeName are set
// on each parsed deck.
func TestGoldfishParse_FieldsPopulated(t *testing.T) {
	client := NewGoldfishClient(nil)
	html := `
	<div class='archetype-tile' id='1'><div class='archetype-tile-title'><a href="/a">Standard Dimir Control</a></div><div class='archetype-tile-statistic-value'>6.0%</div></div>`
	meta := client.parseMetaPage(html, "standard")
	if len(meta.Decks) != 1 {
		t.Fatalf("expected 1 deck, got %d", len(meta.Decks))
	}
	d := meta.Decks[0]
	if d.Format != "standard" {
		t.Errorf("expected format 'standard', got %q", d.Format)
	}
	// normalizeArchetypeName strips the "standard " prefix and lowercases.
	if d.ArchetypeName != "dimir control" {
		t.Errorf("expected normalized archetype 'dimir control', got %q", d.ArchetypeName)
	}
}

// TestGoldfishParse_DeckCap confirms the parser caps at 50 decks.
func TestGoldfishParse_DeckCap(t *testing.T) {
	client := NewGoldfishClient(nil)
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString(`<div class='archetype-tile' id='x'><div class='archetype-tile-title'><a href="/a">Deck</a></div><div class='archetype-tile-statistic-value'>1.0%</div></div>`)
	}
	meta := client.parseMetaPage(sb.String(), "standard")
	if len(meta.Decks) != 50 {
		t.Errorf("expected deck list capped at 50, got %d", len(meta.Decks))
	}
}

// TestGoldfishParse_WhitespaceAroundValue verifies leading/trailing whitespace
// around the percentage is tolerated.
func TestGoldfishParse_WhitespaceAroundValue(t *testing.T) {
	client := NewGoldfishClient(nil)
	html := `
	<div class='archetype-tile' id='1'><div class='archetype-tile-title'><a href="/a">Selesnya Tokens</a></div><div class='archetype-tile-statistic-value'>
	   4.5%
	</div></div>`
	meta := client.parseMetaPage(html, "standard")
	if len(meta.Decks) != 1 {
		t.Fatalf("expected 1 deck, got %d", len(meta.Decks))
	}
	if meta.Decks[0].MetaShare != 4.5 {
		t.Errorf("expected share 4.5, got %v", meta.Decks[0].MetaShare)
	}
}

// TestGoldfishParse_TotalDecksMatchesLength asserts TotalDecks tracks len(Decks).
func TestGoldfishParse_TotalDecksMatchesLength(t *testing.T) {
	client := NewGoldfishClient(nil)
	html := `
	<div class='archetype-tile' id='1'><div class='archetype-tile-title'><a href="/a">Deck A</a></div><div class='archetype-tile-statistic-value'>6.0%</div></div>
	<div class='archetype-tile' id='2'><div class='archetype-tile-title'><a href="/a">Deck B</a></div><div class='archetype-tile-statistic-value'>3.0%</div></div>`
	meta := client.parseMetaPage(html, "standard")
	if meta.TotalDecks != len(meta.Decks) {
		t.Errorf("TotalDecks (%d) != len(Decks) (%d)", meta.TotalDecks, len(meta.Decks))
	}
}

// TestGoldfishClient_GetMeta_Non200 verifies a non-200 response is an error and
// no source data is cached.
func TestGoldfishClient_GetMeta_Non200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewGoldfishClient(&GoldfishConfig{
		BaseURL:        server.URL,
		CacheTTL:       time.Hour,
		RequestTimeout: 5 * time.Second,
		RateLimitMs:    10,
	})
	_, err := client.GetMeta(context.Background(), "standard")
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
	if cached, _ := client.GetCacheStatus("standard"); cached {
		t.Error("failed fetch should not populate cache")
	}
}

// TestGoldfishClient_ResponseSizeCap verifies the HP-1 io.LimitReader cap: a
// response body larger than the 10 MB cap is truncated before parsing, so the
// parser never sees the oversized tail. We assert the fetch still succeeds and
// the parser only sees content within the cap.
func TestGoldfishClient_ResponseSizeCap(t *testing.T) {
	// One valid tile, then padding past the 10 MB cap whose tile must NOT be seen.
	const valid = `<div class='archetype-tile' id='1'><div class='archetype-tile-title'><a href="/a">Within Cap</a></div><div class='archetype-tile-statistic-value'>9.0%</div></div>`
	const beyond = `<div class='archetype-tile' id='2'><div class='archetype-tile-title'><a href="/a">Beyond Cap</a></div><div class='archetype-tile-statistic-value'>9.0%</div></div>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(valid))
		_, _ = w.Write([]byte(strings.Repeat(" ", 11<<20))) // 11 MB of filler > 10 MB cap
		_, _ = w.Write([]byte(beyond))
	}))
	defer server.Close()

	client := NewGoldfishClient(&GoldfishConfig{
		BaseURL:        server.URL,
		CacheTTL:       time.Hour,
		RequestTimeout: 30 * time.Second,
		RateLimitMs:    10,
	})
	meta, err := client.GetMeta(context.Background(), "standard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, d := range meta.Decks {
		if d.Name == "Beyond Cap" {
			t.Fatal("parser saw content beyond the 10 MB size cap; io.LimitReader not applied")
		}
	}
	found := false
	for _, d := range meta.Decks {
		if d.Name == "Within Cap" {
			found = true
		}
	}
	if !found {
		t.Error("expected the within-cap deck to be parsed")
	}
}

// ---------------------------------------------------------------------------
// Decklist page parsing tests (#384).
// All use parseDecklistPage directly or httptest.Server — zero live network.
// ---------------------------------------------------------------------------

// TestParseDecklistPage_Fixture verifies the goldfish_decklist_standard.html
// reference snapshot parses correctly: card counts, card names, and sideboard
// separation.
func TestParseDecklistPage_Fixture(t *testing.T) {
	client := NewGoldfishClient(nil)
	_ = client // parseDecklistPage is a package-level func; client only needed for type check

	html := readFixture(t, "goldfish_decklist_standard.html")
	main, side, err := parseDecklistPage(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(main) != 6 {
		t.Errorf("expected 6 mainboard cards, got %d", len(main))
	}
	if len(side) != 3 {
		t.Errorf("expected 3 sideboard cards, got %d", len(side))
	}

	// Verify HTML entity decoding: Stormchaser's Talent must have the apostrophe.
	foundTalent := false
	for _, c := range main {
		if c.Name == "Stormchaser's Talent" {
			foundTalent = true
			if c.Quantity != 4 {
				t.Errorf("Stormchaser's Talent: expected 4 copies, got %d", c.Quantity)
			}
			break
		}
	}
	if !foundTalent {
		t.Error("expected Stormchaser's Talent with decoded apostrophe in mainboard")
	}
}

// TestParseDecklistPage_NoInput verifies that a page with no deck_input hidden
// field returns nil slices without error.
func TestParseDecklistPage_NoInput(t *testing.T) {
	main, side, err := parseDecklistPage(`<html><body><p>no deck here</p></body></html>`)
	if err != nil {
		t.Fatalf("unexpected error for page with no input: %v", err)
	}
	if main != nil || side != nil {
		t.Errorf("expected nil slices for page with no deck input, got main=%v side=%v", main, side)
	}
}

// TestParseDecklistPage_MainboardOnly verifies a decklist with no sideboard
// section returns an empty sideboard and the full mainboard.
func TestParseDecklistPage_MainboardOnly(t *testing.T) {
	html := `<input name="deck_input[deck]" value="4 Lightning Bolt
4 Goblin Guide
">`
	main, side, err := parseDecklistPage(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(main) != 2 {
		t.Errorf("expected 2 mainboard cards, got %d", len(main))
	}
	if len(side) != 0 {
		t.Errorf("expected 0 sideboard cards, got %d", len(side))
	}
}

// TestParseDecklistPage_HTMLEntityDecoding verifies common HTML entities are
// decoded in card names before parsing.
func TestParseDecklistPage_HTMLEntityDecoding(t *testing.T) {
	// &#39; = apostrophe, &amp; = &
	html := `<input name="deck_input[deck]" value="4 Stormchaser&#39;s Talent
2 Fire &amp; Ice
sideboard
1 Witches&#39; Cauldron
">`
	main, side, err := parseDecklistPage(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(main) != 2 {
		t.Fatalf("expected 2 mainboard cards, got %d", len(main))
	}
	if main[0].Name != "Stormchaser's Talent" {
		t.Errorf("expected Stormchaser's Talent, got %q", main[0].Name)
	}
	if main[1].Name != "Fire & Ice" {
		t.Errorf("expected Fire & Ice, got %q", main[1].Name)
	}
	if len(side) != 1 || side[0].Name != "Witches' Cauldron" {
		t.Errorf("expected Witches' Cauldron in sideboard, got %v", side)
	}
}

// TestParseDecklistPage_MalformedLinesSkipped verifies that lines without a
// leading integer quantity are silently skipped rather than causing an error.
func TestParseDecklistPage_MalformedLinesSkipped(t *testing.T) {
	html := `<input name="deck_input[deck]" value="4 Good Card
bad line without quantity
0 Zero Copies
3 Another Good Card
">`
	main, side, err := parseDecklistPage(html)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the two valid positive-quantity lines should appear.
	if len(main) != 2 {
		t.Errorf("expected 2 valid cards, got %d: %v", len(main), main)
	}
	if len(side) != 0 {
		t.Errorf("expected 0 sideboard cards, got %d", len(side))
	}
}

// TestFetchDecklistPage_Non200IsNonFatal verifies that a non-200 response from
// the decklist page returns an error (the caller will skip the deck rather than
// aborting the format scrape).
func TestFetchDecklistPage_Non200IsNonFatal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewGoldfishClient(&GoldfishConfig{
		BaseURL:            server.URL,
		CacheTTL:           time.Hour,
		RequestTimeout:     5 * time.Second,
		RateLimitMs:        10,
		MaxDecklistFetches: -1, // disable auto-fetch; call fetchDecklistPage directly
	})

	_, _, err := client.fetchDecklistPage(context.Background(), "/archetype/test")
	if err == nil {
		t.Fatal("expected error on 404 response")
	}
}

// TestGoldfishClient_GetMeta_PopulatesCardLists verifies that GetMeta
// populates MainboardCards and SideboardCards on each MetaDeck when the
// decklist server returns valid HTML with a deck_input hidden field.
func TestGoldfishClient_GetMeta_PopulatesCardLists(t *testing.T) {
	const decklistHTML = `
<input name="deck_input[deck]" value="4 Monastery Swiftspear
3 Lightning Bolt
sideboard
2 Spell Pierce
">`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/archetype/") {
			// Decklist page request.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(decklistHTML))
			return
		}
		// Metagame page request.
		html := `
		<html><body>
		<div class='archetype-tile' id='1'>
		<div class='archetype-tile-title'>
		<a href="/archetype/mono-red-aggro#online">Mono Red Aggro</a>
		</div>
		<div class='archetype-tile-statistic metagame-percentage'>
		<div class='archetype-tile-statistic-value'>18.0%</div>
		</div>
		</div>
		</body></html>`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	client := NewGoldfishClient(&GoldfishConfig{
		BaseURL:            server.URL,
		CacheTTL:           time.Hour,
		RequestTimeout:     5 * time.Second,
		RateLimitMs:        10,
		MaxDecklistFetches: 5,
	})

	meta, err := client.GetMeta(context.Background(), "standard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(meta.Decks) != 1 {
		t.Fatalf("expected 1 deck, got %d", len(meta.Decks))
	}
	deck := meta.Decks[0]
	if len(deck.MainboardCards) != 2 {
		t.Errorf("expected 2 mainboard cards, got %d", len(deck.MainboardCards))
	}
	if len(deck.SideboardCards) != 1 {
		t.Errorf("expected 1 sideboard card, got %d", len(deck.SideboardCards))
	}
	if len(deck.MainboardCards) > 0 && deck.MainboardCards[0].Name != "Monastery Swiftspear" {
		t.Errorf("expected Monastery Swiftspear, got %q", deck.MainboardCards[0].Name)
	}
}

// TestGoldfishClient_GetMeta_DecklistFetchErrorIsNonFatal verifies that a
// decklist fetch failure (404 from the archetype sub-page) does not abort the
// overall metagame scrape — the archetype is returned with an empty card list.
func TestGoldfishClient_GetMeta_DecklistFetchErrorIsNonFatal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/archetype/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		html := `
		<html><body>
		<div class='archetype-tile' id='1'>
		<div class='archetype-tile-title'>
		<a href="/archetype/some-deck#online">Some Deck</a>
		</div>
		<div class='archetype-tile-statistic metagame-percentage'>
		<div class='archetype-tile-statistic-value'>10.0%</div>
		</div>
		</div>
		</body></html>`
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(html))
	}))
	defer server.Close()

	client := NewGoldfishClient(&GoldfishConfig{
		BaseURL:            server.URL,
		CacheTTL:           time.Hour,
		RequestTimeout:     5 * time.Second,
		RateLimitMs:        10,
		MaxDecklistFetches: 5,
	})

	meta, err := client.GetMeta(context.Background(), "standard")
	if err != nil {
		t.Fatalf("metagame scrape must not fail when decklist page is 404: %v", err)
	}
	if len(meta.Decks) != 1 {
		t.Fatalf("expected 1 deck, got %d", len(meta.Decks))
	}
	// Card lists are empty, but the archetype row is present.
	if len(meta.Decks[0].MainboardCards) != 0 {
		t.Errorf("expected empty mainboard on decklist-404, got %d cards", len(meta.Decks[0].MainboardCards))
	}
}

// TestGoldfishClient_GetMeta_DecklistCapRespected verifies that at most
// MaxDecklistFetches decklist pages are fetched when the metagame page has more
// archetypes than the cap.
func TestGoldfishClient_GetMeta_DecklistCapRespected(t *testing.T) {
	const decklistHTML = `<input name="deck_input[deck]" value="4 Test Card
sideboard
1 Sideboard Card
">`

	decklistRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/archetype/") {
			decklistRequests++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(decklistHTML))
			return
		}
		// Return 5 archetype tiles.
		var sb strings.Builder
		sb.WriteString("<html><body>")
		for i := 0; i < 5; i++ {
			sb.WriteString(`<div class='archetype-tile' id='x'>`)
			sb.WriteString(`<div class='archetype-tile-title'>`)
			sb.WriteString(`<a href="/archetype/deck-` + strconv.Itoa(i) + `#online">Deck ` + strconv.Itoa(i) + `</a>`)
			sb.WriteString(`</div>`)
			sb.WriteString(`<div class='archetype-tile-statistic metagame-percentage'>`)
			sb.WriteString(`<div class='archetype-tile-statistic-value'>5.0%</div>`)
			sb.WriteString(`</div></div>`)
		}
		sb.WriteString("</body></html>")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sb.String()))
	}))
	defer server.Close()

	const cap = 3
	client := NewGoldfishClient(&GoldfishConfig{
		BaseURL:            server.URL,
		CacheTTL:           time.Hour,
		RequestTimeout:     5 * time.Second,
		RateLimitMs:        10,
		MaxDecklistFetches: cap,
	})

	_, err := client.GetMeta(context.Background(), "standard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decklistRequests != cap {
		t.Errorf("expected exactly %d decklist fetches (cap), got %d", cap, decklistRequests)
	}
}

// TestParseDeckListLine_Valid verifies correctly formatted lines are parsed.
func TestParseDeckListLine_Valid(t *testing.T) {
	cases := []struct {
		line     string
		name     string
		quantity int
	}{
		{"4 Lightning Bolt", "Lightning Bolt", 4},
		{"1 Roaring Furnace // Steaming Sauna", "Roaring Furnace // Steaming Sauna", 1},
		{"20 Island", "Island", 20},
	}
	for _, c := range cases {
		t.Run(c.line, func(t *testing.T) {
			card, err := parseDeckListLine(c.line)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if card.Name != c.name {
				t.Errorf("name: got %q want %q", card.Name, c.name)
			}
			if card.Quantity != c.quantity {
				t.Errorf("quantity: got %d want %d", card.Quantity, c.quantity)
			}
		})
	}
}

// TestParseDeckListLine_Invalid verifies malformed lines return an error.
func TestParseDeckListLine_Invalid(t *testing.T) {
	cases := []string{
		"",
		"nospaceline",
		"0 Zero Copies",
		"-1 Negative",
		"abc Card Name",
	}
	for _, line := range cases {
		t.Run(line, func(t *testing.T) {
			_, err := parseDeckListLine(line)
			if err == nil {
				t.Errorf("expected error for malformed line %q", line)
			}
		})
	}
}

// TestDecodeHTMLEntities verifies entity replacement.
func TestDecodeHTMLEntities(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Stormchaser&#39;s Talent", "Stormchaser's Talent"},
		{"Fire &amp; Ice", "Fire & Ice"},
		{"&quot;Shriekmaw&quot;", `"Shriekmaw"`},
		{"plain name", "plain name"},
	}
	for _, c := range cases {
		got := decodeHTMLEntities(c.in)
		if got != c.want {
			t.Errorf("decodeHTMLEntities(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
