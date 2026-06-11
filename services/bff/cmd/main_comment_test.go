package main_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestAnalyticsPIIRuleComment_Exists is the CI grep-guard for the PII HASHING
// RULE block comment in main.go.
//
// The comment is a security invariant (I-10 / AC1 of ticket #1173): any future
// author adding a PII analytics property must see the rule at the exact wiring
// site.  Deleting the comment silently would remove the only in-code forcing
// function at the analytics.NewClient call.  This test makes that impossible
// without a deliberate, reviewed change.
//
// Pattern mirrors TestNoHashDuplicates_GrepGuard in
// internal/identityhash/hash_test.go — runtime.Caller(0) derives the path at
// runtime so no absolute/machine-specific path is ever hardcoded.
func TestAnalyticsPIIRuleComment_Exists(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) returned false — cannot derive cmd dir")
	}
	// thisFile = .../services/bff/cmd/main_comment_test.go
	// main.go lives in the same directory.
	mainGoPath := filepath.Join(filepath.Dir(thisFile), "main.go")

	content, err := os.ReadFile(mainGoPath)
	if err != nil {
		t.Fatalf("could not read main.go at %s: %v", mainGoPath, err)
	}

	const requiredLiteral = "PII HASHING RULE"
	if !strings.Contains(string(content), requiredLiteral) {
		t.Errorf(
			"main.go does not contain the required literal %q\n"+
				"The PII HASHING RULE block comment must be present above the "+
				"analytics.NewClient call so future authors know to hash any PII "+
				"via identityhash.HashPII before passing it to analytics.Capture.\n"+
				"See ticket #1173 and identityhash/hash.go for the canonical implementation.",
			requiredLiteral,
		)
	}
}
