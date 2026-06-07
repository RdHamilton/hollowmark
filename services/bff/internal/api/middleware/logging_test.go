package middleware_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
)

// newBufLogger returns a slog.Logger that writes JSON to a bytes.Buffer and
// a pointer to that buffer so tests can inspect the output.
func newBufLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	return logger, &buf
}

// parseLogLine parses the first line of buf as JSON and returns the decoded map.
func parseLogLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	var m map[string]any
	dec := json.NewDecoder(buf)

	if err := dec.Decode(&m); err != nil {
		t.Fatalf("log output is not valid JSON: %v\nraw: %s", err, buf.String())
	}

	return m
}

// ──────────────────────────────────────────────────────────────────────────────
// Output shape
// ──────────────────────────────────────────────────────────────────────────────

// TestStructuredLogger_EmitsJSONLine verifies that the middleware writes exactly
// one JSON log line containing the required fields.
func TestStructuredLogger_EmitsJSONLine(t *testing.T) {
	logger, buf := newBufLogger(t)
	middl := bffmiddleware.NewStructuredLogger(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	middl(handler).ServeHTTP(rr, req)

	m := parseLogLine(t, buf)

	requiredKeys := []string{
		"time", "level", "msg",
		"request_id", "method", "route", "path",
		"status", "latency_ms", "account_id_hash",
	}
	for _, k := range requiredKeys {
		if _, ok := m[k]; !ok {
			t.Errorf("log line missing field %q; got keys: %v", k, logKeys(m))
		}
	}
}

// TestStructuredLogger_StatusCodesMapToCorrectLevels verifies INFO/WARN/ERROR
// level selection by status code.
func TestStructuredLogger_StatusCodesMapToCorrectLevels(t *testing.T) {
	cases := []struct {
		status    int
		wantLevel string
	}{
		{http.StatusOK, "INFO"},
		{http.StatusCreated, "INFO"},
		{http.StatusBadRequest, "WARN"},
		{http.StatusUnauthorized, "WARN"},
		{http.StatusNotFound, "WARN"},
		{http.StatusInternalServerError, "ERROR"},
		{http.StatusBadGateway, "ERROR"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			logger, buf := newBufLogger(t)
			middl := bffmiddleware.NewStructuredLogger(logger)

			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			})

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			rr := httptest.NewRecorder()
			middl(handler).ServeHTTP(rr, req)

			m := parseLogLine(t, buf)

			if got := m["level"]; got != tc.wantLevel {
				t.Errorf("status %d: level = %q, want %q", tc.status, got, tc.wantLevel)
			}
		})
	}
}

// TestStructuredLogger_MethodAndPathRecorded verifies that method and path are
// correctly captured from the request.
func TestStructuredLogger_MethodAndPathRecorded(t *testing.T) {
	logger, buf := newBufLogger(t)
	middl := bffmiddleware.NewStructuredLogger(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/events", nil)
	rr := httptest.NewRecorder()
	middl(handler).ServeHTTP(rr, req)

	m := parseLogLine(t, buf)

	if got := m["method"]; got != "POST" {
		t.Errorf("method = %q, want POST", got)
	}

	if got := m["path"]; got != "/api/v1/ingest/events" {
		t.Errorf("path = %q, want /api/v1/ingest/events", got)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Latency
// ──────────────────────────────────────────────────────────────────────────────

// TestStructuredLogger_LatencyIsNonNegative verifies that latency_ms is a
// non-negative float64 value.
func TestStructuredLogger_LatencyIsNonNegative(t *testing.T) {
	logger, buf := newBufLogger(t)
	middl := bffmiddleware.NewStructuredLogger(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	middl(handler).ServeHTTP(rr, req)

	m := parseLogLine(t, buf)

	latency, ok := m["latency_ms"].(float64)
	if !ok {
		t.Fatalf("latency_ms is not a float64; got type %T value %v", m["latency_ms"], m["latency_ms"])
	}

	if latency < 0 {
		t.Errorf("latency_ms = %f, want >= 0", latency)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Account ID hashing — PII redaction
// ──────────────────────────────────────────────────────────────────────────────

// TestStructuredLogger_AccountIDHash_PresentWhenAuthenticated verifies that
// when a user ID is on the context the log line contains account_id_hash and
// NOT the raw user ID string.
func TestStructuredLogger_AccountIDHash_PresentWhenAuthenticated(t *testing.T) {
	const userID int64 = 42

	logger, buf := newBufLogger(t)
	middl := bffmiddleware.NewStructuredLogger(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/collection", nil)
	req = req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
	rr := httptest.NewRecorder()
	middl(handler).ServeHTTP(rr, req)

	m := parseLogLine(t, buf)

	hash, ok := m["account_id_hash"].(string)
	if !ok || hash == "" {
		t.Fatalf("account_id_hash missing or empty for authenticated user; got %v", m["account_id_hash"])
	}

	// The raw user ID must NOT appear in the log line.
	logLine := buf.String()
	if strings.Contains(logLine, `"42"`) || strings.Contains(logLine, `:42,`) || strings.Contains(logLine, `:42}`) {
		t.Errorf("raw user ID 42 must not appear in the log line; got: %s", logLine)
	}

	// The hash must be exactly 16 hex characters (SHA-256[:16]).
	if len(hash) != 16 {
		t.Errorf("account_id_hash length = %d, want 16; hash = %q", len(hash), hash)
	}
}

// TestStructuredLogger_AccountIDHash_EmptyForUnauthenticated verifies that
// when no user is on the context (public routes), account_id_hash is "".
func TestStructuredLogger_AccountIDHash_EmptyForUnauthenticated(t *testing.T) {
	logger, buf := newBufLogger(t)
	middl := bffmiddleware.NewStructuredLogger(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	middl(handler).ServeHTTP(rr, req)

	m := parseLogLine(t, buf)

	if hash := m["account_id_hash"]; hash != "" {
		t.Errorf("account_id_hash should be empty for unauthenticated request; got %q", hash)
	}
}

// TestStructuredLogger_AccountIDHash_IsDeterministic verifies that the same
// user ID always produces the same hash (stable for CloudWatch log correlation).
func TestStructuredLogger_AccountIDHash_IsDeterministic(t *testing.T) {
	const userID int64 = 99

	var hashes [3]string

	for i := range hashes {
		logger, buf := newBufLogger(t)
		middl := bffmiddleware.NewStructuredLogger(logger)

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req = req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
		rr := httptest.NewRecorder()
		middl(handler).ServeHTTP(rr, req)

		m := parseLogLine(t, buf)
		hash, ok := m["account_id_hash"].(string)

		if !ok {
			t.Fatalf("run %d: account_id_hash is not a string", i)
		}

		hashes[i] = hash
	}

	if hashes[0] != hashes[1] || hashes[1] != hashes[2] {
		t.Errorf("account_id_hash is not deterministic: %v", hashes)
	}
}

// TestStructuredLogger_DifferentUserIDs_ProduceDifferentHashes verifies that
// distinct user IDs hash to distinct values (collision-resistance sanity check).
func TestStructuredLogger_DifferentUserIDs_ProduceDifferentHashes(t *testing.T) {
	hash := func(userID int64) string {
		logger, buf := newBufLogger(t)
		middl := bffmiddleware.NewStructuredLogger(logger)

		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req = req.WithContext(bffmiddleware.WithUserID(req.Context(), userID))
		rr := httptest.NewRecorder()
		middl(handler).ServeHTTP(rr, req)

		m := parseLogLine(t, buf)

		return m["account_id_hash"].(string)
	}

	h1 := hash(1)
	h2 := hash(2)

	if h1 == h2 {
		t.Errorf("different user IDs produced the same hash %q — hash function collision", h1)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Level filtering
// ──────────────────────────────────────────────────────────────────────────────

// TestStructuredLogger_LevelFiltering_WarnLevelSuppressesInfo verifies that
// when the logger is configured at WARN level, INFO-level request logs (2xx
// status) are not written.
func TestStructuredLogger_LevelFiltering_WarnLevelSuppressesInfo(t *testing.T) {
	var buf bytes.Buffer
	// Set level to WARN — INFO requests should be suppressed.
	warnLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	middl := bffmiddleware.NewStructuredLogger(warnLogger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK) // INFO-level
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	middl(handler).ServeHTTP(rr, req)

	if buf.Len() != 0 {
		t.Errorf("expected no log output at WARN level for 200 request; got: %s", buf.String())
	}
}

// TestStructuredLogger_LevelFiltering_WarnLevelPassesWarn verifies that when
// the logger is at WARN level, a 4xx request IS logged.
func TestStructuredLogger_LevelFiltering_WarnLevelPassesWarn(t *testing.T) {
	var buf bytes.Buffer
	warnLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	}))

	middl := bffmiddleware.NewStructuredLogger(warnLogger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest) // WARN-level
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr := httptest.NewRecorder()
	middl(handler).ServeHTTP(rr, req)

	if buf.Len() == 0 {
		t.Error("expected log output at WARN level for 400 request; got nothing")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Status passthrough
// ──────────────────────────────────────────────────────────────────────────────

// TestStructuredLogger_DoesNotAlterResponseStatus verifies the middleware is
// transparent: it does not change the status code written to the real response.
func TestStructuredLogger_DoesNotAlterResponseStatus(t *testing.T) {
	logger, _ := newBufLogger(t)
	middl := bffmiddleware.NewStructuredLogger(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/decks", nil)
	rr := httptest.NewRecorder()
	middl(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("response status = %d, want 201; middleware must not alter status", rr.Code)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// NewDefaultLogger
// ──────────────────────────────────────────────────────────────────────────────

// TestNewDefaultLogger_ReturnsNonNilLogger verifies the factory never returns nil.
func TestNewDefaultLogger_ReturnsNonNilLogger(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")

	logger := bffmiddleware.NewDefaultLogger()
	if logger == nil {
		t.Fatal("NewDefaultLogger returned nil")
	}
}

// TestNewDefaultLogger_AcceptsValidLevels verifies that all supported LOG_LEVEL
// values produce a non-nil logger without panicking.
func TestNewDefaultLogger_AcceptsValidLevels(t *testing.T) {
	for _, level := range []string{"DEBUG", "INFO", "WARN", "WARNING", "ERROR", "", "INVALID"} {
		level := level
		t.Run(level, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", level)

			logger := bffmiddleware.NewDefaultLogger()
			if logger == nil {
				t.Fatalf("LOG_LEVEL=%q: NewDefaultLogger returned nil", level)
			}
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────────────

func logKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	return keys
}
