package handlers_test

// collection_pagination_test.go — handler-level tests for #1325 pagination.
//
// Tests for:
//   - totalCount comes from CountCollection.UniqueCards (not filterCount / array length)
//   - filterCount comes from CountFilteredCollection (not len(cards))
//   - page / limit / sort_by / sort_desc / search request fields forwarded to repo
//   - totalPages is derived correctly from filterCount / limit

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// ---------------------------------------------------------------------------
// Stub extension for pagination methods
//
// We extend stubCollectionReader (defined in collection_test.go) via a new
// embedded stub that adds the pagination-specific surface.  The two files are
// in the same package so we define a new separate stub here to keep the two
// test suites decoupled.
// ---------------------------------------------------------------------------

type stubPaginatedReader struct {
	// list surface
	listRows   []repository.CollectionItem
	listFilter repository.CollectionFilter
	listPage   repository.CollectionPage
	listErr    error
	// filtered count surface
	filteredCount int
	filteredErr   error
	// unfiltered count surface (totals)
	counts    repository.CollectionCounts
	countsErr error
	// other surfaces needed by collectionReader
	rarity    []repository.RarityCount
	rarityErr error

	setCardCount    int
	setCardCountErr error
	setCardCountArg string

	sets      []repository.SetCompletionRow
	setsErr   error
	setRarity []repository.SetRarityRow
	setRarErr error

	values        []repository.CardValueRow
	unpriced      int
	valuesErr     error
	lastUpdated   int64
	lastUpdatedEr error
}

func (s *stubPaginatedReader) ListCollectionPage(_ context.Context, _ int64, f repository.CollectionFilter, p repository.CollectionPage) ([]repository.CollectionItem, error) {
	s.listFilter = f
	s.listPage = p
	return s.listRows, s.listErr
}

func (s *stubPaginatedReader) CountFilteredCollection(_ context.Context, _ int64, _ repository.CollectionFilter) (int, error) {
	return s.filteredCount, s.filteredErr
}

func (s *stubPaginatedReader) CountCollection(_ context.Context, _ int64) (repository.CollectionCounts, error) {
	return s.counts, s.countsErr
}

func (s *stubPaginatedReader) CountByRarity(_ context.Context, _ int64) ([]repository.RarityCount, error) {
	return s.rarity, s.rarityErr
}

func (s *stubPaginatedReader) SetCardCount(_ context.Context, setCode string) (int, error) {
	s.setCardCountArg = setCode
	return s.setCardCount, s.setCardCountErr
}

func (s *stubPaginatedReader) SetCompletion(_ context.Context, _ int64) ([]repository.SetCompletionRow, error) {
	return s.sets, s.setsErr
}

func (s *stubPaginatedReader) SetRarityBreakdown(_ context.Context, _ int64) ([]repository.SetRarityRow, error) {
	return s.setRarity, s.setRarErr
}

func (s *stubPaginatedReader) ValueRows(_ context.Context, _ int64) ([]repository.CardValueRow, int, error) {
	return s.values, s.unpriced, s.valuesErr
}

func (s *stubPaginatedReader) LastPriceUpdate(_ context.Context, _ int64) (int64, error) {
	return s.lastUpdated, s.lastUpdatedEr
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func authedPaginatedRequest(t *testing.T, body []byte, userID int64) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/collection", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestCollectionList_TotalCountFromUniqueCards verifies that totalCount in the
// response comes from CountCollection.UniqueCards, not from len(cards) or
// filterCount — per Ray's Rule 5.
func TestCollectionList_TotalCountFromUniqueCards(t *testing.T) {
	// Stub: 3 cards on this page but account has 9500 total unique cards.
	reader := &stubPaginatedReader{
		listRows: []repository.CollectionItem{
			{CardID: 1, ArenaID: 1, Quantity: 1, Name: "Card A"},
			{CardID: 2, ArenaID: 2, Quantity: 1, Name: "Card B"},
			{CardID: 3, ArenaID: 3, Quantity: 1, Name: "Card C"},
		},
		filteredCount: 3000, // filter narrows to 3000
		counts:        repository.CollectionCounts{UniqueCards: 9500, TotalCards: 19000},
	}
	h := handlers.NewCollectionHandler(reader, &collectionAccountLookup{accountID: 7, found: true})

	body, _ := json.Marshal(map[string]any{"page": 1, "limit": 50})
	req := authedPaginatedRequest(t, body, 168)
	rr := httptest.NewRecorder()
	h.List(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Cards       []map[string]any `json:"cards"`
		TotalCount  int              `json:"totalCount"`
		FilterCount int              `json:"filterCount"`
		TotalPages  int              `json:"totalPages"`
	}
	decodeCollectionEnvelope(t, rr.Body.Bytes(), &resp)

	// totalCount must be UniqueCards (9500), not len(cards) or filteredCount
	if resp.TotalCount != 9500 {
		t.Errorf("totalCount: got %d, want 9500 (UniqueCards)", resp.TotalCount)
	}
	// filterCount must be from CountFilteredCollection (3000)
	if resp.FilterCount != 3000 {
		t.Errorf("filterCount: got %d, want 3000 (CountFilteredCollection)", resp.FilterCount)
	}
	// cards on page: 3
	if len(resp.Cards) != 3 {
		t.Errorf("cards: got %d, want 3", len(resp.Cards))
	}
}

// TestCollectionList_TotalPagesFromFilterCount verifies totalPages is derived
// from filterCount / limit (ceiling division), not from total cards.
func TestCollectionList_TotalPagesFromFilterCount(t *testing.T) {
	reader := &stubPaginatedReader{
		listRows:      make([]repository.CollectionItem, 50),
		filteredCount: 205, // ceil(205/50) = 5
		counts:        repository.CollectionCounts{UniqueCards: 10000, TotalCards: 30000},
	}
	h := handlers.NewCollectionHandler(reader, &collectionAccountLookup{accountID: 7, found: true})

	body, _ := json.Marshal(map[string]any{"page": 1, "limit": 50})
	req := authedPaginatedRequest(t, body, 168)
	rr := httptest.NewRecorder()
	h.List(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		TotalPages int `json:"totalPages"`
	}
	decodeCollectionEnvelope(t, rr.Body.Bytes(), &resp)

	if resp.TotalPages != 5 {
		t.Errorf("totalPages: got %d, want 5 (ceil(205/50))", resp.TotalPages)
	}
}

// TestCollectionList_PaginationParamsForwarded verifies that page, limit,
// sort_by, sort_desc, and search from the request body are forwarded to the
// repository.
func TestCollectionList_PaginationParamsForwarded(t *testing.T) {
	reader := &stubPaginatedReader{
		filteredCount: 100,
		counts:        repository.CollectionCounts{UniqueCards: 500},
	}
	h := handlers.NewCollectionHandler(reader, &collectionAccountLookup{accountID: 7, found: true})

	body, _ := json.Marshal(map[string]any{
		"page":      3,
		"limit":     25,
		"sort_by":   "price",
		"sort_desc": true,
		"search":    "bolt",
		"set_code":  "MOM",
	})
	req := authedPaginatedRequest(t, body, 168)
	rr := httptest.NewRecorder()
	h.List(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}

	// Page params
	if reader.listPage.Page != 3 {
		t.Errorf("page: got %d, want 3", reader.listPage.Page)
	}
	if reader.listPage.Limit != 25 {
		t.Errorf("limit: got %d, want 25", reader.listPage.Limit)
	}

	// Filter params (sort + search + existing filters)
	if reader.listFilter.SortBy != "price" {
		t.Errorf("sort_by: got %q, want price", reader.listFilter.SortBy)
	}
	if !reader.listFilter.SortDesc {
		t.Errorf("sort_desc: expected true")
	}
	if reader.listFilter.Search != "bolt" {
		t.Errorf("search: got %q, want bolt", reader.listFilter.Search)
	}
	if reader.listFilter.SetCode != "MOM" {
		t.Errorf("set_code: got %q, want MOM", reader.listFilter.SetCode)
	}
}

// TestCollectionList_DefaultPageAndLimit verifies that an empty request body
// defaults to page=1 and a sensible limit (50).
func TestCollectionList_DefaultPageAndLimit(t *testing.T) {
	reader := &stubPaginatedReader{
		filteredCount: 10,
		counts:        repository.CollectionCounts{UniqueCards: 10},
	}
	h := handlers.NewCollectionHandler(reader, &collectionAccountLookup{accountID: 7, found: true})

	req := authedPaginatedRequest(t, []byte(`{}`), 168)
	rr := httptest.NewRecorder()
	h.List(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	if reader.listPage.Page != 1 {
		t.Errorf("default page: got %d, want 1", reader.listPage.Page)
	}
	if reader.listPage.Limit != 50 {
		t.Errorf("default limit: got %d, want 50", reader.listPage.Limit)
	}
}
