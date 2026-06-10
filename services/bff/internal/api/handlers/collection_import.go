package handlers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// ─── interfaces ──────────────────────────────────────────────────────────────

// CardResolver resolves a (setCode, name) pair from the MTGA Arena export to
// an integer arena_id (used as card_inventory.card_id).
type CardResolver interface {
	ResolveArenaID(ctx context.Context, setCode, name string) (int, bool, error)
}

// InventoryWriter is the write surface of CardInventoryRepository that the
// import handler needs.  Scoped to UpsertDelta so the handler can be tested
// with a lightweight stub.
type InventoryWriter interface {
	UpsertDelta(ctx context.Context, u repository.CardInventoryUpsert) error
}

// ─── handler ─────────────────────────────────────────────────────────────────

// CollectionImportHandler serves POST /api/v1/collection/import.
// It accepts a multipart/form-data file in the MTGA Arena export format,
// resolves each row's arena_id via set_cards, and upserts into card_inventory
// using the existing UpsertDelta path.
type CollectionImportHandler struct {
	resolver CardResolver
	writer   InventoryWriter
	accounts AccountLookup
}

// NewCollectionImportHandler returns a handler wired with the provided
// resolver, writer, and account lookup.
func NewCollectionImportHandler(
	resolver CardResolver,
	writer InventoryWriter,
	accounts AccountLookup,
) *CollectionImportHandler {
	return &CollectionImportHandler{
		resolver: resolver,
		writer:   writer,
		accounts: accounts,
	}
}

// importResponse is the wire shape for POST /api/v1/collection/import.
// accepted counts rows upserted; rejected collects per-row failure reasons.
type importResponse struct {
	Accepted int            `json:"accepted"`
	Rejected []importReject `json:"rejected"`
}

// importReject records why a single parsed row was not written.
type importReject struct {
	Line   string `json:"line"`
	Reason string `json:"reason"`
}

// Import handles POST /api/v1/collection/import.
//
// The request must be multipart/form-data with a single "file" field whose
// content is in the MTGA Arena in-game export format:
//
//	<quantity> <CardName> (<SetCode>) <collectorNumber>
//
// Lines starting with "//" and blank lines are skipped.  Rows that fail
// arena_id resolution go to "rejected" but do not abort the import.
//
// Returns:
//   - 200 with {accepted, rejected} on any partial or full success.
//   - 400 when the file is absent, empty, or has zero parseable rows.
//   - 401 when the request has no authenticated user.
//   - 404 when the authenticated user has no matching account row.
//   - 500 on internal errors.
func (h *CollectionImportHandler) Import(w http.ResponseWriter, r *http.Request) {
	userID, ok := bffmiddleware.UserIDFromContext(r.Context())
	if !ok {
		writeJSONError(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	accountID, found, err := h.accounts.GetAccountIDByUserID(r.Context(), userID)
	if err != nil {
		log.Printf("[CollectionImportHandler.Import] GetAccountIDByUserID userID=%d: %v", userID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if !found {
		writeJSONError(w, "account not found", http.StatusNotFound)
		return
	}

	// 32 MiB parse limit — a full Arena collection is well under 1 MiB.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeJSONError(w, "file upload required (multipart/form-data)", http.StatusBadRequest)
		return
	}

	f, _, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, "missing 'file' field in form", http.StatusBadRequest)
		return
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(f)
	if err != nil {
		log.Printf("[CollectionImportHandler.Import] read file accountID=%d: %v", accountID, err)
		writeJSONError(w, "internal server error", http.StatusInternalServerError)
		return
	}

	if len(strings.TrimSpace(string(raw))) == 0 {
		writeJSONError(w, "file is empty", http.StatusBadRequest)
		return
	}

	// ── parse ───────────────────────────────────────────────────────────────

	type parsedRow struct {
		qty     int
		name    string
		setCode string
		rawLine string
	}

	var parsed []parsedRow
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		qty, name, setCode, ok := ParseArenaLine(line)
		if !ok {
			continue
		}
		parsed = append(parsed, parsedRow{
			qty:     qty,
			name:    name,
			setCode: setCode,
			rawLine: line,
		})
	}

	if len(parsed) == 0 {
		writeJSONError(w, "no parseable rows found in file", http.StatusBadRequest)
		return
	}

	// ── resolve + upsert ────────────────────────────────────────────────────

	// Compute snapshot_hash over the sorted (arenaID, count) pairs of the
	// rows that resolve successfully. Sorting ensures the hash is
	// deterministic regardless of the row order in the file.
	//
	// We do two passes:
	//   1. Resolve all rows and collect (arenaID, count) for hashing.
	//   2. Compute the hash, then upsert all resolved rows.

	var resolved []importEntry
	var rejected []importReject

	for _, p := range parsed {
		arenaID, found, err := h.resolver.ResolveArenaID(r.Context(), p.setCode, p.name)
		if err != nil {
			log.Printf("[CollectionImportHandler.Import] ResolveArenaID accountID=%d set=%q name=%q: %v",
				accountID, p.setCode, p.name, err)
			writeJSONError(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if !found {
			rejected = append(rejected, importReject{
				Line:   p.rawLine,
				Reason: fmt.Sprintf("card not found in set_cards: name=%q set_code=%q", p.name, p.setCode),
			})
			continue
		}
		resolved = append(resolved, importEntry{arenaID: arenaID, count: p.qty})
	}

	// Build snapshot_hash from the sorted resolved set.
	snapshotHash := buildSnapshotHash(resolved)

	// Upsert each resolved row.
	for _, rv := range resolved {
		if err := h.writer.UpsertDelta(r.Context(), repository.CardInventoryUpsert{
			AccountID:    accountID,
			CardID:       rv.arenaID,
			Count:        rv.count,
			SnapshotHash: snapshotHash,
		}); err != nil {
			log.Printf("[CollectionImportHandler.Import] UpsertDelta accountID=%d cardID=%d: %v",
				accountID, rv.arenaID, err)
			writeJSONError(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}

	if rejected == nil {
		rejected = []importReject{}
	}

	writeMatchesJSON(w, importResponse{
		Accepted: len(resolved),
		Rejected: rejected,
	})
}

// ─── ParseArenaLine ──────────────────────────────────────────────────────────

// ParseArenaLine parses a single line from the MTGA Arena in-game export
// format:
//
//	<quantity> <CardName> (<SetCode>) <collectorNumber>
//
// Lines that start with "//" (comments), are blank, or do not match the
// expected structure return ok=false.  A quantity of zero or less also returns
// ok=false.
//
// The function is exported so the handler test suite can unit-test it directly
// without constructing a full HTTP handler.
func ParseArenaLine(line string) (qty int, name, setCode string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "//") {
		return 0, "", "", false
	}

	// The format is: <N> <name> (<SET>) <collNum>
	// Split on the first space to get the quantity token.
	spIdx := strings.Index(line, " ")
	if spIdx < 0 {
		return 0, "", "", false
	}

	qtyStr := line[:spIdx]
	rest := strings.TrimSpace(line[spIdx+1:])

	q, err := strconv.Atoi(qtyStr)
	if err != nil || q <= 0 {
		return 0, "", "", false
	}

	// Find the last "(<SETCODE>)" token — it must be preceded by the card name.
	// There may be multiple parenthesised tokens in the name (e.g. "Serra, the
	// Benevolent") so we find the LAST "(" that contains no spaces (set codes
	// are 2–5 uppercase letters).
	openIdx := strings.LastIndex(rest, "(")
	if openIdx < 0 {
		return 0, "", "", false
	}
	closeIdx := strings.Index(rest[openIdx:], ")")
	if closeIdx < 0 {
		return 0, "", "", false
	}
	closeIdx += openIdx

	cardName := strings.TrimSpace(rest[:openIdx])
	set := strings.TrimSpace(rest[openIdx+1 : closeIdx])

	if cardName == "" || set == "" {
		return 0, "", "", false
	}

	return q, cardName, set, true
}

// ─── snapshot hash ───────────────────────────────────────────────────────────

// importEntry holds a single resolved (arenaID, count) pair before upsert.
type importEntry struct {
	arenaID int
	count   int
}

// buildSnapshotHash computes a SHA-256 over the sorted (arenaID, count) set.
// Sorting by arenaID ensures the hash is deterministic regardless of parse
// order. The result is returned as a lowercase hex string.
func buildSnapshotHash(rows []importEntry) string {
	sorted := make([]importEntry, len(rows))
	copy(sorted, rows)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].arenaID < sorted[j].arenaID })

	h := sha256.New()
	for _, r := range sorted {
		_, _ = fmt.Fprintf(h, "%d:%d\n", r.arenaID, r.count)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
