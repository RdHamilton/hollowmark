package repository_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// waitlistStrPtr returns a pointer to s.
// Uses a unique name to avoid collision with ptr/strPtr helpers in other test files.
func waitlistStrPtr(s string) *string { return &s }

// TestWaitlistRepository_InsertIfNew_StoresAllUTMColumns asserts that a signup
// with all six UTM fields (source, medium, campaign, content, term, referrer)
// persists every column correctly and returns a non-zero position.
//
// This is the primary RED test for ticket #130 — it will fail until the
// migration and the extended InsertIfNew signature are in place.
func TestWaitlistRepository_InsertIfNew_StoresAllUTMColumns(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewWaitlistRepository(db)

	const email = "utm-all-fields@test.example"
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM waitlist_entries WHERE email = $1`, email)
	})

	utmSource := waitlistStrPtr("google")
	utmMedium := waitlistStrPtr("cpc")
	utmCampaign := waitlistStrPtr("summer-sale")
	utmContent := waitlistStrPtr("banner-v2")
	utmTerm := waitlistStrPtr("magic cards")
	referrer := waitlistStrPtr("https://example.com/landing")

	id, position, created, err := repo.InsertIfNew(
		context.Background(),
		email,
		utmSource, utmMedium, utmCampaign,
		utmContent, utmTerm,
		referrer,
	)
	if err != nil {
		t.Fatalf("InsertIfNew: %v", err)
	}
	if !created {
		t.Fatal("expected created=true for new email, got false")
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}
	if position < 0 {
		t.Fatalf("expected position >= 0, got %d", position)
	}

	// Read back the row and assert all six attribution columns.
	var (
		gotSource, gotMedium, gotCampaign sql.NullString
		gotContent, gotTerm               sql.NullString
		gotReferrer                       sql.NullString
	)
	err = db.QueryRowContext(
		context.Background(),
		`SELECT utm_source, utm_medium, utm_campaign,
		        utm_content, utm_term, referrer
		   FROM waitlist_entries WHERE email = $1`, email,
	).Scan(&gotSource, &gotMedium, &gotCampaign,
		&gotContent, &gotTerm, &gotReferrer)
	if err != nil {
		t.Fatalf("read back row: %v", err)
	}

	wantFields := []struct {
		name string
		got  sql.NullString
		want string
	}{
		{"utm_source", gotSource, "google"},
		{"utm_medium", gotMedium, "cpc"},
		{"utm_campaign", gotCampaign, "summer-sale"},
		{"utm_content", gotContent, "banner-v2"},
		{"utm_term", gotTerm, "magic cards"},
		{"referrer", gotReferrer, "https://example.com/landing"},
	}
	for _, f := range wantFields {
		if !f.got.Valid {
			t.Errorf("%s: expected %q, got NULL", f.name, f.want)
		} else if f.got.String != f.want {
			t.Errorf("%s: expected %q, got %q", f.name, f.want, f.got.String)
		}
	}
}

// TestWaitlistRepository_InsertIfNew_NullUTMContentTerm asserts that omitting
// utm_content and utm_term (nil) stores NULL in those columns without error,
// and that the existing utm_source/medium/campaign/referrer columns still work.
func TestWaitlistRepository_InsertIfNew_NullUTMContentTerm(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewWaitlistRepository(db)

	const email = "utm-null-content-term@test.example"
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM waitlist_entries WHERE email = $1`, email)
	})

	_, _, created, err := repo.InsertIfNew(
		context.Background(),
		email,
		waitlistStrPtr("email"), waitlistStrPtr("newsletter"), waitlistStrPtr("weekly"),
		nil, // utm_content — should store NULL
		nil, // utm_term    — should store NULL
		waitlistStrPtr("https://example.com"),
	)
	if err != nil {
		t.Fatalf("InsertIfNew (nil content+term): %v", err)
	}
	if !created {
		t.Fatal("expected created=true")
	}

	var gotContent, gotTerm sql.NullString
	err = db.QueryRowContext(
		context.Background(),
		`SELECT utm_content, utm_term FROM waitlist_entries WHERE email = $1`, email,
	).Scan(&gotContent, &gotTerm)
	if err != nil {
		t.Fatalf("read back row: %v", err)
	}
	if gotContent.Valid {
		t.Errorf("utm_content: expected NULL, got %q", gotContent.String)
	}
	if gotTerm.Valid {
		t.Errorf("utm_term: expected NULL, got %q", gotTerm.String)
	}
}

// TestWaitlistRepository_InsertIfNew_DuplicateEmail_ReturnsFalse verifies the
// existing ON CONFLICT DO NOTHING behaviour is preserved after the signature
// change — a duplicate email returns created=false with no error.
func TestWaitlistRepository_InsertIfNew_DuplicateEmail_ReturnsFalse(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewWaitlistRepository(db)

	const email = "dup-email@test.example"
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM waitlist_entries WHERE email = $1`, email)
	})

	// First insert — must succeed.
	_, _, created, err := repo.InsertIfNew(context.Background(), email,
		nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("first InsertIfNew: %v", err)
	}
	if !created {
		t.Fatal("expected created=true on first insert")
	}

	// Second insert — same email, must return created=false.
	_, _, created, err = repo.InsertIfNew(context.Background(), email,
		nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("second InsertIfNew: %v", err)
	}
	if created {
		t.Fatal("expected created=false on duplicate email")
	}
}
