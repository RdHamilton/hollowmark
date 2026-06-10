package config_test

import (
	"testing"

	"github.com/RdHamilton/hollowmark/services/bff/internal/config"
)

// TestLoad_InternalSvcSecret_OptionalInDevelopment verifies that
// INTERNAL_SVC_SECRET is not required in development mode — the middleware
// will fail-closed but the BFF starts without error.
func TestLoad_InternalSvcSecret_OptionalInDevelopment(t *testing.T) {
	t.Setenv("MTGA_ENV", "development")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("CLERK_SECRET_KEY", "")
	t.Setenv("ANALYTICS_PII_SALT", "")
	t.Setenv("INTERNAL_SVC_SECRET", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("development mode with empty INTERNAL_SVC_SECRET should not error: %v", err)
	}
	if cfg.InternalSvcSecret != "" {
		t.Errorf("expected empty InternalSvcSecret, got %q", cfg.InternalSvcSecret)
	}
}

// TestLoad_InternalSvcSecret_RequiredInProduction verifies that an unset
// INTERNAL_SVC_SECRET causes Load to return an error when MTGA_ENV=production.
func TestLoad_InternalSvcSecret_RequiredInProduction(t *testing.T) {
	t.Setenv("MTGA_ENV", "production")
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("CLERK_SECRET_KEY", "sk_test_dummy")
	t.Setenv("ANALYTICS_PII_SALT", "testsalt")
	t.Setenv("INTERNAL_SVC_SECRET", "")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when MTGA_ENV=production and INTERNAL_SVC_SECRET is unset")
	}
}

// TestLoad_InternalSvcSecret_RequiredInStaging verifies that an unset
// INTERNAL_SVC_SECRET causes Load to return an error when MTGA_ENV=staging.
func TestLoad_InternalSvcSecret_RequiredInStaging(t *testing.T) {
	t.Setenv("MTGA_ENV", "staging")
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("CLERK_SECRET_KEY", "sk_test_dummy")
	t.Setenv("ANALYTICS_PII_SALT", "testsalt")
	t.Setenv("INTERNAL_SVC_SECRET", "")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error when MTGA_ENV=staging and INTERNAL_SVC_SECRET is unset")
	}
}

// TestLoad_InternalSvcSecret_FromEnv verifies that INTERNAL_SVC_SECRET is
// surfaced as Config.InternalSvcSecret with whitespace trimmed.
func TestLoad_InternalSvcSecret_FromEnv(t *testing.T) {
	t.Setenv("MTGA_ENV", "development")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("INTERNAL_SVC_SECRET", "  aabbccdd1122334455667788990011aabbccdd1122334455667788990011aabb  ")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const want = "aabbccdd1122334455667788990011aabbccdd1122334455667788990011aabb"
	if cfg.InternalSvcSecret != want {
		t.Errorf("InternalSvcSecret: want %q, got %q", want, cfg.InternalSvcSecret)
	}
}
