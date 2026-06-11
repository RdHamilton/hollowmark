package analytics_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gopkg.in/yaml.v3"
)

// taxonomyDoc mirrors the top-level structure of taxonomy.yml — used for
// direct YAML parsing in tests without importing the gen script types.
type taxonomyDoc struct {
	Events []taxonomyEvent `yaml:"events"`
}

type taxonomyEvent struct {
	Name       string             `yaml:"name"`
	Properties []taxonomyProperty `yaml:"properties"`
}

type taxonomyProperty struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	PIIClass string `yaml:"pii_class"`
}

func loadTaxonomy(t *testing.T) taxonomyDoc {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	// thisFile = .../services/bff/internal/analytics/deck_events_taxonomy_test.go
	// Walk up 5: analytics -> internal -> bff -> services -> repoRoot
	repoRoot := filepath.Clean(filepath.Join(thisFile, "..", "..", "..", "..", ".."))
	yamlPath := filepath.Join(repoRoot, "services", "bff", "internal", "analytics", "taxonomy.yml")

	raw, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("cannot read taxonomy.yml: %v", err)
	}
	var doc taxonomyDoc
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("cannot parse taxonomy.yml: %v", err)
	}
	return doc
}

// TestDeckEventsInTaxonomy asserts that every deck-operation event emitted
// from decks.go is registered in taxonomy.yml.  This prevents the analytics
// seam from accepting events that have no taxonomy entry and therefore no
// PII classification review.
func TestDeckEventsInTaxonomy(t *testing.T) {
	doc := loadTaxonomy(t)

	// Build a lookup set from the taxonomy.
	registered := make(map[string]struct{}, len(doc.Events))
	for _, ev := range doc.Events {
		registered[ev.Name] = struct{}{}
	}

	// These are the exact string literals emitted via analytics.Capture in
	// services/bff/internal/api/handlers/decks.go.  Keep in sync with that
	// file — if a name changes there it must change here (and in the taxonomy).
	deckEvents := []string{
		"get_deck",
		"create_deck",
		"update_deck",
		"delete_deck",
		"clone_deck",
		"export_deck",
	}

	for _, name := range deckEvents {
		if _, ok := registered[name]; !ok {
			t.Errorf("deck event %q is emitted in decks.go but is NOT registered in taxonomy.yml — add a taxonomy entry with pii_class annotations before emitting this event", name)
		}
	}
}

// TestExportDeckNamePIIClass asserts that the export_deck event's deck_name
// property carries a non-none pii_class.  deck_name is a free-text
// user-supplied string and must be classified as pii_hashed (the taxonomy's
// class for user-controlled content that can contain personal data).
func TestExportDeckNamePIIClass(t *testing.T) {
	doc := loadTaxonomy(t)

	var exportDeck *taxonomyEvent
	for i := range doc.Events {
		if doc.Events[i].Name == "export_deck" {
			exportDeck = &doc.Events[i]
			break
		}
	}
	if exportDeck == nil {
		t.Fatal("export_deck event not found in taxonomy.yml — add the entry first")
	}

	var deckNameProp *taxonomyProperty
	for i := range exportDeck.Properties {
		if exportDeck.Properties[i].Name == "deck_name" {
			deckNameProp = &exportDeck.Properties[i]
			break
		}
	}
	if deckNameProp == nil {
		t.Fatal("export_deck event has no deck_name property in taxonomy.yml")
	}

	// deck_name is a user-supplied free-text string.  The correct pii_class
	// for user-controlled content is "pii_hashed" — this means the value
	// must be hashed before being forwarded to PostHog.
	const wantPIIClass = "pii_hashed"
	if deckNameProp.PIIClass != wantPIIClass {
		t.Errorf("export_deck.deck_name has pii_class %q, want %q — user-supplied free-text strings must be classified as pii_hashed", deckNameProp.PIIClass, wantPIIClass)
	}
}
