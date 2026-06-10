package handlers_test

import (
	"bytes"
	"context"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// ─── stubs ──────────────────────────────────────────────────────────────────

// stubCardResolver implements handlers.CardResolver.
type stubCardResolver struct {
	// mapping: (setCode+"::"+name) → arena_id; 0 means not found.
	lookup map[string]int
	err    error
}

func (s *stubCardResolver) ResolveArenaID(_ context.Context, setCode, name string) (int, bool, error) {
	if s.err != nil {
		return 0, false, s.err
	}
	key := setCode + "::" + name
	id, ok := s.lookup[key]
	if !ok || id == 0 {
		return 0, false, nil
	}
	return id, true, nil
}

// stubInventoryWriter implements handlers.InventoryWriter.
type stubInventoryWriter struct {
	calls []repository.CardInventoryUpsert
	err   error
}

func (s *stubInventoryWriter) UpsertDelta(_ context.Context, u repository.CardInventoryUpsert) error {
	s.calls = append(s.calls, u)
	return s.err
}

// ─── helpers ────────────────────────────────────────────────────────────────

// multipartFile builds a multipart/form-data request with a single file field
// named "file" carrying the given body content.
func multipartFile(t *testing.T, content string) *http.Request {
	t.Helper()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "collection.csv")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatalf("write form file: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/collection/import", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// withImportAuth injects a user ID into the request context.
func withImportAuth(req *http.Request, userID int64) *http.Request {
	return req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
}

const goldenFixture = `// MTGA in-game collection export format
// Format: <quantity> <CardName> (<SetCode>) <collectorNumber>
4 Lightning Bolt (ONS) 197
2 Thoughtseize (THS) 107
1 Teferi, Hero of Dominaria (DAR) 207`

// ─── parse unit tests ────────────────────────────────────────────────────────

func TestParseArenaLine_ValidLines(t *testing.T) {
	cases := []struct {
		line     string
		wantQty  int
		wantName string
		wantSet  string
	}{
		{"4 Lightning Bolt (ONS) 197", 4, "Lightning Bolt", "ONS"},
		{"2 Thoughtseize (THS) 107", 2, "Thoughtseize", "THS"},
		{"1 Teferi, Hero of Dominaria (DAR) 207", 1, "Teferi, Hero of Dominaria", "DAR"},
		{"10 Snapcaster Mage (ISD) 78", 10, "Snapcaster Mage", "ISD"},
	}

	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			qty, name, setCode, ok := handlers.ParseArenaLine(tc.line)
			if !ok {
				t.Fatalf("ParseArenaLine(%q): ok=false, expected ok=true", tc.line)
			}
			if qty != tc.wantQty {
				t.Errorf("qty: want %d, got %d", tc.wantQty, qty)
			}
			if name != tc.wantName {
				t.Errorf("name: want %q, got %q", tc.wantName, name)
			}
			if setCode != tc.wantSet {
				t.Errorf("setCode: want %q, got %q", tc.wantSet, setCode)
			}
		})
	}
}

func TestParseArenaLine_InvalidLines(t *testing.T) {
	cases := []string{
		"",
		"// comment line",
		"not a valid line",
		"Card Name Without Quantity",
		"0 Zero Quantity (SET) 1",
		"-1 Negative Qty (SET) 1",
		"abc Name (SET) 1",
	}

	for _, line := range cases {
		t.Run(line, func(t *testing.T) {
			_, _, _, ok := handlers.ParseArenaLine(line)
			if ok {
				t.Errorf("ParseArenaLine(%q): expected ok=false, got ok=true", line)
			}
		})
	}
}

// ─── handler unit tests (stub resolver + writer) ─────────────────────────────

func newImportHandler(resolver handlers.CardResolver, writer handlers.InventoryWriter, accounts handlers.AccountLookup) *handlers.CollectionImportHandler {
	return handlers.NewCollectionImportHandler(resolver, writer, accounts)
}

func TestCollectionImportHandler_Unauthorized(t *testing.T) {
	h := newImportHandler(&stubCardResolver{}, &stubInventoryWriter{}, &collectionAccountLookup{accountID: 1, found: true})
	req := multipartFile(t, goldenFixture) // no auth context
	w := httptest.NewRecorder()
	h.Import(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestCollectionImportHandler_AccountNotFound(t *testing.T) {
	h := newImportHandler(
		&stubCardResolver{},
		&stubInventoryWriter{},
		&collectionAccountLookup{accountID: 0, found: false},
	)
	req := withImportAuth(multipartFile(t, goldenFixture), 1)
	w := httptest.NewRecorder()
	h.Import(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 when account not found, got %d", w.Code)
	}
}

func TestCollectionImportHandler_EmptyFile(t *testing.T) {
	h := newImportHandler(
		&stubCardResolver{},
		&stubInventoryWriter{},
		&collectionAccountLookup{accountID: 1, found: true},
	)
	req := withImportAuth(multipartFile(t, ""), 1)
	w := httptest.NewRecorder()
	h.Import(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty file, got %d", w.Code)
	}
}

func TestCollectionImportHandler_NoParsableRows(t *testing.T) {
	h := newImportHandler(
		&stubCardResolver{},
		&stubInventoryWriter{},
		&collectionAccountLookup{accountID: 1, found: true},
	)
	content := "// just comments\n// nothing parseable\n"
	req := withImportAuth(multipartFile(t, content), 1)
	w := httptest.NewRecorder()
	h.Import(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for zero parseable rows, got %d", w.Code)
	}
}

func TestCollectionImportHandler_MissingFileField(t *testing.T) {
	h := newImportHandler(
		&stubCardResolver{},
		&stubInventoryWriter{},
		&collectionAccountLookup{accountID: 1, found: true},
	)
	// Send a plain JSON body instead of multipart.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/collection/import",
		strings.NewReader(`{"not": "multipart"}`))
	req.Header.Set("Content-Type", "application/json")
	req = withImportAuth(req, 1)
	w := httptest.NewRecorder()
	h.Import(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-multipart body, got %d", w.Code)
	}
}

func TestCollectionImportHandler_AllAccepted(t *testing.T) {
	resolver := &stubCardResolver{
		lookup: map[string]int{
			"ONS::Lightning Bolt":            700001,
			"THS::Thoughtseize":              700002,
			"DAR::Teferi, Hero of Dominaria": 700003,
		},
	}
	writer := &stubInventoryWriter{}
	h := newImportHandler(resolver, writer, &collectionAccountLookup{accountID: 42, found: true})

	req := withImportAuth(multipartFile(t, goldenFixture), 99)
	w := httptest.NewRecorder()
	h.Import(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", w.Code, w.Body.String())
	}

	// 3 parseable rows, all resolved.
	if len(writer.calls) != 3 {
		t.Errorf("expected 3 UpsertDelta calls, got %d", len(writer.calls))
	}

	// All upserts scoped to accountID=42.
	for _, c := range writer.calls {
		if c.AccountID != 42 {
			t.Errorf("expected AccountID=42, got %d", c.AccountID)
		}
		if c.SnapshotHash == "" {
			t.Errorf("expected non-empty snapshot_hash")
		}
	}

	// Verify response shape contains accepted+rejected.
	body := w.Body.String()
	if !strings.Contains(body, `"accepted"`) {
		t.Errorf("response missing 'accepted' field: %s", body)
	}
	if !strings.Contains(body, `"rejected"`) {
		t.Errorf("response missing 'rejected' field: %s", body)
	}
}

func TestCollectionImportHandler_PartialReject_UnresolvedName(t *testing.T) {
	// Only one card resolves; the other two cannot be found in set_cards.
	resolver := &stubCardResolver{
		lookup: map[string]int{
			"ONS::Lightning Bolt": 700001,
			// Thoughtseize and Teferi are not in the stub.
		},
	}
	writer := &stubInventoryWriter{}
	h := newImportHandler(resolver, writer, &collectionAccountLookup{accountID: 42, found: true})

	req := withImportAuth(multipartFile(t, goldenFixture), 99)
	w := httptest.NewRecorder()
	h.Import(w, req)

	// Endpoint returns 200 even when some rows are rejected (partial success).
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", w.Code, w.Body.String())
	}

	// Only one UpsertDelta call (only Lightning Bolt resolved).
	if len(writer.calls) != 1 {
		t.Errorf("expected 1 UpsertDelta call, got %d", len(writer.calls))
	}
}

func TestCollectionImportHandler_AllRejected_ZeroUpserts(t *testing.T) {
	// No cards resolve.
	resolver := &stubCardResolver{lookup: map[string]int{}}
	writer := &stubInventoryWriter{}
	h := newImportHandler(resolver, writer, &collectionAccountLookup{accountID: 42, found: true})

	req := withImportAuth(multipartFile(t, goldenFixture), 99)
	w := httptest.NewRecorder()
	h.Import(w, req)

	// Still a 200 (zero-upsert is not an error — the file was parseable).
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 even when all rows rejected, got %d", w.Code)
	}
	if len(writer.calls) != 0 {
		t.Errorf("expected 0 UpsertDelta calls, got %d", len(writer.calls))
	}
}

func TestCollectionImportHandler_SnapshotHashDeterministic(t *testing.T) {
	// Import the same file twice — the snapshot_hash on every row must be
	// identical across the two runs (deterministic over sorted arena_id+count).
	resolver := &stubCardResolver{
		lookup: map[string]int{
			"ONS::Lightning Bolt":            700001,
			"THS::Thoughtseize":              700002,
			"DAR::Teferi, Hero of Dominaria": 700003,
		},
	}

	runImport := func() []repository.CardInventoryUpsert {
		writer := &stubInventoryWriter{}
		h := newImportHandler(resolver, writer, &collectionAccountLookup{accountID: 5, found: true})
		req := withImportAuth(multipartFile(t, goldenFixture), 5)
		w := httptest.NewRecorder()
		h.Import(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
		return writer.calls
	}

	first := runImport()
	second := runImport()

	if len(first) != len(second) {
		t.Fatalf("call count mismatch: first=%d second=%d", len(first), len(second))
	}
	for i := range first {
		if first[i].SnapshotHash != second[i].SnapshotHash {
			t.Errorf("row %d: snapshot_hash not deterministic: %q vs %q",
				i, first[i].SnapshotHash, second[i].SnapshotHash)
		}
	}
}

func TestCollectionImportHandler_ResolverError_Returns500(t *testing.T) {
	resolver := &stubCardResolver{err: errors.New("db unreachable")}
	writer := &stubInventoryWriter{}
	h := newImportHandler(resolver, writer, &collectionAccountLookup{accountID: 1, found: true})

	req := withImportAuth(multipartFile(t, goldenFixture), 1)
	w := httptest.NewRecorder()
	h.Import(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on resolver error, got %d", w.Code)
	}
}

func TestCollectionImportHandler_WriterError_Returns500(t *testing.T) {
	resolver := &stubCardResolver{
		lookup: map[string]int{"ONS::Lightning Bolt": 700001},
	}
	writer := &stubInventoryWriter{err: errors.New("db write failure")}
	h := newImportHandler(resolver, writer, &collectionAccountLookup{accountID: 1, found: true})

	content := "4 Lightning Bolt (ONS) 197\n"
	req := withImportAuth(multipartFile(t, content), 1)
	w := httptest.NewRecorder()
	h.Import(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on writer error, got %d", w.Code)
	}
}

func TestCollectionImportHandler_AccountLookupError_Returns500(t *testing.T) {
	h := newImportHandler(
		&stubCardResolver{},
		&stubInventoryWriter{},
		&collectionAccountLookup{err: errors.New("db unreachable")},
	)
	req := withImportAuth(multipartFile(t, goldenFixture), 1)
	w := httptest.NewRecorder()
	h.Import(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 on account lookup error, got %d", w.Code)
	}
}
