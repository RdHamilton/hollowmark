package identityhash_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/identityhash"
)

// TestHashAccountID_ProducesExpectedOutput verifies the canonical SHA-256-hex[:16]
// output for a known input, confirming byte-for-byte compatibility with the
// three former copies in handlers/posthog.go, middleware/logging.go, and
// projection/worker.go.
func TestHashAccountID_ProducesExpectedOutput(t *testing.T) {
	// SHA-256("42") hex, first 16 chars — pre-computed for the regression pin.
	// Any change to the hash function would break existing PostHog distinct_ids.
	const input = "42"
	const want = "73475cb40a568e8d"

	got := identityhash.HashAccountID(input)
	if got != want {
		t.Errorf("HashAccountID(%q) = %q, want %q", input, got, want)
	}
}

// TestHashAccountID_Returns16Chars verifies the output is always exactly 16
// hex characters regardless of input length or content.
func TestHashAccountID_Returns16Chars(t *testing.T) {
	cases := []string{"0", "1", "99999", "user_abc", ""}
	for _, tc := range cases {
		got := identityhash.HashAccountID(tc)
		if len(got) != 16 {
			t.Errorf("HashAccountID(%q) length = %d, want 16; value = %q", tc, len(got), got)
		}
	}
}

// TestHashAccountID_DeterministicAndStable verifies the function is pure:
// the same input always yields the same hash across repeated calls.
func TestHashAccountID_DeterministicAndStable(t *testing.T) {
	const id = "123456789"
	h1 := identityhash.HashAccountID(id)
	h2 := identityhash.HashAccountID(id)
	if h1 != h2 {
		t.Errorf("HashAccountID not deterministic: %q != %q", h1, h2)
	}
}

// TestHashAccountID_DifferentInputsDifferentHashes verifies two distinct ids
// produce distinct hashes (no accidental constant-return regression).
func TestHashAccountID_DifferentInputsDifferentHashes(t *testing.T) {
	h1 := identityhash.HashAccountID("1")
	h2 := identityhash.HashAccountID("2")
	if h1 == h2 {
		t.Errorf("HashAccountID(\"1\") == HashAccountID(\"2\") — must be distinct")
	}
}

// TestNoHashDuplicates_GrepGuard is the FM-2 CI grep-guard.
//
// It asserts that no file under services/bff/ contains a sha256.Sum256 call
// inside a hashAccountID-family function (hashAccountID, hashAccountIDForLog,
// hashAccountIDProjection) outside of internal/identityhash/hash.go.
//
// A new copy of the hashing logic instead of importing identityhash.HashAccountID
// will fail this test. The only permitted location is the canonical package.
func TestNoHashDuplicates_GrepGuard(t *testing.T) {
	// Derive services/bff root from this test file's runtime path — no hardcoded
	// absolute paths so this passes on any machine / CI checkout.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) returned false — cannot derive bffRoot")
	}
	// thisFile = .../services/bff/internal/identityhash/hash_test.go
	// Walk up 4 levels: identityhash -> internal -> bff
	bffRoot := filepath.Clean(filepath.Join(thisFile, "..", "..", "..", ".."))

	// The canonical implementation dir — the only permitted location.
	canonicalDir := filepath.Join(bffRoot, "internal", "identityhash")

	var violations []string

	err := filepath.WalkDir(bffRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			// Skip non-Go dirs.
			if base == "vendor" || base == ".git" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// The canonical package is the one permitted location — skip the entire dir.
		if strings.HasPrefix(filepath.Dir(path), canonicalDir) {
			return nil
		}
		// Skip _test.go files — they may reference the function names as string
		// literals (e.g. in grep-guard assertions) without being duplicate impls.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		// Flag files where a hashAccountID-family function definition directly
		// contains a sha256.Sum256 call — i.e. an actual duplicate implementation,
		// not merely a delegation wrapper or an unrelated sha256 use elsewhere.
		//
		// Detection: scan line by line.  When we enter a func hashAccountID*
		// definition, check whether sha256.Sum256 appears before the function closes.
		lines := strings.Split(string(content), "\n")
		inHashFn := false
		braceDepth := 0
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			// Detect entry into a hashAccountID-family function definition.
			if !inHashFn {
				if strings.HasPrefix(trimmed, "func hashAccountID") ||
					strings.HasPrefix(trimmed, "func hashAccountIDForLog") ||
					strings.HasPrefix(trimmed, "func hashAccountIDProjection") {
					inHashFn = true
					braceDepth = strings.Count(line, "{") - strings.Count(line, "}")
				}
				continue
			}
			// Inside the function body: track brace depth.
			braceDepth += strings.Count(line, "{") - strings.Count(line, "}")
			// If the function directly calls sha256.Sum256 it is a duplicate impl.
			if strings.Contains(line, "sha256.Sum256") {
				violations = append(violations, path)
				break
			}
			// Function closed.
			if braceDepth <= 0 {
				inHashFn = false
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%s): %v", bffRoot, err)
	}

	if len(violations) > 0 {
		t.Errorf("FM-2 grep-guard: %d file(s) contain a duplicate hashAccountID sha256 implementation outside internal/identityhash/hash.go — replace with identityhash.HashAccountID:\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}
}
