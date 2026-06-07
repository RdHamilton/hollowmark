package ratingsclient_test

// Phase B tests — verify that the ratingsclient retains ALSA, Color, Rarity,
// ATA, and GIHCount from the BFF response, and exposes them via the new
// CardMetaByID lookup method.
//
// Per TDD: these tests are written first and fail before implementation.

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// sampleBodyWithMeta returns a draft-ratings response that includes
// ALSA, color, rarity, ata, and gih_count per card, in addition to the
// Phase A fields. This is the full wire shape the BFF already emits.
func sampleBodyWithMeta(set, format string) string {
	return fmt.Sprintf(`{
		"set_code": %q,
		"draft_format": %q,
		"card_ratings": [
			{
				"arena_id": 100,
				"name": "Lightning Bolt",
				"gihwr": 58.4,
				"color": "R",
				"rarity": "rare",
				"alsa": 2.3,
				"ata": 2.1,
				"gih_count": 1500
			},
			{
				"arena_id": 200,
				"name": "Bulk Rare",
				"gihwr": 51.0,
				"color": "G",
				"rarity": "rare",
				"alsa": 8.7,
				"ata": 8.5,
				"gih_count": 120
			},
			{
				"arena_id": 300,
				"name": "No Data Card",
				"color": "U",
				"rarity": "uncommon"
			}
		],
		"color_ratings": []
	}`, set, format)
}

// ─── CardMetaByID — new Phase B lookup ───────────────────────────────────

// TestPhaseB_CardMetaByID_RetainsALSA verifies that ALSA is retained from
// the wire response and returned via CardMetaByID.
func TestPhaseB_CardMetaByID_RetainsALSA(t *testing.T) {
	bff := newFakeBFF(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(envelopeJSON(sampleBodyWithMeta("BLB", "PremierDraft"))))
	})
	defer bff.Close()

	c, _ := newClient(bff, 24*time.Hour)
	if err := c.Warm(context.Background(), "BLB", "PremierDraft"); err != nil {
		t.Fatal(err)
	}

	meta, ok := c.CardMetaByID("100")
	if !ok {
		t.Fatal("CardMetaByID(100) returned !ok — card should be present")
	}
	if meta.ALSA != 2.3 {
		t.Errorf("ALSA = %v, want 2.3", meta.ALSA)
	}
}

// TestPhaseB_CardMetaByID_RetainsColor verifies Color is retained.
func TestPhaseB_CardMetaByID_RetainsColor(t *testing.T) {
	bff := newFakeBFF(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(envelopeJSON(sampleBodyWithMeta("BLB", "PremierDraft"))))
	})
	defer bff.Close()

	c, _ := newClient(bff, 24*time.Hour)
	_ = c.Warm(context.Background(), "BLB", "PremierDraft")

	meta, ok := c.CardMetaByID("100")
	if !ok {
		t.Fatal("CardMetaByID(100) not found")
	}
	if len(meta.Colors) == 0 || meta.Colors[0] != "R" {
		t.Errorf("Colors = %v, want [R]", meta.Colors)
	}
}

// TestPhaseB_CardMetaByID_RetainsRarity verifies Rarity is retained.
func TestPhaseB_CardMetaByID_RetainsRarity(t *testing.T) {
	bff := newFakeBFF(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(envelopeJSON(sampleBodyWithMeta("BLB", "PremierDraft"))))
	})
	defer bff.Close()

	c, _ := newClient(bff, 24*time.Hour)
	_ = c.Warm(context.Background(), "BLB", "PremierDraft")

	meta, ok := c.CardMetaByID("100")
	if !ok {
		t.Fatal("CardMetaByID(100) not found")
	}
	if meta.Rarity != "rare" {
		t.Errorf("Rarity = %q, want %q", meta.Rarity, "rare")
	}
}

// TestPhaseB_CardMetaByID_RetainsGIHCount verifies GIHCount is retained
// and is the right value. ADR-047: GIHCount is a *int, nil when absent.
func TestPhaseB_CardMetaByID_RetainsGIHCount(t *testing.T) {
	bff := newFakeBFF(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(envelopeJSON(sampleBodyWithMeta("BLB", "PremierDraft"))))
	})
	defer bff.Close()

	c, _ := newClient(bff, 24*time.Hour)
	_ = c.Warm(context.Background(), "BLB", "PremierDraft")

	// Card 100: gih_count=1500
	meta100, ok := c.CardMetaByID("100")
	if !ok {
		t.Fatal("CardMetaByID(100) not found")
	}
	if meta100.GIHCount == nil {
		t.Fatal("GIHCount should not be nil for card 100 (1500 games in wire)")
	}
	if *meta100.GIHCount != 1500 {
		t.Errorf("GIHCount = %d, want 1500", *meta100.GIHCount)
	}

	// Card 300: no gih_count in wire → nil
	meta300, ok := c.CardMetaByID("300")
	if !ok {
		t.Fatal("CardMetaByID(300) not found")
	}
	if meta300.GIHCount != nil {
		t.Errorf("GIHCount for card 300 (no wire value) = %d, want nil", *meta300.GIHCount)
	}
}

// TestPhaseB_CardMetaByID_HighALSACard verifies the bulk-rare case (arena_id 200)
// which has ALSA 8.7 and low GIHCount.
func TestPhaseB_CardMetaByID_HighALSACard(t *testing.T) {
	bff := newFakeBFF(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(envelopeJSON(sampleBodyWithMeta("BLB", "PremierDraft"))))
	})
	defer bff.Close()

	c, _ := newClient(bff, 24*time.Hour)
	_ = c.Warm(context.Background(), "BLB", "PremierDraft")

	meta, ok := c.CardMetaByID("200")
	if !ok {
		t.Fatal("CardMetaByID(200) not found")
	}
	if meta.ALSA != 8.7 {
		t.Errorf("ALSA = %v, want 8.7", meta.ALSA)
	}
	if meta.GIHCount == nil || *meta.GIHCount != 120 {
		t.Errorf("GIHCount = %v, want 120", meta.GIHCount)
	}
}

// TestPhaseB_CardMetaByID_NotFound — unknown arena_id returns (zero, false).
func TestPhaseB_CardMetaByID_NotFound(t *testing.T) {
	bff := newFakeBFF(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(envelopeJSON(sampleBodyWithMeta("BLB", "PremierDraft"))))
	})
	defer bff.Close()

	c, _ := newClient(bff, 24*time.Hour)
	_ = c.Warm(context.Background(), "BLB", "PremierDraft")

	_, ok := c.CardMetaByID("9999")
	if ok {
		t.Error("CardMetaByID(9999) should return !ok for unknown card")
	}
}

// TestPhaseB_ExistingGIHWRUnchanged — widening the wire struct must not
// break existing GIHWR lookups.
func TestPhaseB_ExistingGIHWRUnchanged(t *testing.T) {
	bff := newFakeBFF(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(envelopeJSON(sampleBodyWithMeta("BLB", "PremierDraft"))))
	})
	defer bff.Close()

	c, _ := newClient(bff, 24*time.Hour)
	_ = c.Warm(context.Background(), "BLB", "PremierDraft")

	v, ok := c.GIHWR("100", "PremierDraft")
	if !ok || v != 58.4 {
		t.Errorf("GIHWR(100) = (%v, %v), want (58.4, true) — must be unchanged by Phase B widening", v, ok)
	}
}
