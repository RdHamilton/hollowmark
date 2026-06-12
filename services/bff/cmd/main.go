package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"

	"github.com/RdHamilton/hollowmark/services/bff/internal/analytics"
	"github.com/RdHamilton/hollowmark/services/bff/internal/api/handlers"
	bffmiddleware "github.com/RdHamilton/hollowmark/services/bff/internal/api/middleware"
	"github.com/RdHamilton/hollowmark/services/bff/internal/api/sse"
	"github.com/RdHamilton/hollowmark/services/bff/internal/config"
	"github.com/RdHamilton/hollowmark/services/bff/internal/dbpool"
	"github.com/RdHamilton/hollowmark/services/bff/internal/email"
	"github.com/RdHamilton/hollowmark/services/bff/internal/erasure"
	"github.com/RdHamilton/hollowmark/services/bff/internal/observability"
	"github.com/RdHamilton/hollowmark/services/bff/internal/projection"
	"github.com/RdHamilton/hollowmark/services/bff/internal/reconciler"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage"
	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
	contract "github.com/RdHamilton/hollowmark/services/contract"
	"github.com/getsentry/sentry-go"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	posthoglib "github.com/posthog/posthog-go"

	"github.com/clerk/clerk-sdk-go/v2"
	clerkuser "github.com/clerk/clerk-sdk-go/v2/user"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// version is injected at build time via -ldflags "-X main.version=<tag>" by
// .github/actions/build-bff/action.yml.  It correlates Sentry events to the
// deployed build tag.  When empty or "dev", cfg.GitCommit is used as the
// release identifier instead (backward-compat for pre-PR-#2680 deploys and
// local development).
var version string

var port = flag.Int("port", 8080, "HTTP server port")

// printEmbeddedVersion is set by --print-embedded-version.  When true, the
// binary prints the highest embedded migration version as a bare integer and
// exits immediately — with no DB connection and no config/SSM load.
//
// Used by restart-bff.sh and restart-bff-staging.sh during deployment:
// the staged binary (already mv'd to /usr/local/bin/ by stage-binary.sh) is
// invoked with this flag to read the staged embedded version before the
// service is restarted, so the migration-skew guard compares the about-to-
// start binary against the DB — not the currently-running binary (#1151).
var printEmbeddedVersion = flag.Bool("print-embedded-version", false, "print the highest embedded migration version as a bare integer and exit")

func runMigrationsWithRetry(dsn string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		log.Println("Running database migrations...")
		err := storage.RunMigrations(dsn)
		if err == nil {
			log.Println("Migrations complete.")
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("migration init: %w", err)
		}
		log.Printf("Database not ready, retrying in 1s: %v", err)
		time.Sleep(time.Second)
	}
}

func main() {
	flag.Parse()

	// --print-embedded-version: print the highest embedded migration version as
	// a bare integer and exit.  No DB, no config, no SSM — safe to call
	// mid-deploy against the staged binary.
	if *printEmbeddedVersion {
		v, err := storage.EmbeddedMaxVersion()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: EmbeddedMaxVersion: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("%d\n", v)
		os.Exit(0)
	}

	// BFF_PORT env var is used as a fallback when -port is not explicitly
	// provided on the command line.  This lets the staging systemd unit set
	// Environment=BFF_PORT=8081 without hardcoding -port in ExecStart (which
	// gets overwritten on every deploy).  An explicit -port CLI flag always wins.
	portFlagSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "port" {
			portFlagSet = true
		}
	})
	if !portFlagSet {
		if envPort := os.Getenv("BFF_PORT"); envPort != "" {
			if _, err := fmt.Sscanf(envPort, "%d", port); err != nil {
				log.Fatalf("invalid BFF_PORT %q: %v", envPort, err)
			}
		}
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	// Runtime Secrets Manager resolution is OPT-IN as of #2461.
	//
	// Default (toggle unset / not "true"): the provisioner-side deploy script
	// has already spliced fresh RDS credentials into DATABASE_URL under a
	// scoped deploy role, so the BFF never constructs an AWS SDK client.
	// This keeps the EC2 instance role free of secretsmanager:GetSecretValue
	// and removes a startup failure mode (AccessDenied → crash-loop).
	//
	// Opt-in (BFF_DB_RESOLVE_FROM_SM=true AND DB_SECRET_ARN set): retains
	// the legacy runtime-resolution path for future rotation-resilience.
	secretARN := os.Getenv("DB_SECRET_ARN")
	if shouldResolveFromSM(os.Getenv("BFF_DB_RESOLVE_FROM_SM"), secretARN) {
		secretCtx, secretCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer secretCancel()
		resolved, resolveErr := resolveDBURL(secretCtx, fetchCredsFromAWS, secretARN, cfg.DatabaseURL)
		if resolveErr != nil {
			log.Fatalf("BFF_DB_RESOLVE_FROM_SM: %v", resolveErr)
		}
		cfg.DatabaseURL = resolved
		log.Printf("DB credentials resolved from Secrets Manager (arn=...%s)", secretARN[len(secretARN)-12:])
	}

	// Initialise Sentry error monitoring.  The DSN is read from SENTRY_DSN
	// (sourced from SSM /vaultmtg/prod/sentry-bff-dsn at deploy time).
	// When empty, Sentry is disabled — all SDK calls become no-ops.
	// The DSN is never logged.
	if cfg.SentryDSN != "" {
		sentryOpts := sentry.ClientOptions{
			Dsn:              cfg.SentryDSN,
			Environment:      cfg.Env,
			TracesSampleRate: 0.1,
		}
		// Release correlates Sentry events to a specific deployed revision.
		// Prefer the build-time ldflag `version` (set by build-bff action.yml
		// via -ldflags "-X main.version=<tag>").  Fall back to GIT_COMMIT from
		// the env file (written by the deploy pipeline) for backward compat
		// with pre-PR-#2680 deployments and local dev builds.
		switch {
		case version != "" && version != "dev":
			sentryOpts.Release = version
		case cfg.GitCommit != "":
			sentryOpts.Release = cfg.GitCommit
		}
		if err := sentry.Init(sentryOpts); err != nil {
			log.Fatalf("sentry.Init: %v", err)
		}
		// Flush buffered events before the process exits.
		defer sentry.Flush(2 * time.Second)
		log.Println("Sentry initialised.")
	} else {
		log.Println("SENTRY_DSN not set — Sentry disabled (development mode only).")
	}

	// Wire the Clerk user ID context-key extractor for the erasure service.
	// MUST be called after Sentry init (C3) so that any Sentry alert from the
	// mount-gate or cascade has an active hub.  The extractor reads the Clerk
	// session claims that RequireClerkAuth stores in the request context via
	// clerk.ContextWithSessionClaims.
	erasure.SetClerkUserIDFromContextFn(func(ctx context.Context) (string, bool) {
		claims, ok := clerk.SessionClaimsFromContext(ctx)
		if !ok || claims == nil {
			return "", false
		}
		return claims.Subject, true
	})

	if cfg.DatabaseURL != "" {
		// Pre-flight: detect a rolled-back binary deployed against a DB that has
		// already been migrated further.  This uses a throwaway connection (the
		// shared pool is not open yet) and is fatal only when db > binary.
		// An unreachable DB or absent schema_migrations table is fail-open — the
		// retry loop in runMigrationsWithRetry will handle those cases.
		if err := storage.CheckBinaryAheadOfDB(cfg.DatabaseURL); err != nil {
			log.Fatalf("%v", err)
		}
		if err := runMigrationsWithRetry(cfg.DatabaseURL, 30*time.Second); err != nil {
			log.Fatalf("migrations failed: %v", err)
		}
	} else {
		log.Println("DATABASE_URL not set — skipping migrations (development mode only).")
	}

	fmt.Println("VaultMTG BFF")
	fmt.Println("==================")
	fmt.Printf("port: %d\n\n", *port)

	// Initialise PostHog server-side analytics.  The API key is read from
	// POSTHOG_API_KEY (sourced from SSM /vaultmtg/app/production/posthog-api-key
	// at deploy time).  When empty, PostHog is disabled — a no-op client is used
	// so all handler code paths are always exercised.  The key is never logged.
	//
	// The PostHog enqueuer is resolved here; the analytics.Client is built
	// after the DB pool is open so DBHaltChecker (#890) can be wired into both
	// branches.  When no DB is available (development), NoopHaltChecker is used.
	var phEnqueuer analytics.PostHogEnqueuer
	if cfg.PostHogAPIKey != "" {
		phClient, err := posthoglib.NewWithConfig(cfg.PostHogAPIKey, posthoglib.Config{
			Endpoint: cfg.PostHogHost,
		})
		if err != nil {
			log.Fatalf("posthog.NewWithConfig: %v", err)
		}
		defer func() {
			if err := phClient.Close(); err != nil {
				log.Printf("posthog close: %v", err)
			}
		}()
		phEnqueuer = phClient
		log.Println("PostHog initialised.")
	} else {
		phEnqueuer = analytics.NoopEnqueuer{}
		log.Println("POSTHOG_API_KEY not set — PostHog disabled (development mode only).")
	}
	// PII HASHING RULE — read before adding any new analytics property.
	//
	// Any property passed to analytics.Capture that contains human-identifiable
	// data (email address, display_name, mtga_screen_name, IP address, or similar)
	// MUST be hashed with identityhash.HashPII BEFORE being passed to Capture:
	//
	//   hashed := identityhash.HashPII(cfg.AnalyticsPIISalt, rawValue)
	//   ac.Capture(ctx, distinctID, eventName, map[string]any{"email_hash": hashed}, opts)
	//
	// DO NOT pass the raw value. The salt (cfg.AnalyticsPIISalt) is sourced from
	// SSM /vaultmtg/{env}/analytics-pii-salt (SecureString). Critical constraints:
	//   - Never log the salt value — it must stay in memory only.
	//   - Rotating the salt invalidates all existing hashes (PostHog person records
	//     will deduplicate differently against historical events). Rotation requires a
	//     coordinated PostHog person-merge or a clean break.
	//   - Use HashAccountID (not HashPII) for internal numeric account IDs — those
	//     are not human-identifiable and do not require a salt.
	//   - See services/bff/internal/identityhash/hash.go for the canonical implementation.
	//
	// Current HashPII call sites: boot_signal.go (ip_hash), account_profile.go (email, display_name).
	// No PII properties are emitted to PostHog today via analytics.Capture — when the first
	// one lands, add it to the list above and add a test verifying the raw value is never in
	// the captured properties.
	//
	// analyticsClient is finalised after the DB pool opens so DBHaltChecker
	// can be wired. Until then it uses NoopHaltChecker as a safe default.
	analyticsClient := analytics.NewClient(phEnqueuer, analytics.NewNoopHaltChecker())

	broker := sse.New()

	sseBroadcaster := &sseBroadcast{broker: broker}
	ingestHandler := handlers.NewIngestHandler(sseBroadcaster).WithAnalyticsClient(analyticsClient)

	// Wire Clerk auth middleware when CLERK_SECRET_KEY is configured.
	// This middleware protects browser-facing routes by verifying Clerk session
	// JWTs.  When the key is absent (e.g. development without a Clerk account)
	// the middleware is nil and callers fall back to the API-key path or serve
	// a 503.
	var clerkAuthMiddl func(http.Handler) http.Handler
	var clerkAuthSSEMiddl func(http.Handler) http.Handler
	var clerkOAuthMiddl func(http.Handler) http.Handler
	if cfg.ClerkSecretKey != "" {
		clerkAuthMiddl = bffmiddleware.RequireClerkAuth(cfg.ClerkSecretKey)
		// SSE middleware accepts the Clerk session cookie as a fallback token
		// source, because the browser EventSource API cannot set Authorization
		// headers.  See middleware.RequireClerkAuthForSSE for full design notes.
		clerkAuthSSEMiddl = bffmiddleware.RequireClerkAuthForSSE(cfg.ClerkSecretKey)
	} else {
		log.Println("CLERK_SECRET_KEY not set — Clerk JWT auth disabled (development only).")
	}
	// The daemon's PKCE flow returns a Clerk OAuth access token (jti prefix
	// "oat_") which is NOT a session JWT and is rejected by RequireClerkAuth.
	// RequireClerkOAuthToken validates these tokens via /oauth/userinfo on the
	// configured Frontend API host.
	if cfg.ClerkFrontendAPI != "" {
		clerkOAuthMiddl = bffmiddleware.RequireClerkOAuthToken(cfg.ClerkFrontendAPI)
	} else {
		log.Println("CLERK_FRONTEND_API not set — daemon OAuth-token auth disabled (development only).")
	}

	// Wire API key handler and auth middleware when a database is available.
	// sqlDB is declared here so it is in scope for NewHealthzHandler below.
	var (
		sqlDB                             *sql.DB
		apiKeysHandler                    *handlers.APIKeysHandler
		apiKeyAuthMiddl                   func(http.Handler) http.Handler
		daemonAPIKeyAuthMiddl             func(http.Handler) http.Handler
		clerkUserResolver                 func(http.Handler) http.Handler
		draftRatingsHandler               *handlers.DraftRatingsHandler
		historyHandler                    *handlers.HistoryHandler
		historySummaryHandler             *handlers.HistorySummaryHandler
		listV2Handler                     *handlers.ListV2Handler
		statsHandler                      *handlers.StatsHandler
		daemonHealthHandler               *handlers.DaemonHealthHandler
		daemonRegisterHandler             *handlers.DaemonRegisterHandler
		daemonsListHandler                *handlers.DaemonsListHandler
		daemonsRevokeHandler              *handlers.DaemonsRevokeHandler
		adminFleetHealthHandler           *handlers.AdminFleetHealthHandler
		adminProjectionErrorsCountHandler *handlers.AdminProjectionErrorsCountHandler
		adminDataFreshnessHandler         *handlers.AdminDataFreshnessHandler
		adminBackfillDraftSessionsHandler *handlers.AdminBackfillDraftSessionsHandler
		matchesHandler                    *handlers.MatchesHandler
		collectionHandler                 *handlers.CollectionHandler
		questsHandler                     *handlers.QuestsHandler
		standardHandler                   *handlers.StandardHandler
		gamePlaysHandler                  *handlers.GamePlaysHandler
		metaHandler                       *handlers.MetaHandler
		opponentsHandler                  *handlers.OpponentsHandler
		notesHandler                      *handlers.NotesHandler
		cardsHandler                      *handlers.CardsHandler
		decksHandler                      *handlers.DecksHandler
		draftsHandler                     *handlers.DraftsHandler
		mlHandler                         *handlers.MLHandler
		settingsHandler                   *handlers.SettingsHandler
		waitlistHandler                   *handlers.WaitlistHandler
		systemAccountHandler              *handlers.SystemAccountHandler
		wildcardRecommendationsHandler    *handlers.WildcardRecommendationsHandler
		consentHandler                    *handlers.ConsentHandler
		dataExportHandler                 *handlers.DataExportHandler
		collectionImportHandler           *handlers.CollectionImportHandler
		restrictionHandler                *handlers.RestrictionHandler
		adminRestrictionHandler           *handlers.AdminRestrictionHandler
		accountProfileHandler             *handlers.AccountProfileHandler
		accountDeletionHandler            *handlers.AccountDeletionHandler
		accountDeletionStatusHandler      *handlers.AccountDeletionStatusHandler
	)

	// erasureWG tracks in-flight erasure cascade goroutines.  The BFF shutdown
	// sequence waits for this group before exiting so no cascade is abandoned
	// mid-flight on SIGTERM.
	var erasureWG sync.WaitGroup

	// bffCtx is the BFF root context — used by long-lived background goroutines
	// (erasure cascade jobs) that must NOT be cancelled on SIGTERM.  They drain
	// via erasureWG before the process exits.
	bffCtx := context.Background()

	// projCtx is cancelled on SIGTERM so the projection worker exits cleanly.
	projCtx, projCancel := context.WithCancel(context.Background())

	if cfg.DatabaseURL != "" {
		var err error
		sqlDB, err = sql.Open("pgx", cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("open db: %v", err)
		}
		dbpool.Configure(sqlDB)
		pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer pingCancel()
		if err := sqlDB.PingContext(pingCtx); err != nil {
			log.Fatalf("db ping: %v", err)
		}

		// Wire the real DBHaltChecker now that the DB pool is open (#890).
		// This replaces the NoopHaltChecker wired at analytics.Client construction
		// time (before sqlDB was available).  Both the real-PostHog branch and the
		// NoopEnqueuer branch get the same DBHaltChecker so neither branch is a
		// restriction bypass (Ray's correction: replace BOTH NewNoopHaltChecker calls).
		analyticsClient = analytics.NewClient(phEnqueuer, repository.NewDBHaltChecker(sqlDB))
		// Re-wire the ingestHandler with the updated analyticsClient so it also
		// benefits from real halt checking (ingest is already constructed above with
		// the noop; .WithAnalyticsClient returns a new value, not a mutation).
		ingestHandler = ingestHandler.WithAnalyticsClient(analyticsClient)
		log.Println("Analytics DBHaltChecker (#890) wired.")

		apiKeyRepo := repository.NewAPIKeyRepository(sqlDB)
		apiKeysHandler = handlers.NewAPIKeysHandler(apiKeyRepo)
		apiKeyAuthMiddl = bffmiddleware.APIKeyAuth(apiKeyRepo)

		draftRatingsRepo := repository.NewDraftRatingsRepository(sqlDB)
		draftRatingsHandler = handlers.NewDraftRatingsHandler(draftRatingsRepo, cfg)

		daemonEventsRepo := repository.NewDaemonEventsRepository(sqlDB)
		ingestHandler = ingestHandler.WithRepository(daemonEventsRepo)

		accountRepo := repository.NewAccountRepository(sqlDB)
		matchesRepo := repository.NewMatchesRepository(sqlDB)
		draftSessionsRepo := repository.NewDraftSessionsRepository(sqlDB)
		deckListRepo := repository.NewDeckListRepository(sqlDB)

		historyHandler = handlers.NewHistoryHandler(accountRepo, matchesRepo, draftSessionsRepo)
		historySummaryHandler = handlers.NewHistorySummaryHandler(accountRepo, repository.NewHistorySummaryRepository(sqlDB))

		// Phase 2 PR #1 — new /api/v1/matches surface (camelCase, full filter
		// support).  Replaces the SPA's daemonClient /matches calls.  See
		// docs/product/milestones/v0.3.1/daemon-local-api-phase2-audit.md.
		matchesHandler = handlers.NewMatchesHandler(matchesRepo, accountRepo)

		// Phase 2 PR #2 — /api/v1/collection surface (cards/stats/sets/value).
		// Replaces the SPA's daemonClient /collection calls.  See
		// .claude/plans/spa-route-migration.md.
		collectionRepo := repository.NewCollectionRepository(sqlDB)
		collectionHandler = handlers.NewCollectionHandler(collectionRepo, accountRepo)

		// #895 — POST /api/v1/collection/import (manual collection import, D3).
		// Resolves MTGA Arena export lines to arena_ids via set_cards, then
		// upserts into card_inventory using the existing UpsertDelta path.
		// Clerk-auth-guarded; session-derived account, no client id.
		cardSetResolver := repository.NewCardSetResolver(sqlDB)
		cardInventoryWriterForImport := repository.NewCardInventoryRepository(sqlDB)
		collectionImportHandler = handlers.NewCollectionImportHandler(
			cardSetResolver, cardInventoryWriterForImport, accountRepo,
		)

		// Phase 2 PR #3 — /api/v1/quests surface (active/history/wins/stats).
		// QuestRepository was previously write-only (projection worker writes);
		// PR #3 adds the read-side methods used here.
		questsHandler = handlers.NewQuestsHandler(repository.NewQuestRepository(sqlDB), accountRepo)

		// Phase 2 PR #4 — /api/v1/standard surface (sets/rotation/config/
		// validate/legality). Mixed scope: sets+config+legality are global,
		// rotation+affected-decks+validate are account-scoped via accountRepo.
		standardHandler = handlers.NewStandardHandler(repository.NewStandardRepository(sqlDB), accountRepo)

		// Phase 2 PR #5a — game-plays read surface. Routes mount under
		// /api/v1/matches/{matchId}/plays/* and /api/v1/gameplays/* per the
		// SPA contract (gameplays.ts). Backed by game_plays,
		// game_state_snapshots, and opponent_cards_observed.
		gamePlaysHandler = handlers.NewGamePlaysHandler(repository.NewGamePlaysRepository(sqlDB), accountRepo)

		// Phase 2 PR #5b — /api/v1/meta surface (archetypes/tier/cards from
		// mtgzone_*; deck-analysis / identify-archetype / insights / refresh
		// stubbed pending the archetype-matching + scrape pipeline).
		metaHandler = handlers.NewMetaHandler(repository.NewMetaRepository(sqlDB))

		// Phase 2 PR #6 — opponents + analytics + archetype-expected-cards.
		// Reads opponent_deck_profiles, matchup_statistics,
		// archetype_expected_cards, opponent_cards_observed.
		opponentsHandler = handlers.NewOpponentsHandler(repository.NewOpponentsRepository(sqlDB), accountRepo)

		// Phase 2 PR #7 — notes + suggestions surface (deck_notes CRUD,
		// matches.notes/rating column, ml_suggestions read+dismiss).
		// generate-suggestions stubbed pending the ML pipeline.
		notesHandler = handlers.NewNotesHandler(repository.NewNotesRepository(sqlDB), accountRepo)

		// Phase 2 PR #8 — /api/v1/cards/* surface (search/lookup/sets/
		// ratings/CFB/import). Mostly global catalog reads;
		// /collection-quantities + /search-with-collection are the two
		// account-scoped endpoints.
		cardsHandler = handlers.NewCardsHandler(repository.NewCardsRepository(sqlDB), accountRepo)

		// Phase 2 PR #9 — /api/v1/decks/* surface (CRUD, cards, tags,
		// permutations, import/export, library + STUBs for the deck-builder
		// + recommendation pipeline).
		decksHandler = handlers.NewDecksHandler(repository.NewDecksRepository(sqlDB), accountRepo).WithAnalyticsClient(analyticsClient)

		// Phase 2 PR #10 — /api/v1/drafts/* surface (sessions, picks,
		// stats, 17lands export, community comparison, temporal trends,
		// learning curve) plus the /decks/* and /feedback/* strays from
		// drafts.ts. Recommendation/grading endpoints stubbed pending
		// the ML pipeline.
		draftsHandler = handlers.NewDraftsHandler(repository.NewDraftsRepository(sqlDB), accountRepo)

		// Phase 2 PR #11 — /api/v1/ml-suggestions/* + /api/v1/ml/*
		// surface (list/generate/dismiss/apply, synergy report,
		// card synergies, combinations, play patterns, learned-data
		// wipe). Reuses NotesRepository for the suggestion list/dismiss
		// reads; the new MLRepository owns apply + synergy + play
		// patterns + scoped clear.
		mlHandler = handlers.NewMLHandler(
			repository.NewNotesRepository(sqlDB),
			repository.NewMLRepository(sqlDB),
			accountRepo,
		)

		// Phase 2 PR #12 — /api/v1/settings[/{key}] surface
		// (account-scoped JSONB key/value store; backs the SPA's
		// AppSettings + per-key getters/setters).
		settingsHandler = handlers.NewSettingsHandler(
			repository.NewSettingsRepository(sqlDB),
			accountRepo,
		)

		// ListV2Handler provides cursor-paginated v2 endpoints for matches,
		// drafts, decks, and collection (ADR-018).
		cardInventoryRepoV2 := repository.NewCardInventoryRepository(sqlDB)
		listV2Handler = handlers.NewListV2Handler(
			accountRepo, matchesRepo, draftSessionsRepo, deckListRepo, cardInventoryRepoV2,
		)

		daemonHealthHandler = handlers.NewDaemonHealthHandler(daemonEventsRepo)

		// SystemAccountHandler serves GET /api/v1/system/account — returns the
		// authenticated user's MTGA account row (name, wins, mastery, etc.).
		// Clerk-session-authenticated; fixes the SPA 404 introduced by PR #2063.
		systemAccountHandler = handlers.NewSystemAccountHandler(accountRepo)

		// DaemonRegisterHandler mints (or retrieves) a per-account API key for the
		// daemon PKCE registration flow.  Protected by RequireClerkOAuthToken — the
		// daemon calls this with the Clerk OAuth access token obtained via the PKCE
		// browser flow.  See ADR-020 §POST /api/v1/daemon/register Wire Format.
		daemonAPIKeyRepo := repository.NewDaemonAPIKeyRepository(sqlDB)
		userRepo := repository.NewUserRepository(sqlDB)
		daemonRegisterHandler = handlers.NewDaemonRegisterHandler(daemonAPIKeyRepo, userRepo).WithAnalyticsClient(analyticsClient)

		// daemonAPIKeyAuthMiddl protects daemon-facing routes (currently only
		// POST /api/v1/ingest/events).  It validates the api_key minted by
		// daemon_register against daemon_api_keys, resolves account_id (Clerk
		// user_id) → users.id (int64), and sets user_id on the request context
		// so the standard UserIDFromContext continues to work.
		daemonAPIKeyAuthMiddl = bffmiddleware.DaemonAPIKeyAuth(daemonAPIKeyRepo, userRepo)

		// ADR-031 §3 + §4: per-device list + soft-revoke endpoints. Both
		// are Clerk-session-authenticated (NOT daemon-api-key); the SPA's
		// Devices UI (#2632) is the primary consumer.
		daemonsListHandler = handlers.NewDaemonsListHandler(daemonAPIKeyRepo)
		daemonsRevokeHandler = handlers.NewDaemonsRevokeHandler(daemonAPIKeyRepo)

		// #2559 fleet-health admin endpoint — aggregate-only daemon counts
		// for ops dashboards. Protected by AdminTokenAuth (static Bearer
		// token from SSM /vaultmtg/app/production/bff-admin-token).
		adminFleetHealthHandler = handlers.NewAdminFleetHealthHandler(daemonAPIKeyRepo)

		// #239 projection-errors count admin endpoint — total DLQ row count
		// for operational visibility. Protected by AdminTokenAuth.
		projectionErrorsRepo := repository.NewProjectionErrorsRepository(sqlDB)
		adminProjectionErrorsCountHandler = handlers.NewAdminProjectionErrorsCountHandler(projectionErrorsRepo)

		// #402 data-freshness admin endpoint — confirms draft_card_ratings
		// (17Lands sync output) is current before ML feature flag flips.
		// Protected by AdminTokenAuth. Uses the same threshold as the
		// draft-ratings handler (DraftRatingsStalenessThresholdHours).
		adminDataFreshnessHandler = handlers.NewAdminDataFreshnessHandler(
			draftRatingsRepo,
			cfg.DraftRatingsStalenessThresholdHours,
		)

		// #1350 one-time backfill for stale in_progress draft_sessions rows.
		// Protected by AdminTokenMiddl. POST /api/v1/admin/ops/backfill-draft-sessions.
		adminBackfillDraftSessionsHandler = handlers.NewAdminBackfillDraftSessionsHandler(draftSessionsRepo)

		// StatsHandler provides deck performance, win-rate trend, and format
		// distribution analytics endpoints (issue #1513).
		statsRepo := repository.NewStatsRepository(sqlDB)
		statsHandler = handlers.NewStatsHandler(accountRepo, statsRepo, statsRepo, statsRepo).
			WithDraftAnalytics(statsRepo).
			WithRankProgression(statsRepo).
			WithResultBreakdown(statsRepo)

		// WaitlistHandler (Phase 1, ticket #121) — public POST /api/v1/waitlist.
		// Persists the email to the waitlist table and makes a best-effort
		// Mailchimp signup. No Clerk auth required. Rate limited per-IP at 5 req/h.
		// SSM parameters provisioned by Ray via ticket #122.
		waitlistRepo := repository.NewWaitlistRepository(sqlDB)
		var mailchimpClient handlers.MailchimpClient
		if cfg.MailchimpAPIKey != "" && cfg.MailchimpListID != "" {
			mc, mcErr := handlers.NewMailchimpHTTPClient(cfg.MailchimpAPIKey, cfg.MailchimpListID)
			if mcErr != nil {
				log.Printf("WARN: mailchimp client init failed: %v — waitlist will persist DB rows only", mcErr)
			} else {
				mailchimpClient = mc
			}
		} else {
			log.Println("MAILCHIMP_API_KEY or MAILCHIMP_LIST_ID not set — Mailchimp disabled for waitlist.")
		}
		// Any future PII property on the waitlist funnel event (e.g. email) is subject to the PII HASHING RULE above.
		// piiSalt: cfg.AnalyticsPIISalt (SSM /vaultmtg/{env}/analytics-pii-salt)
		// Used to omit email from log lines (#135). Empty in local dev → log omission
		// is always correct (email absent regardless of salt value — Ray Q4 ruling).
		waitlistHandler = handlers.NewWaitlistHandler(waitlistRepo, mailchimpClient, cfg.AnalyticsPIISalt).WithAnalyticsClient(analyticsClient)

		// Mailchimp waitlist reconciler (ticket #126) — retries failed
		// subscriptions on a 15-minute cadence. Only started when a Mailchimp
		// client is available (same guard as the handler). Uses projCtx so it
		// exits cleanly on SIGTERM. No separate WaitGroup: a partially-drained
		// batch retries on the next tick; Mailchimp PUT is idempotent.
		if mailchimpClient != nil {
			mcReconciler := reconciler.NewMailchimpReconciler(waitlistRepo, mailchimpClient)
			go mcReconciler.Run(projCtx)
		}

		// WildcardRecommendationsHandler — ADR-045 full implementation (ticket #420).
		// Joins inventory + card_inventory + set_cards + draft_card_ratings +
		// mtgzone_archetypes + mtgzone_archetype_cards per ADR-045 §2.
		// cardInventoryRepoV2 was already initialised above for ListV2Handler.
		wildcardGapRepo := repository.NewWildcardGapRepository(sqlDB)
		wildcardRecommendationsHandler = handlers.NewWildcardRecommendationsHandler(
			accountRepo,
			repository.NewInventoryRepository(sqlDB),
			cardInventoryRepoV2,
			draftRatingsRepo,
			repository.NewMetaRepository(sqlDB),
			wildcardGapRepo,
			wildcardGapRepo,
		)

		// ConsentHandler — POST /api/v1/account/consent (#885).
		// Records consent events (signup ToS/PP, COPPA gate, cookie opt-in/out,
		// install dialog) as append-only rows in the consent_log table.
		// Protected by composeClerkAuth — requires a valid Clerk session JWT.
		// tos_version and privacy_policy_version are server-canonical (from cfg);
		// client-supplied values are ignored (Ray Q2 ruling).
		consentHandler = handlers.NewConsentHandler(
			repository.NewConsentLogRepository(sqlDB),
			accountRepo,
			handlers.ConsentConfig{
				TOSVersion:           cfg.TOSVersion,
				PrivacyPolicyVersion: cfg.PrivacyPolicyVersion,
			},
		)

		// DataExportHandler — GET /api/v1/account/data-export (#886).
		// Synchronous GDPR Art.15 data export; rate-limited to 1 export/24h/user
		// via dsr_access_log table. Protected by composeClerkAuth.
		//
		// clerkFetcher is non-nil when CLERK_SECRET_KEY is set (production and
		// staging). When nil (local dev without Clerk), clerk_profile is omitted
		// from the export -- the Art.15 DB tables are still included.
		var clerkFetcher *repository.ClerkProfileFetcher
		if cfg.ClerkSecretKey != "" {
			clerkFetcher = repository.NewClerkProfileFetcher(
				clerkuser.NewClient(&clerk.ClientConfig{}),
			)
		}
		dataExportHandler = handlers.NewDataExportHandler(
			repository.NewDSRAccessLogRepository(sqlDB),
			repository.NewDataExportRepository(sqlDB, clerkFetcher),
			accountRepo,
		)

		// RestrictionHandler + AdminRestrictionHandler — GDPR Art.18 Right to
		// Restriction (#890). User-facing: POST/DELETE /api/v1/account/restrict-processing.
		// Admin-facing: POST/DELETE /admin/account/{userID}/restrict-processing.
		// Both write restriction_audit_log rows (actor='user'|'admin').
		restrictionRepo := repository.NewRestrictionRepository(sqlDB)
		restrictionHandler = handlers.NewRestrictionHandler(restrictionRepo, accountRepo)
		adminRestrictionHandler = handlers.NewAdminRestrictionHandler(restrictionRepo, accountRepo)

		// AccountProfileHandler — PATCH /api/v1/account/profile (#888).
		// GDPR Art.16 Right to Rectification: atomic audit log (PII-hashed, salted) +
		// users.email sync from Clerk-verified primary address (PR #3099 revision).
		//
		// rectifySvc: RectificationService wraps both writes in a single *sql.Tx so
		// that the audit INSERT and the users.email UPDATE are committed or rolled
		// back together (Bianca V1 BLOCK fix — a real shared transaction).
		// It also satisfies rectificationAuditWriter for the display_name path.
		//
		// piiSalt: reuses cfg.AnalyticsPIISalt (SSM /vaultmtg/{env}/analytics-pii-salt)
		// — no new SSM parameter (Bianca V2 fix: HashPII with existing salt).
		//
		// clerkFetcher: same *ClerkProfileFetcher used above for DataExportHandler.
		// When nil (local dev without CLERK_SECRET_KEY), email-change requests fail
		// closed with 500 rather than trusting the client body (Sarah F2 fix).
		rectifySvc := repository.NewRectificationService(sqlDB)
		accountProfileHandler = handlers.NewAccountProfileHandler(
			rectifySvc,
			rectifySvc,
			accountRepo,
			cfg.AnalyticsPIISalt,
			clerkFetcher,
		)

		// GDPR Art.17 — account deletion entry point (#887).
		//
		// buildAccountDeletionHandler constructs the erasure clients and applies the
		// mount-gate: if any client is a Noop in production/staging, the gate fires,
		// a Sentry alert is sent, and nil is returned (route stays 404).  In
		// development Noop clients are acceptable — the route is always mounted.
		//
		// The erasure Service is wired to bffCtx (not the request context) so
		// in-flight cascade goroutines survive the HTTP request lifecycle.
		deletionRepo := repository.NewDeletionRepository(sqlDB)
		accountDeletionHandler = buildAccountDeletionHandler(
			cfg,
			buildPostHogDeleter(cfg),
			buildMailchimpErasureClient(cfg),
			buildClerkAdminClient(cfg),
			buildEmailSender(bffCtx),
			bffCtx,
			deletionRepo,
			&erasureWG,
		)
		if accountDeletionHandler != nil {
			accountDeletionStatusHandler = handlers.NewAccountDeletionStatusHandler(deletionRepo)
		}

		// Wire Clerk→DB user ID bridge when both Clerk and a database are available.
		// userRepo was created above for daemonRegisterHandler/daemonAPIKeyAuthMiddl.
		clerkUserResolver = bffmiddleware.ClerkUserResolver(userRepo)

		// Start projection worker unless disabled by env var.
		if os.Getenv("BFF_PROJECTION_DISABLED") != "true" {
			cardInventoryRepo := repository.NewCardInventoryRepository(sqlDB)
			inventoryRepo := repository.NewInventoryRepository(sqlDB)
			questRepo := repository.NewQuestRepository(sqlDB)
			deckProjectorRepo := repository.NewDeckProjectorRepository(sqlDB)
			gamePlayRepo := repository.NewGamePlayRepository(sqlDB)
			projectionErrorsRepo := repository.NewProjectionErrorsRepository(sqlDB)
			gameEventCountersRepo := repository.NewGameEventCountersRepository(sqlDB)
			worker := projection.NewWorker(
				daemonEventsRepo,
				accountRepo,
				matchesRepo,
				draftSessionsRepo,
				cardInventoryRepo,
				inventoryRepo,
				questRepo,
				deckProjectorRepo,
				gamePlayRepo,
			)
			worker.WithCounterStore(gameEventCountersRepo)
			worker.WithCardPlayStore(gamePlayRepo)
			worker.WithGameRowWriter(gamePlayRepo)
			worker.WithDLQ(projectionErrorsRepo)
			// Wire draft → deck creation (ADR-051 deck linkage).
			decksRepo := repository.NewDecksRepository(sqlDB)
			worker.WithDraftDeckCreator(decksRepo)
			worker.WithDraftPickReader(draftSessionsRepo)
			worker.WithAnalyticsClient(analyticsClient)
			// Wire deck-summary (header-only) fan-out from inventory.updated (#1337).
			worker.WithDeckSummaryStore(deckProjectorRepo)
			// Wire mastery pass fan-out from inventory.updated (#1338).
			worker.WithMasteryStore(repository.NewAccountMasteryRepository(sqlDB))
			go worker.Run(projCtx)
		} else {
			log.Println("BFF_PROJECTION_DISABLED=true — projection worker not started.")
		}
	} else {
		log.Printf("WARN: no DATABASE_URL — API key auth unavailable (env=%s); guarded endpoints return 503", cfg.Env)
	}

	embeddedVersion := "unknown"
	if v, err := storage.EmbeddedMaxVersion(); err == nil {
		embeddedVersion = fmt.Sprintf("%d", v)
	}
	var healthzPinger handlers.Pinger
	if sqlDB != nil {
		healthzPinger = sqlDB
	}
	healthzHandler := handlers.NewHealthzHandler(cfg.Env, healthzPinger, embeddedVersion)

	// E2EUnguardedSSE is only honoured in development; in any other env the
	// flag is silently ignored so a misconfigured staging/prod box stays safe.
	e2eUnguardedSSE := cfg.Env == "development" && os.Getenv("BFF_E2E_UNGUARDED_SSE") == "true"
	if e2eUnguardedSSE {
		log.Println("WARN: BFF_E2E_UNGUARDED_SSE=true — SSE endpoint is unauthenticated (E2E mode only)")
	}

	// RequireInternalSvcAuth is always constructed regardless of env — the
	// middleware itself fails closed when the secret is empty so there is no
	// unauthenticated exposure. In production/staging cfg.InternalSvcSecret is
	// required by config.Load(); in development it may be empty.
	internalSvcAuthMiddl := bffmiddleware.RequireInternalSvcAuth(cfg.InternalSvcSecret)
	if cfg.InternalSvcSecret == "" {
		log.Println("INTERNAL_SVC_SECRET not set — /internal/v1/* routes fail closed (development only).")
	}

	// Refuse to start if the ingest handler is wired but its auth middleware is
	// not.  An unauthenticated ingest endpoint would allow event mis-attribution
	// across accounts (ticket #1332, root-cause: duplicate-accounts P0).
	// DaemonAPIKeyAuthMiddl is only non-nil when sqlDB is available, so this
	// guard catches startup misconfigurations before any request is served.
	if daemonAPIKeyAuthMiddl == nil {
		log.Fatal("FATAL: DaemonAPIKeyAuthMiddl is nil — refusing to start without ingest auth guard (ticket #1332)")
	}

	r := BuildRouter(cfg, RouterDeps{
		Broker:                            broker,
		IngestHandler:                     ingestHandler,
		APIKeysHandler:                    apiKeysHandler,
		DraftRatingsHandler:               draftRatingsHandler,
		HistoryHandler:                    historyHandler,
		HistorySummaryHandler:             historySummaryHandler,
		ListV2Handler:                     listV2Handler,
		StatsHandler:                      statsHandler,
		DaemonHealthHandler:               daemonHealthHandler,
		DaemonRegisterHandler:             daemonRegisterHandler,
		DaemonsListHandler:                daemonsListHandler,
		DaemonsRevokeHandler:              daemonsRevokeHandler,
		AdminFleetHealthHandler:           adminFleetHealthHandler,
		AdminProjectionErrorsCountHandler: adminProjectionErrorsCountHandler,
		AdminDataFreshnessHandler:         adminDataFreshnessHandler,
		AdminBackfillDraftSessionsHandler: adminBackfillDraftSessionsHandler,
		MatchesHandler:                    matchesHandler,
		CollectionHandler:                 collectionHandler,
		QuestsHandler:                     questsHandler,
		StandardHandler:                   standardHandler,
		GamePlaysHandler:                  gamePlaysHandler,
		MetaHandler:                       metaHandler,
		OpponentsHandler:                  opponentsHandler,
		NotesHandler:                      notesHandler,
		CardsHandler:                      cardsHandler,
		DecksHandler:                      decksHandler,
		DraftsHandler:                     draftsHandler,
		MLHandler:                         mlHandler,
		SettingsHandler:                   settingsHandler,
		WaitlistHandler:                   waitlistHandler,
		SystemAccountHandler:              systemAccountHandler,
		WildcardRecommendationsHandler:    wildcardRecommendationsHandler,
		ConsentHandler:                    consentHandler,
		DataExportHandler:                 dataExportHandler,
		CollectionImportHandler:           collectionImportHandler,
		RestrictionHandler:                restrictionHandler,
		AdminRestrictionHandler:           adminRestrictionHandler,
		AccountProfileHandler:             accountProfileHandler,
		AccountDeletionHandler:            accountDeletionHandler,
		AccountDeletionStatusHandler:      accountDeletionStatusHandler,
		HealthzHandler:                    healthzHandler,
		InternalSvcAuthMiddl:              internalSvcAuthMiddl,
		ClerkAuthMiddl:                    clerkAuthMiddl,
		ClerkAuthSSEMiddl:                 clerkAuthSSEMiddl,
		ClerkOAuthMiddl:                   clerkOAuthMiddl,
		ClerkUserResolver:                 clerkUserResolver,
		APIKeyAuthMiddl:                   apiKeyAuthMiddl,
		DaemonAPIKeyAuthMiddl:             daemonAPIKeyAuthMiddl,
		AdminTokenMiddl:                   bffmiddleware.AdminTokenAuth(cfg.BFFAdminToken),
		SentryMiddl:                       bffmiddleware.NewSentryMiddleware(),
		E2EUnguardedSSE:                   e2eUnguardedSSE,
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: r,
	}

	go func() {
		log.Printf("BFF listening on :%d", *port)

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down...")

	projCancel()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}

	// Drain in-flight erasure cascade goroutines before exit.  Each goroutine
	// increments erasureWG.Add(1) before launch and defers Done.  We wait here
	// so no cascade is abandoned mid-flight on SIGTERM.  The goroutines use
	// bffCtx (context.Background()) so they are not affected by the SIGTERM
	// signal; they run to natural completion.
	erasureWG.Wait()

	fmt.Println("BFF stopped.")
}

// RouterDeps holds all optional handlers and middleware that BuildRouter needs.
// Nil fields are treated as "not configured" and the corresponding routes are
// either omitted or served with a degraded response.
type RouterDeps struct {
	Broker              *sse.Broker
	IngestHandler       *handlers.IngestHandler
	APIKeysHandler      *handlers.APIKeysHandler
	DraftRatingsHandler *handlers.DraftRatingsHandler
	HistoryHandler      *handlers.HistoryHandler
	// HistorySummaryHandler serves GET /api/v1/history/summary — the HomePage
	// summary card (today/this_week/all_time stats + streak + last_match).
	// Protected by Clerk auth. See ticket #689.
	HistorySummaryHandler *handlers.HistorySummaryHandler
	// ListV2Handler serves the cursor-paginated v2 list endpoints (ADR-018).
	ListV2Handler *handlers.ListV2Handler
	// StatsHandler serves the analytics stats endpoints (issue #1513).
	StatsHandler        *handlers.StatsHandler
	DaemonHealthHandler *handlers.DaemonHealthHandler
	// DaemonRegisterHandler serves POST /v1/daemon/register — mints or retrieves
	// a per-account API key for the daemon PKCE registration flow (ADR-020).
	// Protected by RequireClerkAuth — the daemon sends its Clerk session JWT.
	DaemonRegisterHandler *handlers.DaemonRegisterHandler
	// DaemonsListHandler serves GET /api/v1/daemons — lists the caller's
	// active daemon registrations. Protected by Clerk session auth per
	// ADR-031 §4.
	DaemonsListHandler *handlers.DaemonsListHandler
	// DaemonsRevokeHandler serves DELETE /api/v1/daemons/{device_id} — soft-
	// deletes (revokes) the caller's daemon registration. Protected by
	// Clerk session auth per ADR-031 §3.
	DaemonsRevokeHandler *handlers.DaemonsRevokeHandler
	// AdminFleetHealthHandler serves GET /api/v1/admin/daemons/fleet-health —
	// aggregate daemon key counts for ops dashboards. Zero PII.
	// Protected by AdminTokenMiddl (static Bearer token from SSM).
	// Requires a non-nil DB (daemon_api_keys table).
	AdminFleetHealthHandler *handlers.AdminFleetHealthHandler
	// AdminProjectionErrorsCountHandler serves
	// GET /api/v1/admin/projection-errors/count — total DLQ row count.
	// Protected by AdminTokenMiddl. Requires a non-nil DB.
	AdminProjectionErrorsCountHandler *handlers.AdminProjectionErrorsCountHandler
	// AdminDataFreshnessHandler serves GET /api/v1/admin/data-freshness —
	// reports whether draft_card_ratings (the 17Lands sync output) is current.
	// Returns "fresh", "stale", or "no_data" with age_hours and threshold_hours.
	// Protected by AdminTokenMiddl. Satisfies ticket #402 data-freshness AC.
	AdminDataFreshnessHandler *handlers.AdminDataFreshnessHandler
	// AdminBackfillDraftSessionsHandler serves
	// POST /api/v1/admin/ops/backfill-draft-sessions — one-time idempotent
	// backfill that closes stale in_progress draft_sessions rows (#1350).
	// Protected by AdminTokenMiddl.
	AdminBackfillDraftSessionsHandler *handlers.AdminBackfillDraftSessionsHandler
	// MatchesHandler serves the Phase 2 /api/v1/matches/* surface that the
	// SPA's daemonClient previously hit. Protected by DaemonAPIKeyAuth.
	MatchesHandler *handlers.MatchesHandler
	// CollectionHandler serves the Phase 2 /api/v1/collection/* surface
	// (cards/stats/sets/value). Protected by DaemonAPIKeyAuth.
	CollectionHandler *handlers.CollectionHandler
	// QuestsHandler serves the Phase 2 /api/v1/quests/* surface
	// (active/history/wins/stats). Protected by DaemonAPIKeyAuth.
	QuestsHandler *handlers.QuestsHandler
	// StandardHandler serves the Phase 2 /api/v1/standard/* surface
	// (sets/rotation/config/validate/legality). Protected by DaemonAPIKeyAuth.
	StandardHandler *handlers.StandardHandler
	// GamePlaysHandler serves the Phase 2 in-game telemetry routes:
	// /api/v1/matches/{matchId}/plays/*, /opponent-cards, /snapshots
	// and /api/v1/gameplays/game/{gameId}. Protected by DaemonAPIKeyAuth.
	GamePlaysHandler *handlers.GamePlaysHandler
	// MetaHandler serves the Phase 2 /api/v1/meta/* surface
	// (archetypes/tier/cards from mtgzone_*; analysis/identify/insights/
	// refresh stubbed). Protected by DaemonAPIKeyAuth.
	MetaHandler *handlers.MetaHandler
	// OpponentsHandler serves the Phase 2 opponents + analytics +
	// archetype-expected-cards surface. Routes mount across four URL
	// prefixes (matches/{id}/opponent-analysis, opponents/decks,
	// analytics/matchups, analytics/opponent-history,
	// archetypes/{name}/expected-cards). Protected by DaemonAPIKeyAuth.
	OpponentsHandler *handlers.OpponentsHandler
	// NotesHandler serves the Phase 2 notes + suggestions surface
	// (deck-notes CRUD, match-notes GET/PUT, suggestions list/generate/
	// dismiss). Protected by DaemonAPIKeyAuth.
	NotesHandler *handlers.NotesHandler
	// CardsHandler serves the Phase 2 /api/v1/cards/* surface — card
	// metadata, sets, 17Lands ratings (with staleness + refresh stub),
	// CFB ratings (CRUD + arena-id linking), and the two account-scoped
	// collection-aware endpoints. Protected by DaemonAPIKeyAuth.
	CardsHandler *handlers.CardsHandler
	// DecksHandler serves the Phase 2 /api/v1/decks/* surface (CRUD,
	// cards, tags, permutations, import/export, plus STUBs for the
	// deck-builder + recommendation pipeline). Protected by
	// DaemonAPIKeyAuth.
	DecksHandler *handlers.DecksHandler
	// DraftsHandler serves the Phase 2 /api/v1/drafts/* surface
	// (sessions, picks, stats, 17lands export, community comparison,
	// trends, learning curve) + the /decks/* + /feedback/* strays.
	// Protected by DaemonAPIKeyAuth.
	DraftsHandler *handlers.DraftsHandler
	// MLHandler serves the Phase 2 ml-suggestions + synergy +
	// play-patterns surface. Reuses NotesRepository for the
	// list/generate/dismiss aliases; the new MLRepository owns
	// apply / synergy / play-patterns / account-scoped clear.
	// Protected by DaemonAPIKeyAuth.
	MLHandler *handlers.MLHandler
	// SettingsHandler serves the Phase 2 /api/v1/settings[/{key}]
	// surface. Account-scoped JSONB key/value store; backs the SPA's
	// AppSettings + per-key getters/setters. Protected by DaemonAPIKeyAuth.
	SettingsHandler *handlers.SettingsHandler
	// WaitlistHandler serves POST /api/v1/waitlist (tickets #121, #834).
	// Public endpoint — no Clerk auth required. Rate limited per-IP.
	// 200 OK + {"position": N} on new email; 409 Conflict on duplicate.
	WaitlistHandler *handlers.WaitlistHandler
	// SystemAccountHandler serves GET /api/v1/system/account.
	// Returns the authenticated user's MTGA account row wrapped in the
	// standard {"data": ...} envelope.  Fixes the SPA 404 from PR #2063.
	// Protected by ClerkAuthMiddl (or APIKeyAuthMiddl in the fallback branch).
	SystemAccountHandler *handlers.SystemAccountHandler
	// WildcardRecommendationsHandler serves GET /api/v1/recommendations/wildcards.
	// ADR-045 §6 (v0.3.7 scaffold): returns 501 with complete ADR-045 JSON shape.
	// Full implementation in v0.3.8 ticket #420.
	// Protected by composeClerkAuth — serves user-specific collection/inventory data.
	WildcardRecommendationsHandler *handlers.WildcardRecommendationsHandler
	// ConsentHandler serves POST /api/v1/account/consent (#885).
	// Records consent events (signup ToS/PP, COPPA gate, cookie opt-in/out,
	// install dialog) as append-only rows in consent_log.
	// Protected by composeClerkAuth.
	ConsentHandler *handlers.ConsentHandler
	// DataExportHandler serves GET /api/v1/account/data-export (#886).
	// Synchronous GDPR Art.15 Right of Access export; rate-limited 1/24h/user.
	// Protected by composeClerkAuth.
	DataExportHandler *handlers.DataExportHandler
	// CollectionImportHandler serves POST /api/v1/collection/import (#895).
	// Accepts a multipart/form-data file in the MTGA Arena export format,
	// resolves each row to an arena_id via set_cards, and upserts into
	// card_inventory via UpsertDelta. Clerk-auth-guarded (session-derived
	// account, no client id). S-07 §1A applies (lives in bff/cmd).
	CollectionImportHandler *handlers.CollectionImportHandler
	// RestrictionHandler serves GDPR Art.18 Right to Restriction endpoints (#890):
	//   POST   /api/v1/account/restrict-processing   — set restriction for caller.
	//   DELETE /api/v1/account/restrict-processing   — clear restriction for caller.
	// Protected by composeClerkAuth. Writes restriction_audit_log (actor='user').
	RestrictionHandler *handlers.RestrictionHandler
	// AdminRestrictionHandler serves GDPR Art.18 admin-token-gated endpoints (#890):
	//   POST   /admin/account/{userID}/restrict-processing
	//   DELETE /admin/account/{userID}/restrict-processing
	// Protected by AdminTokenMiddl. Writes restriction_audit_log (actor='admin').
	AdminRestrictionHandler *handlers.AdminRestrictionHandler
	// AccountProfileHandler serves PATCH /api/v1/account/profile (#888).
	// GDPR Art.16 Right to Rectification — writes a rectification audit-log row
	// (PII-hashed) and syncs users.email so the Art.17 erasure cascade reads
	// the correct address. date_of_birth_year is rejected with 400 (COPPA-gated).
	// Protected by composeClerkAuth.
	AccountProfileHandler *handlers.AccountProfileHandler
	// AccountDeletionHandler serves DELETE /api/v1/account (#887).
	// GDPR Art.17 Right to Erasure entry point.  Returns 202 Accepted with a
	// job_id.  When nil (mount-gate fired), the route returns 404.
	// Protected by composeClerkAuth.
	AccountDeletionHandler *handlers.AccountDeletionHandler
	// AccountDeletionStatusHandler serves GET /api/v1/account/deletion-status/{job_id}.
	// Returns pending/completed status for a deletion job scoped to the caller.
	// Protected by composeClerkAuth.
	AccountDeletionStatusHandler *handlers.AccountDeletionStatusHandler
	// HealthzHandler serves GET /healthz — intentionally public (no auth).
	HealthzHandler *handlers.HealthzHandler
	// InternalSvcAuthMiddl protects the /internal/v1/* route group with
	// HMAC-SHA256 service-to-service JWT verification (ADR-070).
	// Constructed from cfg.InternalSvcSecret by main.go; when nil (empty
	// secret or no DB) the middleware is RequireInternalSvcAuth("") which
	// fails closed — no request reaches an internal handler unauthenticated.
	InternalSvcAuthMiddl func(http.Handler) http.Handler
	ClerkAuthMiddl       func(http.Handler) http.Handler
	// ClerkAuthSSEMiddl is used exclusively for GET /api/v1/events.  It accepts
	// the Clerk session cookie as a fallback token source in addition to the
	// standard Authorization: Bearer header.  This is required because the
	// browser EventSource API cannot set custom request headers.
	// See middleware.RequireClerkAuthForSSE for the full design rationale.
	ClerkAuthSSEMiddl func(http.Handler) http.Handler
	// ClerkOAuthMiddl validates Clerk OAuth access tokens (jti "oat_*") via
	// /oauth/userinfo.  Used on POST /api/v1/daemon/register — the daemon
	// PKCE flow returns OAuth access tokens, not session JWTs.
	ClerkOAuthMiddl   func(http.Handler) http.Handler
	ClerkUserResolver func(http.Handler) http.Handler
	APIKeyAuthMiddl   func(http.Handler) http.Handler
	// DaemonAPIKeyAuthMiddl validates a daemon api_key against daemon_api_keys
	// and resolves the matching Clerk account_id → users.id (int64). Used on
	// POST /api/v1/ingest/events. The legacy APIKeyAuth path checks the
	// api_keys table which is not where daemon_register stores its keys.
	DaemonAPIKeyAuthMiddl func(http.Handler) http.Handler
	// AdminTokenMiddl protects GET /api/v1/admin/daemons/fleet-health with a
	// static high-entropy Bearer token (from SSM bff-admin-token). Uses
	// crypto/subtle.ConstantTimeCompare — never bcrypt.
	AdminTokenMiddl func(http.Handler) http.Handler
	// SentryMiddl is the Sentry panic/error capture middleware.  When non-nil
	// it is installed as the outermost middleware so it captures panics from
	// all downstream handlers.  Safe to omit in tests and development.
	SentryMiddl func(http.Handler) http.Handler
	// E2EUnguardedSSE removes auth from GET /api/v1/events when true.
	// Must only be set when MTGA_ENV=development (enforced in main).
	// Used exclusively by the CI pipeline E2E job (BFF_E2E_UNGUARDED_SSE=true).
	E2EUnguardedSSE bool
}

// composeClerkAuth chains the Clerk JWT verifier with the user resolver into a
// single middleware so SPA-facing route blocks can keep the
// `r.With(auth).Method(path, handler)` pattern they had under
// DaemonAPIKeyAuth.  The user resolver maps the (string) Clerk user_id from
// the JWT to the int64 users.id every downstream handler reads via
// UserIDFromContext.  Pass a nil userResolver in test contexts that have no
// database — Clerk verification still runs but UserIDFromContext returns 0.
func composeClerkAuth(authMiddl, userResolver func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		h := next
		if userResolver != nil {
			h = userResolver(h)
		}
		return authMiddl(h)
	}
}

// BuildRouter constructs and returns the chi router for the BFF service.
// It is a standalone function (not a method) so that tests can call it
// directly without spawning a real HTTP server.
func BuildRouter(cfg *config.Config, deps RouterDeps) http.Handler {
	r := chi.NewRouter()
	r.Use(chimiddleware.RequestID)
	r.Use(bffmiddleware.NewStructuredLogger(bffmiddleware.NewDefaultLogger()))
	// SentryMiddl is installed before chi's Recoverer so that panics are
	// captured by Sentry before being swallowed.  Repanic=true (set inside
	// NewSentryMiddleware) ensures chi.Recoverer still writes the 500 response.
	if deps.SentryMiddl != nil {
		r.Use(deps.SentryMiddl)
	}
	r.Use(chimiddleware.Recoverer)
	// AllowedOrigins is configured via the ALLOWED_ORIGINS environment variable
	// (comma-separated list).  See ADR-006 for the full connectivity design.
	// Defaults to localhost-only values when the variable is not set.
	//
	// AllowCredentials must be true so that browser EventSource connections
	// using withCredentials:true receive Access-Control-Allow-Credentials:true
	// on the streaming 200 response.  Without it the browser blocks the SSE
	// connection even when Access-Control-Allow-Origin is a specific origin.
	// AllowedOrigins must remain a list of exact origins (never "*") when
	// AllowCredentials is true — go-chi/cors enforces this at runtime.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-ID"},
		AllowCredentials: true,
	}))

	// ── Public routes ────────────────────────────────────────────────────────
	// These routes require no authentication.

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"bff"}`))
	})

	// GET /healthz — public health check used by staging deploy checks and uptime
	// monitors.  Returns env and migration status.  Intentionally unauthenticated.
	if deps.HealthzHandler != nil {
		r.Get("/healthz", deps.HealthzHandler.ServeHTTP)
	}

	// ── /internal/v1/* route group (ADR-070 — internal service-to-service auth) ─
	// All routes under this prefix are protected by RequireInternalSvcAuth.
	// nginx MUST deny /internal/ at the proxy layer (returns 403 before the
	// request reaches the BFF). This group is the BFF-side enforcement layer.
	//
	// InternalSvcAuthMiddl is constructed from cfg.InternalSvcSecret; when the
	// secret is empty (development) the middleware still runs but fails closed —
	// no unauthenticated request reaches any internal handler.
	{
		internalAuthMiddl := deps.InternalSvcAuthMiddl
		if internalAuthMiddl == nil {
			// Fallback: use an empty-secret instance which fails closed.
			internalAuthMiddl = bffmiddleware.RequireInternalSvcAuth("")
		}
		r.Route("/internal/v1", func(r chi.Router) {
			r.Use(internalAuthMiddl)
			// GET /internal/v1/health — service-to-service liveness probe.
			// Used by Lambdas to confirm BFF reachability before dispatching work.
			r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"ok","service":"bff-internal"}`))
			})
		})
	}

	// GET /api/v1/daemon/version — latest daemon version (no auth required).
	// The handler uses a live GitHub Releases API fetch (5-minute in-memory cache)
	// to return the latest daemon/v* release. Falls back to the static BFF config
	// (BFF_DAEMON_LATEST_VERSION) when the GitHub API is unreachable.
	daemonVersionHandler := handlers.NewDaemonVersionHandler(cfg)
	daemonVersionHandler.WithFetcher(handlers.NewReleaseFetcher(
		"https://api.github.com/repos/RdHamilton/hollowmark/releases",
		5*time.Minute,
		cfg.GitHubToken, // optional GitHub token (BFF_GITHUB_TOKEN); anonymous when empty
		nil,             // use default http.Client with 10s timeout
	))
	r.Get("/api/v1/daemon/version", daemonVersionHandler.GetDaemonVersion)

	// POST /api/v1/waitlist — waitlist signup (tickets #121, #834).
	// Intentionally public (no Clerk auth). Rate limited at 5 req/h per IP.
	// 200 OK on new email with {"position": N}; 409 Conflict on duplicate.
	if deps.WaitlistHandler != nil {
		r.Post("/api/v1/waitlist", deps.WaitlistHandler.Join)
	}

	// POST /api/v1/boot-signal — config-failure beacon receiver (ADR-077, ticket #1212).
	// Intentionally public (no Clerk auth — fires before config inits). CORS-simple
	// (sendBeacon with text/plain Blob — no preflight). Rate limited at 20 req/min per IP.
	// Returns 204 on all valid and over-limit paths; 400 on oversize body or invalid schema.
	// Never returns 429 (silent drop policy — fire-and-forget must not surface errors to
	// a booting SPA). Sink: structured CloudWatch log line only. No DB writes. See AC4–AC7.
	// Constructed inline — no external dependencies (no DB, no Mailchimp, no Clerk).
	bootSignalHandler := handlers.NewBootSignalHandler(cfg.AnalyticsPIISalt)
	r.Post("/api/v1/boot-signal", bootSignalHandler.Handle)

	// POST /api/v1/daemon/register — daemon PKCE registration (Clerk OAuth token required).
	// The daemon calls this immediately after completing the PKCE browser flow,
	// sending the Clerk OAuth access token as the Bearer token.  The handler
	// validates the token via Clerk's /oauth/userinfo, mints (or retrieves) a
	// per-account API key, and returns it in the response body.  This route
	// uses RequireClerkOAuthToken (NOT RequireClerkAuth) because the PKCE flow
	// returns OAuth access tokens (jti "oat_*") rather than session JWTs.
	// Mounted under /api/v1/ to match the rest of the daemon-facing API (events,
	// daemon/version) — nginx only forwards /api/v1/* to the BFF.
	// See ADR-020 §POST /api/v1/daemon/register Wire Format.
	if deps.DaemonRegisterHandler != nil {
		if deps.ClerkOAuthMiddl != nil {
			r.With(deps.ClerkOAuthMiddl).Post("/api/v1/daemon/register", deps.DaemonRegisterHandler.Register)
		} else {
			// CLERK_FRONTEND_API is not configured — mount the route fail-closed
			// (503) so the daemon receives a meaningful error instead of a generic
			// 404 that is indistinguishable from a routing mismatch.  A 404 causes
			// the daemon to log "auth error: BFF registration: BFF returned 404:
			// 404 page not found" with no indication that the route itself is
			// missing.  503 makes the misconfiguration immediately actionable.
			// Root cause of 2026-05-30 prod incident: CLERK_FRONTEND_API was not
			// provisioned to the env file by deploy-bff.yml / ec2-bootstrap.sh.
			log.Println("WARN: POST /api/v1/daemon/register degraded — CLERK_FRONTEND_API not configured; endpoint returns 503")
			r.Post("/api/v1/daemon/register", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"service unavailable: daemon registration not configured"}`))
			})
		}
	}

	// ── /api/v1/daemons surface (ADR-031 §3 + §4) ────────────────────────────
	// GET /api/v1/daemons              — list caller's active daemons.
	// DELETE /api/v1/daemons/{device_id} — soft-revoke caller's daemon.
	// Both are SPA-facing (consumed by the Devices UI #2632), so authn is
	// the user's Clerk session — NOT the daemon API key. Cross-tenancy is
	// SQL-enforced by the WHERE account_id = $caller clause inside the repo
	// methods; the handler simply forwards the authenticated Clerk user_id.
	if deps.DaemonsListHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Get("/api/v1/daemons", deps.DaemonsListHandler.List)
		} else {
			log.Println("WARN: GET /api/v1/daemons disabled — Clerk auth middleware not configured")
		}
	}
	if deps.DaemonsRevokeHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Delete("/api/v1/daemons/{device_id}", deps.DaemonsRevokeHandler.Revoke)
		} else {
			log.Println("WARN: DELETE /api/v1/daemons/{device_id} disabled — Clerk auth middleware not configured")
		}
	}

	// ── Admin routes (#2559) ─────────────────────────────────────────────────
	// GET /api/v1/admin/daemons/fleet-health — aggregate daemon counts for
	// ops dashboards. Zero PII. Protected by a static Bearer token stored in
	// SSM /vaultmtg/app/production/bff-admin-token (not Clerk, not daemon
	// api_key). AdminTokenMiddl uses crypto/subtle.ConstantTimeCompare.
	//
	// AdminTokenMiddl is always non-nil (constructed in main with cfg.BFFAdminToken
	// which may be empty); when the token is not configured the middleware rejects
	// all requests — the endpoint is mounted but fail-closed.
	adminMiddl := deps.AdminTokenMiddl
	if adminMiddl == nil {
		// Safety net: if AdminTokenMiddl was not wired, reject everything.
		adminMiddl = func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "admin token not configured", http.StatusUnauthorized)
			})
		}
	}

	if deps.AdminFleetHealthHandler != nil {
		r.With(adminMiddl).Get("/api/v1/admin/daemons/fleet-health", deps.AdminFleetHealthHandler.ServeHTTP)
	}

	// GET /api/v1/admin/projection-errors/count — total DLQ row count (#239).
	// Protected by the same static Bearer token middleware as fleet-health.
	if deps.AdminProjectionErrorsCountHandler != nil {
		r.With(adminMiddl).Get("/api/v1/admin/projection-errors/count", deps.AdminProjectionErrorsCountHandler.ServeHTTP)
	}

	// GET /api/v1/admin/data-freshness — 17Lands sync freshness check (#402).
	// Returns status ("fresh"/"stale"/"no_data"), max_cached_at, age_hours, and
	// threshold_hours so operators can confirm ML inputs are current before a
	// feature flag flip. Protected by the same AdminTokenMiddl.
	if deps.AdminDataFreshnessHandler != nil {
		r.With(adminMiddl).Get("/api/v1/admin/data-freshness", deps.AdminDataFreshnessHandler.ServeHTTP)
	}

	// POST /api/v1/admin/ops/backfill-draft-sessions — one-time idempotent
	// repair for stale in_progress draft_sessions rows (#1350). Must be
	// invoked manually once in prod after the #1344 PR-B fix ships. Safe to
	// re-run (returns 0 rows on subsequent calls).
	if deps.AdminBackfillDraftSessionsHandler != nil {
		r.With(adminMiddl).Post("/api/v1/admin/ops/backfill-draft-sessions", deps.AdminBackfillDraftSessionsHandler.ServeHTTP)
	}

	// ── Phase 2 — /api/v1/matches/* (camelCase API, full filter support) ─────
	// Replaces the SPA's daemonClient /matches calls.  Browser-facing, so
	// protected by Clerk session auth (not the daemon's machine credential):
	// the SPA holds a Clerk JWT, not the daemon's keychain api_key.  See
	// docs/product/milestones/v0.3.1/daemon-local-api-phase2-audit.md.
	if deps.MatchesHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			m := deps.MatchesHandler
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			// List + lookup
			r.With(auth).Post("/api/v1/matches", m.List)
			r.With(auth).Get("/api/v1/matches/{matchId}", m.Get)
			r.With(auth).Get("/api/v1/matches/{matchId}/games", m.Games)
			// Filter dropdowns
			r.With(auth).Get("/api/v1/matches/formats", m.Formats)
			r.With(auth).Get("/api/v1/matches/archetypes", m.Archetypes)
			// Aggregations
			r.With(auth).Post("/api/v1/matches/stats", m.Stats)
			r.With(auth).Post("/api/v1/matches/trends", m.Trends)
			r.With(auth).Post("/api/v1/matches/format-distribution", m.FormatDistribution)
			r.With(auth).Post("/api/v1/matches/performance-by-hour", m.PerformanceByHour)
			r.With(auth).Post("/api/v1/matches/matchup-matrix", m.MatchupMatrix)
			// Rank views
			r.With(auth).Get("/api/v1/matches/rank-progression/{format}", m.RankProgression)
			r.With(auth).Get("/api/v1/matches/rank-progression-timeline", m.RankProgressionTimeline)
			// Export
			r.With(auth).Get("/api/v1/matches/export", m.Export)
			// Compare
			r.With(auth).Post("/api/v1/matches/compare", m.Compare)
			r.With(auth).Post("/api/v1/matches/compare/formats", m.CompareFormats)
			r.With(auth).Post("/api/v1/matches/compare/decks", m.CompareDecks)
			r.With(auth).Post("/api/v1/matches/compare/time-periods", m.CompareTimePeriods)
		} else {
			log.Println("WARN: /api/v1/matches/* disabled — Clerk auth middleware not configured")
		}
	}

	// Phase 2 PR #2 — /api/v1/collection surface. Same auth model + envelope
	// contract as /api/v1/matches/*. Replaces the SPA's daemonClient
	// /collection calls (only the live wrappers — dead Wails-era functions
	// were dropped on the SPA side in this PR).
	if deps.CollectionHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			c := deps.CollectionHandler
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Post("/api/v1/collection", c.List)
			r.With(auth).Get("/api/v1/collection/stats", c.Stats)
			r.With(auth).Get("/api/v1/collection/sets", c.Sets)
			r.With(auth).Get("/api/v1/collection/value", c.Value)
		} else {
			log.Println("WARN: /api/v1/collection/* disabled — Clerk auth middleware not configured")
		}
	}

	// #895 — POST /api/v1/collection/import — manual collection import (D3).
	// Mounted inside the same Clerk-auth group as the read surface above.
	// Literal sub-path /import must be registered after the bare /collection
	// POST so chi's router resolves it before the catch-all.
	if deps.CollectionImportHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Post("/api/v1/collection/import", deps.CollectionImportHandler.Import)
		} else {
			log.Println("WARN: POST /api/v1/collection/import disabled — Clerk auth middleware not configured")
		}
	}

	// Phase 2 PR #3 — /api/v1/quests surface (active/history/wins/stats).
	// Same auth + envelope contract.
	if deps.QuestsHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			q := deps.QuestsHandler
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Get("/api/v1/quests/active", q.Active)
			r.With(auth).Get("/api/v1/quests/history", q.History)
			r.With(auth).Get("/api/v1/quests/wins/daily", q.DailyWins)
			r.With(auth).Get("/api/v1/quests/wins/weekly", q.WeeklyWins)
			r.With(auth).Get("/api/v1/quests/stats", q.Stats)
		} else {
			log.Println("WARN: /api/v1/quests/* disabled — Clerk auth middleware not configured")
		}
	}

	// Phase 2 PR #5a — game-plays / snapshots / opponent-cards routes that
	// extend the matches surface, plus the dedicated /api/v1/gameplays/game
	// path. Same auth + envelope contract.
	if deps.GamePlaysHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			gp := deps.GamePlaysHandler
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Get("/api/v1/matches/{matchId}/plays", gp.MatchPlays)
			r.With(auth).Get("/api/v1/matches/{matchId}/plays/timeline", gp.MatchTimeline)
			r.With(auth).Get("/api/v1/matches/{matchId}/plays/summary", gp.MatchPlaySummary)
			r.With(auth).Get("/api/v1/matches/{matchId}/opponent-cards", gp.MatchOpponentCards)
			r.With(auth).Get("/api/v1/matches/{matchId}/snapshots", gp.MatchSnapshots)
			r.With(auth).Get("/api/v1/gameplays/game/{gameId}", gp.PlaysByGame)
		} else {
			log.Println("WARN: gameplays routes disabled — Clerk auth middleware not configured")
		}
	}

	// Phase 2 PR #10 — /api/v1/drafts/* surface. Plus the /decks/* and
	// /feedback/* strays from drafts.ts. Recommendation + grading
	// endpoints are documented STUBs.
	if deps.DraftsHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			d := deps.DraftsHandler
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			// Sessions / lists / lookups
			r.With(auth).Post("/api/v1/drafts", d.List)
			r.With(auth).Get("/api/v1/drafts/formats", d.Formats)
			r.With(auth).Get("/api/v1/drafts/recent", d.Recent)
			r.With(auth).Get("/api/v1/drafts/exportable", d.Exportable)
			// Stats / community / trends / learning curve
			r.With(auth).Post("/api/v1/drafts/stats", d.Stats)
			r.With(auth).Get("/api/v1/drafts/community-comparison/{setCode}", d.CommunityComparisonByGet)
			r.With(auth).Post("/api/v1/drafts/community-comparison", d.CommunityComparisonByPost)
			r.With(auth).Get("/api/v1/drafts/community-comparison", d.AllCommunityComparisons)
			r.With(auth).Post("/api/v1/drafts/trends", d.Trends)
			r.With(auth).Get("/api/v1/drafts/learning-curve/{setCode}", d.LearningCurve)
			// STUBs (no session id). grade-pick + win-probability moved to
			// the daemon's localapi in PR #17b (live-state only the
			// daemon can serve); the SPA already targets daemonClient
			// for both since PR #1886.
			r.With(auth).Post("/api/v1/drafts/insights", d.Insights)
			r.With(auth).Post("/api/v1/drafts/recalculate-set-grades", d.RecalculateSetGrades)
			// Per-session: literal sub-paths first
			r.With(auth).Get("/api/v1/drafts/{sessionId}/picks", d.Picks)
			r.With(auth).Get("/api/v1/drafts/{sessionId}/pool", d.Pool)
			r.With(auth).Get("/api/v1/drafts/{sessionId}/curve", d.Curve)
			r.With(auth).Get("/api/v1/drafts/{sessionId}/colors", d.Colors)
			r.With(auth).Get("/api/v1/drafts/{sessionId}/analysis", d.DraftGrade)
			r.With(auth).Post("/api/v1/drafts/{sessionId}/analyze-picks", d.AnalyzeSessionPickQuality)
			// current-pack moved to the daemon's localapi in PR #17b —
			// the SPA already calls daemonClient for it since PR #1886.
			r.With(auth).Get("/api/v1/drafts/{sessionId}/deck-metrics", d.DeckMetrics)
			r.With(auth).Post("/api/v1/drafts/{sessionId}/calculate-prediction", d.CalculatePrediction)
			r.With(auth).Post("/api/v1/drafts/{sessionId}/calculate-grade", d.CalculateGrade)
			r.With(auth).Get("/api/v1/drafts/{sessionId}/export/17lands", d.Export17Lands)
			// Per-session catch-all GET last so literal sub-paths win.
			r.With(auth).Get("/api/v1/drafts/{sessionId}", d.Get)
			// /decks/* strays from drafts.ts
			r.With(auth).Post("/api/v1/decks/recommendations", d.DecksRecommendations)
			r.With(auth).Post("/api/v1/decks/explain-recommendation", d.DecksExplainRecommendation)
			r.With(auth).Post("/api/v1/decks/classify-draft-pool", d.DecksClassifyDraftPool)
			// /feedback/* strays from drafts.ts
			r.With(auth).Get("/api/v1/feedback/stats", d.FeedbackStats)
			r.With(auth).Post("/api/v1/feedback/recommendation", d.FeedbackRecommendation)
			r.With(auth).Post("/api/v1/feedback/action", d.FeedbackAction)
			r.With(auth).Post("/api/v1/feedback/outcome", d.FeedbackOutcome)
		} else {
			log.Println("WARN: /api/v1/drafts/* disabled — Clerk auth middleware not configured")
		}
	}

	// Phase 2 PR #9 — /api/v1/decks/* surface. CRUD + cards + tags +
	// permutations + import/export are real; deck-builder + recommendation
	// endpoints are documented STUBs pending the ML pipeline.
	if deps.DecksHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			d := deps.DecksHandler
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			// List + CRUD
			r.With(auth).Get("/api/v1/decks", d.List)
			r.With(auth).Post("/api/v1/decks", d.Create)
			// Library / by-tags / by-draft / archetypes (literal paths
			// before the {deckId} wildcard so chi prefers the static).
			r.With(auth).Post("/api/v1/decks/by-tags", d.ByTags)
			r.With(auth).Post("/api/v1/decks/library", d.Library)
			r.With(auth).Get("/api/v1/decks/by-draft/{draftEventId}", d.GetByDraftEvent)
			r.With(auth).Get("/api/v1/decks/archetypes", d.Archetypes)
			// Import / parse / suggest / analyze / generate / build-around
			r.With(auth).Post("/api/v1/decks/import", d.Import)
			r.With(auth).Post("/api/v1/decks/parse", d.Parse)
			r.With(auth).Post("/api/v1/decks/suggest", d.SuggestDecks)
			r.With(auth).Post("/api/v1/decks/analyze", d.AnalyzeDeck)
			r.With(auth).Post("/api/v1/decks/apply-suggestion", d.ApplySuggestion)
			r.With(auth).Post("/api/v1/decks/build-around", d.BuildAround)
			r.With(auth).Post("/api/v1/decks/build-around/suggest-next", d.BuildAroundSuggestNext)
			r.With(auth).Post("/api/v1/decks/generate", d.Generate)
			r.With(auth).Post("/api/v1/decks/suggested/export-content", d.SuggestedExportContent)
			// Permutations
			r.With(auth).Get("/api/v1/decks/{deckId}/permutations", d.ListPermutations)
			r.With(auth).Get("/api/v1/decks/{deckId}/permutations/current", d.CurrentPermutation)
			r.With(auth).Get("/api/v1/decks/{deckId}/permutations/{permutationId}", d.GetPermutation)
			r.With(auth).Get("/api/v1/decks/{deckId}/permutations/{fromPermutationId}/diff/{toPermutationId}", d.PermutationDiff)
			r.With(auth).Put("/api/v1/decks/{deckId}/permutations/{permutationId}/name", d.UpdatePermutationName)
			r.With(auth).Post("/api/v1/decks/{deckId}/permutations/{permutationId}/restore", d.RestorePermutation)
			// Cards
			r.With(auth).Post("/api/v1/decks/{deckId}/cards", d.AddCard)
			r.With(auth).Delete("/api/v1/decks/{deckId}/cards/{cardId}/all", d.RemoveAllCopies)
			r.With(auth).Delete("/api/v1/decks/{deckId}/cards/{cardId}", d.RemoveCard)
			// Tags
			r.With(auth).Post("/api/v1/decks/{deckId}/tags", d.AddTag)
			r.With(auth).Delete("/api/v1/decks/{deckId}/tags/{tag}", d.RemoveTag)
			// Per-deck stats / performance / classify / validate / clone /
			// recommendations / card-performance
			r.With(auth).Get("/api/v1/decks/{deckId}/stats", d.Stats)
			r.With(auth).Get("/api/v1/decks/{deckId}/curve", d.Stats)
			r.With(auth).Get("/api/v1/decks/{deckId}/colors", d.Stats)
			r.With(auth).Get("/api/v1/decks/{deckId}/statistics", d.Stats)
			r.With(auth).Get("/api/v1/decks/{deckId}/matches", d.Performance)
			r.With(auth).Get("/api/v1/decks/{deckId}/performance", d.Performance)
			r.With(auth).Get("/api/v1/decks/{deckId}/validate-draft", d.ValidateDraft)
			r.With(auth).Get("/api/v1/decks/{deckId}/classify", d.Classify)
			r.With(auth).Get("/api/v1/decks/{deckId}/card-performance", d.CardPerformance)
			r.With(auth).Get("/api/v1/decks/{deckId}/recommendations/add", d.AddRecommendations)
			r.With(auth).Get("/api/v1/decks/{deckId}/recommendations/remove", d.RemoveRecommendations)
			r.With(auth).Get("/api/v1/decks/{deckId}/recommendations/swap", d.SwapRecommendations)
			r.With(auth).Get("/api/v1/decks/{deckId}/recommendations/all", d.AllRecommendations)
			r.With(auth).Post("/api/v1/decks/{deckId}/clone", d.Clone)
			r.With(auth).Post("/api/v1/decks/{deckId}/export", d.Export)
			// Generic by-id GET/PUT/DELETE — mounted last so the literal
			// paths above win.
			r.With(auth).Get("/api/v1/decks/{deckId}", d.Get)
			r.With(auth).Put("/api/v1/decks/{deckId}", d.Update)
			r.With(auth).Delete("/api/v1/decks/{deckId}", d.Delete)
		} else {
			log.Println("WARN: /api/v1/decks/* disabled — Clerk auth middleware not configured")
		}
	}

	// Phase 2 PR #8 — /api/v1/cards/* surface. Mostly global catalog reads
	// (cards/sets/ratings/CFB); /collection-quantities and
	// /search-with-collection are the two account-scoped endpoints.
	if deps.CardsHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			c := deps.CardsHandler
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Get("/api/v1/cards", c.Search)
			r.With(auth).Get("/api/v1/cards/sets", c.AllSets)
			r.With(auth).Get("/api/v1/cards/sets/{setCode}/cards", c.SetCards)
			r.With(auth).Get("/api/v1/cards/ratings/{setCode}/colors", c.ColorRatings)
			r.With(auth).Get("/api/v1/cards/ratings/{setCode}/{format}/staleness", c.RatingsStaleness)
			r.With(auth).Get("/api/v1/cards/ratings/{setCode}/{format}", c.CardRatings)
			r.With(auth).Post("/api/v1/cards/ratings/{setCode}/refresh", c.RefreshRatings)
			r.With(auth).Post("/api/v1/cards/collection-quantities", c.CollectionQuantities)
			r.With(auth).Post("/api/v1/cards/search-with-collection", c.SearchWithCollection)
			r.With(auth).Get("/api/v1/cards/cfb/{setCode}/count", c.CFBRatingsCount)
			r.With(auth).Get("/api/v1/cards/cfb/{setCode}/card/{cardName}", c.CFBRatingByCard)
			r.With(auth).Get("/api/v1/cards/cfb/{setCode}", c.CFBRatings)
			r.With(auth).Post("/api/v1/cards/cfb/import", c.ImportCFB)
			r.With(auth).Post("/api/v1/cards/cfb/{setCode}/link-arena-ids", c.LinkCFBArenaIds)
			r.With(auth).Delete("/api/v1/cards/cfb/{setCode}", c.DeleteCFB)
			// Single-card lookup last so the literal /sets and /cfb prefixes win.
			r.With(auth).Get("/api/v1/cards/{arenaId}", c.GetByArenaID)
		} else {
			log.Println("WARN: /api/v1/cards/* disabled — Clerk auth middleware not configured")
		}
	}

	// POST /api/v1/account/consent — consent event recorder (#885).
	// Records consent events (signup ToS/PP, COPPA gate, cookie opt-in/out,
	// install dialog) as append-only rows in consent_log.
	// Protected by composeClerkAuth (Clerk session JWT required).
	// Returns 201 Created on success; SPA must block app entry until 201 returns.
	if deps.ConsentHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Post("/api/v1/account/consent", deps.ConsentHandler.RecordConsent)
		} else {
			log.Println("WARN: POST /api/v1/account/consent disabled — Clerk auth middleware not configured")
		}
	}

	// GET /api/v1/account/data-export — GDPR Art.15 data export (#886).
	// Synchronous export of all user-keyed personal data; rate-limited to 1
	// request per 24-hour window per user (returns 429 + Retry-After if exceeded).
	// Protected by composeClerkAuth (Clerk session JWT required).
	if deps.DataExportHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Get("/api/v1/account/data-export", deps.DataExportHandler.Export)
		} else {
			log.Println("WARN: GET /api/v1/account/data-export disabled — Clerk auth middleware not configured")
		}
	}

	// GDPR Art.18 Right to Restriction — user-facing endpoints (#890).
	//   POST   /api/v1/account/restrict-processing  — sets processing_restricted_at = NOW()
	//   DELETE /api/v1/account/restrict-processing  — clears processing_restricted_at = NULL
	// Both write a restriction_audit_log row with actor='user'.
	// Protected by composeClerkAuth (Clerk session JWT required).
	if deps.RestrictionHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Post("/api/v1/account/restrict-processing", deps.RestrictionHandler.SetRestriction)
			r.With(auth).Delete("/api/v1/account/restrict-processing", deps.RestrictionHandler.ClearRestriction)
		} else {
			log.Println("WARN: /api/v1/account/restrict-processing disabled — Clerk auth middleware not configured")
		}
	}

	// GDPR Art.18 Right to Restriction — admin-token-gated endpoints (#890).
	//   POST   /admin/account/{userID}/restrict-processing
	//   DELETE /admin/account/{userID}/restrict-processing
	// {userID} is the internal users.id (int64). Both write restriction_audit_log
	// with actor='admin'. Protected by AdminTokenMiddl (same static Bearer token
	// used by fleet-health and projection-errors admin endpoints).
	if deps.AdminRestrictionHandler != nil {
		r.With(adminMiddl).Post("/admin/account/{userID}/restrict-processing", deps.AdminRestrictionHandler.AdminSetRestriction)
		r.With(adminMiddl).Delete("/admin/account/{userID}/restrict-processing", deps.AdminRestrictionHandler.AdminClearRestriction)
	}

	// PATCH /api/v1/account/profile — GDPR Art.16 Right to Rectification (#888).
	// Writes a PII-hashed rectification audit-log row and syncs users.email so
	// the Art.17 erasure cascade reads the correct address (Ray Issue 1).
	// date_of_birth_year is rejected with 400 (COPPA-gated, Ray Issue 2).
	// Protected by composeClerkAuth (Clerk session JWT required).
	if deps.AccountProfileHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Patch("/api/v1/account/profile", deps.AccountProfileHandler.Patch)
		} else {
			log.Println("WARN: PATCH /api/v1/account/profile disabled — Clerk auth middleware not configured")
		}
	}

	// GDPR Art.17 Right to Erasure (#887).
	//
	// DELETE /api/v1/account — submit a deletion request (202 + job_id).
	// GET    /api/v1/account/deletion-status/{job_id} — poll cascade status.
	//
	// Both routes are mounted only when AccountDeletionHandler is non-nil.  A
	// nil AccountDeletionHandler means the mount-gate fired (a required erasure
	// client is Noop in production/staging) and the routes return 404 — this
	// is intentional fail-loud behaviour so an Art.17 request is never silently
	// accepted and then discarded.
	//
	// The mount-gate fires a Sentry alert inside buildAccountDeletionHandler so
	// the engineering team is notified if an SSM parameter is missing at deploy.
	if deps.AccountDeletionHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Delete("/api/v1/account", deps.AccountDeletionHandler.Delete)
			if deps.AccountDeletionStatusHandler != nil {
				r.With(auth).Get("/api/v1/account/deletion-status/{job_id}", deps.AccountDeletionStatusHandler.Status)
			}
		} else {
			log.Println("WARN: DELETE /api/v1/account disabled — Clerk auth middleware not configured")
		}
	}

	// ADR-045 §6 — wildcard recommendations scaffold (ticket #416, v0.3.7).
	// GET /api/v1/recommendations/wildcards — returns 501 stub with the complete
	// ADR-045 response shape. Full implementation in v0.3.8 ticket #420.
	// Clerk-auth-guarded: serves user-specific collection + inventory data.
	if deps.WildcardRecommendationsHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Get("/api/v1/recommendations/wildcards", deps.WildcardRecommendationsHandler.GetWildcardRecommendations)
		} else {
			log.Println("WARN: GET /api/v1/recommendations/wildcards disabled — Clerk auth middleware not configured")
		}
	}

	// Phase 2 PR #12 — /api/v1/settings[/{key}] surface
	// (account-scoped JSONB key/value store).
	if deps.SettingsHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			s := deps.SettingsHandler
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Get("/api/v1/settings", s.GetSettings)
			r.With(auth).Put("/api/v1/settings", s.UpdateSettings)
			r.With(auth).Get("/api/v1/settings/{key}", s.GetSetting)
			r.With(auth).Put("/api/v1/settings/{key}", s.UpdateSetting)
		} else {
			log.Println("WARN: /api/v1/settings/* disabled — Clerk auth middleware not configured")
		}
	}

	// Phase 2 PR #11 — ml-suggestions + synergy + play-patterns surface.
	// Mounts under /api/v1/ml-suggestions/*, /api/v1/decks/{id}/ml-suggestions,
	// /api/v1/decks/{id}/synergy-report, /api/v1/cards/{id}/synergies, and
	// /api/v1/ml/*. Three of the eleven routes are aliases for notes-side
	// list/generate/dismiss with the richer MLSuggestion response shape.
	if deps.MLHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			m := deps.MLHandler
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Get("/api/v1/decks/{deckId}/ml-suggestions", m.ListMLSuggestions)
			r.With(auth).Post("/api/v1/decks/{deckId}/ml-suggestions/generate", m.GenerateMLSuggestions)
			r.With(auth).Put("/api/v1/ml-suggestions/{suggestionId}/dismiss", m.DismissMLSuggestion)
			r.With(auth).Put("/api/v1/ml-suggestions/{suggestionId}/apply", m.ApplyMLSuggestion)
			r.With(auth).Get("/api/v1/decks/{deckId}/synergy-report", m.SynergyReport)
			r.With(auth).Get("/api/v1/cards/{cardId}/synergies", m.CardSynergies)
			r.With(auth).Get("/api/v1/ml/combinations", m.CombinationStats)
			r.With(auth).Post("/api/v1/ml/process-history", m.ProcessMatchHistory)
			r.With(auth).Get("/api/v1/ml/play-patterns", m.GetUserPlayPatterns)
			r.With(auth).Post("/api/v1/ml/play-patterns/update", m.UpdateUserPlayPatterns)
			r.With(auth).Delete("/api/v1/ml/learned-data", m.ClearLearnedData)
		} else {
			log.Println("WARN: ml-suggestions routes disabled — Clerk auth middleware not configured")
		}
	}

	// Phase 2 PR #7 — notes + suggestions surface (deck_notes CRUD,
	// matches.notes/rating column, ml_suggestions list/dismiss/stub-generate).
	if deps.NotesHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			n := deps.NotesHandler
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Get("/api/v1/decks/{deckId}/notes", n.ListDeckNotes)
			r.With(auth).Get("/api/v1/decks/{deckId}/notes/{noteId}", n.GetDeckNote)
			r.With(auth).Post("/api/v1/decks/{deckId}/notes", n.CreateDeckNote)
			r.With(auth).Put("/api/v1/decks/{deckId}/notes/{noteId}", n.UpdateDeckNote)
			r.With(auth).Delete("/api/v1/decks/{deckId}/notes/{noteId}", n.DeleteDeckNote)
			r.With(auth).Get("/api/v1/matches/{matchId}/notes", n.GetMatchNotes)
			r.With(auth).Put("/api/v1/matches/{matchId}/notes", n.UpdateMatchNotes)
			r.With(auth).Get("/api/v1/decks/{deckId}/suggestions", n.ListSuggestions)
			r.With(auth).Post("/api/v1/decks/{deckId}/suggestions/generate", n.GenerateSuggestions)
			r.With(auth).Put("/api/v1/suggestions/{suggestionId}/dismiss", n.DismissSuggestion)
		} else {
			log.Println("WARN: notes/suggestions routes disabled — Clerk auth middleware not configured")
		}
	}

	// Phase 2 PR #6 — opponents + analytics + archetype-expected-cards.
	// Mixed scope: match-bound and per-account routes are scoped via
	// matches.account_id; archetypes/{name}/expected-cards is global.
	if deps.OpponentsHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			o := deps.OpponentsHandler
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Get("/api/v1/matches/{matchId}/opponent-analysis", o.OpponentAnalysis)
			r.With(auth).Get("/api/v1/opponents/decks", o.ListDecks)
			r.With(auth).Get("/api/v1/analytics/matchups", o.MatchupStats)
			r.With(auth).Get("/api/v1/analytics/opponent-history", o.OpponentHistory)
			r.With(auth).Get("/api/v1/archetypes/{name}/expected-cards", o.ExpectedCardsByArchetype)
		} else {
			log.Println("WARN: opponents routes disabled — Clerk auth middleware not configured")
		}
	}

	// Phase 2 PR #5b — /api/v1/meta surface. Account-agnostic (meta data is
	// global), but still gated behind Clerk auth so anonymous callers can't
	// enumerate the archetype catalogue.
	if deps.MetaHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			m := deps.MetaHandler
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Get("/api/v1/meta/archetypes", m.Archetypes)
			r.With(auth).Get("/api/v1/meta/tier", m.Tier)
			r.With(auth).Get("/api/v1/meta/archetypes/cards", m.ArchetypeCards)
			r.With(auth).Get("/api/v1/meta/deck-analysis", m.DeckAnalysis)
			r.With(auth).Post("/api/v1/meta/identify-archetype", m.IdentifyArchetype)
			r.With(auth).Get("/api/v1/meta/insights", m.FormatInsights)
			r.With(auth).Post("/api/v1/meta/refresh", m.Refresh)
		} else {
			log.Println("WARN: /api/v1/meta/* disabled — Clerk auth middleware not configured")
		}
	}

	// Phase 2 PR #4 — /api/v1/standard surface. Mixed scope: sets / config /
	// card-legality are global; rotation / affected-decks / validate are
	// per-account. Same envelope + Clerk auth contract.
	if deps.StandardHandler != nil {
		if deps.ClerkAuthMiddl != nil {
			s := deps.StandardHandler
			auth := composeClerkAuth(deps.ClerkAuthMiddl, deps.ClerkUserResolver)
			r.With(auth).Get("/api/v1/standard/sets", s.Sets)
			r.With(auth).Get("/api/v1/standard/config", s.Config)
			r.With(auth).Get("/api/v1/standard/rotation", s.Rotation)
			r.With(auth).Get("/api/v1/standard/rotation/affected-decks", s.AffectedDecks)
			r.With(auth).Post("/api/v1/standard/validate/{deckId}", s.ValidateDeck)
			r.With(auth).Get("/api/v1/standard/cards/{arenaId}/legality", s.CardLegality)
		} else {
			log.Println("WARN: /api/v1/standard/* disabled — Clerk auth middleware not configured")
		}
	}

	// ── Daemon-facing routes (APIKey auth) ───────────────────────────────────
	// These routes are called by the local daemon binary, not the browser.
	// All daemon M2M routes are protected by APIKeyAuth — the legacy HMAC
	// DAEMON_JWT_SECRET path has been removed (see ADR-009 / issue #1315).

	// POST /api/keys — create a new API key for a user.
	// Protected by APIKeyAuth so user_id is derived from the verified key,
	// never from a caller-supplied header.
	if deps.APIKeysHandler != nil {
		if deps.APIKeyAuthMiddl != nil {
			r.With(deps.APIKeyAuthMiddl).Post("/api/keys", deps.APIKeysHandler.CreateAPIKey)
		} else {
			// No DB available — route omitted rather than serving unauthenticated.
			log.Println("WARN: POST /api/keys disabled — no database for API key auth")
		}
	}

	// POST /api/v1/ingest/events — daemon api_key auth required.
	// Uses DaemonAPIKeyAuth (not the legacy APIKeyAuth) because the
	// PKCE-minted daemon api_key lives in daemon_api_keys, not api_keys.
	// Mounted under /api/v1/ so nginx (which only forwards /api/v1/*) can reach it.
	//
	// Route is omitted entirely when DaemonAPIKeyAuthMiddl is nil — an
	// unauthenticated ingest endpoint is never acceptable (ticket #1332).
	// Startup validation in main() fatals before BuildRouter is reached, so
	// the nil branch here is a defence-in-depth guard, not a runtime path.
	if deps.IngestHandler != nil {
		if deps.DaemonAPIKeyAuthMiddl != nil {
			r.With(deps.DaemonAPIKeyAuthMiddl).Post("/api/v1/ingest/events", deps.IngestHandler.IngestEvent)
		} else {
			log.Println("WARN: POST /api/v1/ingest/events disabled — DaemonAPIKeyAuthMiddl not initialised")
		}
	}

	// ── Browser-facing protected routes (Clerk JWT auth) ─────────────────────
	// All routes below require a valid Clerk session JWT.
	// Auth priority:
	//   1. Clerk JWT (when CLERK_SECRET_KEY is set) — primary auth for browser clients.
	//   2. API-key fallback (when DATABASE_URL is set but CLERK_SECRET_KEY is not).
	//   3. 503 Service Unavailable — neither auth backend is configured.
	//
	// In production both CLERK_SECRET_KEY and DATABASE_URL are required by
	// config.Load(), so only the Clerk path is reachable in production.

	sseHandler := deps.Broker.Handler(bffmiddleware.UserIDFromContext)

	// sseClerkMiddl resolves to the cookie-aware SSE middleware when available,
	// falling back to the standard Bearer-only middleware.  Both verify the same
	// Clerk JWT — only the token transport differs.
	sseClerkMiddl := deps.ClerkAuthSSEMiddl
	if sseClerkMiddl == nil {
		sseClerkMiddl = deps.ClerkAuthMiddl
	}

	switch {
	case deps.ClerkAuthMiddl != nil:
		// GET /api/v1/events — SSE stream for browser clients.
		//
		// Mounted in its own group with the cookie-aware SSE middleware
		// (ClerkAuthSSEMiddl) instead of the standard ClerkAuthMiddl.  The
		// browser EventSource API cannot set custom Authorization headers, so
		// the SSE middleware also accepts the Clerk session cookie ("__session")
		// as a fallback token source.  All other Clerk-protected routes remain
		// in the Bearer-only group below.
		r.Group(func(r chi.Router) {
			r.Use(sseClerkMiddl)
			if deps.ClerkUserResolver != nil {
				r.Use(deps.ClerkUserResolver)
			}
			r.Get("/api/v1/events", sseHandler)
		})

		// Protected group — all non-SSE routes require a valid Clerk JWT via
		// the standard Authorization: Bearer header.
		r.Group(func(r chi.Router) {
			r.Use(deps.ClerkAuthMiddl)

			// ClerkUserResolver bridges the Clerk string user ID to the DB int64
			// user ID required by handlers.  When not configured (e.g. no DB in
			// development) the group still works but UserIDFromContext returns 0.
			if deps.ClerkUserResolver != nil {
				r.Use(deps.ClerkUserResolver)
			}

			// GET /api/v1/draft-ratings/{setCode}/{format} — draft card and color ratings.
			// Protected to prevent unauthenticated scraping and to scope future
			// per-user personalisation features.
			if deps.DraftRatingsHandler != nil {
				r.Get("/api/v1/draft-ratings/{setCode}/{format}", deps.DraftRatingsHandler.GetDraftRatings)
			}

			// ── Cloud history endpoints (Clerk-protected, Postgres-backed) ──────
			// These are NOT the desktop /api/v1/matches and /api/v1/drafts routes
			// (those are SQLite-backed in the desktop BFF and must not be touched).
			// Cloud history lives under /api/v1/history/ to make the split clear.
			if deps.HistoryHandler != nil {
				r.Get("/api/v1/history/matches", deps.HistoryHandler.GetMatches)
				r.Get("/api/v1/history/drafts", deps.HistoryHandler.GetDrafts)
			}
			if deps.HistorySummaryHandler != nil {
				r.Get("/api/v1/history/summary", deps.HistorySummaryHandler.GetSummary)
			}

			// ── v2 cursor-paginated list endpoints (ADR-018) ─────────────────
			// These replace the v1 offset-paginated list endpoints.  v1 routes
			// are kept as deprecation shims for one release (v0.4.0), then
			// removed in v0.4.1.
			if deps.ListV2Handler != nil {
				r.Get("/api/v2/history/matches", deps.ListV2Handler.GetMatches)
				r.Get("/api/v2/history/drafts", deps.ListV2Handler.GetDrafts)
				r.Get("/api/v2/decks", deps.ListV2Handler.GetDecks)
				r.Get("/api/v2/collection", deps.ListV2Handler.GetCollection)
				// /api/v1/collection is a v1 alias for the v2 collection endpoint.
				r.Get("/api/v1/collection", deps.ListV2Handler.GetCollection)
			}

			// ── Stats / analytics endpoints (issues #1513, #1514) ───────────
			if deps.StatsHandler != nil {
				r.Get("/api/v1/stats/deck-performance", deps.StatsHandler.GetDeckPerformance)
				r.Get("/api/v1/stats/win-rate-trend", deps.StatsHandler.GetWinRateTrend)
				r.Get("/api/v1/stats/format-distribution", deps.StatsHandler.GetFormatDistribution)
				r.Get("/api/v1/stats/draft-analytics", deps.StatsHandler.GetDraftAnalytics)
				r.Get("/api/v1/stats/rank-progression", deps.StatsHandler.GetRankProgression)
				r.Get("/api/v1/stats/result-breakdown", deps.StatsHandler.GetResultBreakdown)
			}

			// GET /api/v1/health/daemon — reports whether this user's daemon is
			// currently connected (last event received within 60 s).
			// Always 200; the response body carries the status.
			if deps.DaemonHealthHandler != nil {
				r.Get("/api/v1/health/daemon", deps.DaemonHealthHandler.GetDaemonHealth)
			}

			// GET /api/v1/system/account — returns the authenticated user's MTGA
			// account row (name, wins, mastery, etc.) wrapped in {"data": ...}.
			// Fixes the SPA 404 introduced by PR #2063 (closes #272).
			if deps.SystemAccountHandler != nil {
				r.Get("/api/v1/system/account", deps.SystemAccountHandler.GetSystemAccount)
			}
		})

	case deps.APIKeyAuthMiddl != nil:
		// Fallback: API-key auth when Clerk is not configured (non-production).
		r.With(deps.APIKeyAuthMiddl).Get("/api/v1/events", sseHandler)

		if deps.DraftRatingsHandler != nil {
			r.With(deps.APIKeyAuthMiddl).Get("/api/v1/draft-ratings/{setCode}/{format}", deps.DraftRatingsHandler.GetDraftRatings)
		}

		if deps.HistoryHandler != nil {
			r.With(deps.APIKeyAuthMiddl).Get("/api/v1/history/matches", deps.HistoryHandler.GetMatches)
			r.With(deps.APIKeyAuthMiddl).Get("/api/v1/history/drafts", deps.HistoryHandler.GetDrafts)
		}
		if deps.HistorySummaryHandler != nil {
			r.With(deps.APIKeyAuthMiddl).Get("/api/v1/history/summary", deps.HistorySummaryHandler.GetSummary)
		}

		if deps.ListV2Handler != nil {
			r.With(deps.APIKeyAuthMiddl).Get("/api/v2/history/matches", deps.ListV2Handler.GetMatches)
			r.With(deps.APIKeyAuthMiddl).Get("/api/v2/history/drafts", deps.ListV2Handler.GetDrafts)
			r.With(deps.APIKeyAuthMiddl).Get("/api/v2/decks", deps.ListV2Handler.GetDecks)
			r.With(deps.APIKeyAuthMiddl).Get("/api/v2/collection", deps.ListV2Handler.GetCollection)
			// /api/v1/collection is a v1 alias for the v2 collection endpoint.
			r.With(deps.APIKeyAuthMiddl).Get("/api/v1/collection", deps.ListV2Handler.GetCollection)
		}

		if deps.StatsHandler != nil {
			r.With(deps.APIKeyAuthMiddl).Get("/api/v1/stats/deck-performance", deps.StatsHandler.GetDeckPerformance)
			r.With(deps.APIKeyAuthMiddl).Get("/api/v1/stats/win-rate-trend", deps.StatsHandler.GetWinRateTrend)
			r.With(deps.APIKeyAuthMiddl).Get("/api/v1/stats/format-distribution", deps.StatsHandler.GetFormatDistribution)
			r.With(deps.APIKeyAuthMiddl).Get("/api/v1/stats/draft-analytics", deps.StatsHandler.GetDraftAnalytics)
			r.With(deps.APIKeyAuthMiddl).Get("/api/v1/stats/rank-progression", deps.StatsHandler.GetRankProgression)
			r.With(deps.APIKeyAuthMiddl).Get("/api/v1/stats/result-breakdown", deps.StatsHandler.GetResultBreakdown)
		}

		if deps.DaemonHealthHandler != nil {
			r.With(deps.APIKeyAuthMiddl).Get("/api/v1/health/daemon", deps.DaemonHealthHandler.GetDaemonHealth)
		}

		if deps.SystemAccountHandler != nil {
			r.With(deps.APIKeyAuthMiddl).Get("/api/v1/system/account", deps.SystemAccountHandler.GetSystemAccount)
		}

	default:
		if deps.E2EUnguardedSSE {
			// E2E pipeline mode: serve SSE without auth so pipeline log-fixture
			// tests can receive events.  Only reachable when MTGA_ENV=development
			// and BFF_E2E_UNGUARDED_SSE=true (enforced in main before BuildRouter).
			// Inject a sentinel user ID (1) so the SSE broker can subscribe the
			// connection — no real auth is performed in this mode.
			e2eSentinelMiddl := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					next.ServeHTTP(w, req.WithContext(bffmiddleware.WithUserID(req.Context(), 1)))
				})
			}
			r.With(e2eSentinelMiddl).Get("/api/v1/events", sseHandler)
		} else {
			// Neither auth backend is configured — serve 503 so the gap is visible.
			r.Get("/api/v1/events", func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "service unavailable — database not configured", http.StatusServiceUnavailable)
			})
		}

		if deps.DraftRatingsHandler != nil {
			r.Get("/api/v1/draft-ratings/{setCode}/{format}", func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "service unavailable — auth not configured", http.StatusServiceUnavailable)
			})
		}
	}

	return r
}

// sseBroadcast adapts the SSE Broker to the handlers.EventBroadcaster interface.
type sseBroadcast struct {
	broker *sse.Broker
}

func (b *sseBroadcast) BroadcastDaemonEvent(userID int64, event contract.DaemonEvent) {
	b.broker.Publish(userID, event)
}

// ── Erasure client constructors ───────────────────────────────────────────────

// buildPostHogDeleter returns a real PostHogHTTPClient when both
// POSTHOG_PERSONAL_API_KEY and POSTHOG_PROJECT_ID are configured; otherwise
// returns NoopPostHogDeleter.
func buildPostHogDeleter(cfg *config.Config) erasure.PostHogDeleter {
	if cfg.PostHogPersonalAPIKey != "" && cfg.PostHogProjectID != "" {
		client := erasure.NewPostHogHTTPClient(
			cfg.PostHogPersonalAPIKey,
			cfg.PostHogProjectID,
			cfg.PostHogHost,
		)
		log.Println("PostHog erasure client initialised.")
		return client
	}
	log.Println("POSTHOG_PERSONAL_API_KEY or POSTHOG_PROJECT_ID not set — PostHog erasure client disabled.")
	return erasure.NoopPostHogDeleter{}
}

// buildMailchimpErasureClient returns a real MailchimpErasureClient when both
// MAILCHIMP_API_KEY and MAILCHIMP_LIST_ID are configured; otherwise returns
// NoopMailchimpDeleter.
func buildMailchimpErasureClient(cfg *config.Config) erasure.MailchimpPermanentDeleter {
	if cfg.MailchimpAPIKey != "" && cfg.MailchimpListID != "" {
		mc, err := erasure.NewMailchimpErasureClient(cfg.MailchimpAPIKey, cfg.MailchimpListID)
		if err != nil {
			log.Printf("WARN: mailchimp erasure client init failed: %v — erasure Mailchimp step disabled", err)
			return erasure.NoopMailchimpDeleter{}
		}
		log.Println("Mailchimp erasure client initialised.")
		return mc
	}
	log.Println("MAILCHIMP_API_KEY or MAILCHIMP_LIST_ID not set — Mailchimp erasure client disabled.")
	return erasure.NoopMailchimpDeleter{}
}

// buildClerkAdminClient returns a real ClerkAdminClient when CLERK_SECRET_KEY
// is configured; otherwise returns NoopClerkDeleter.
func buildClerkAdminClient(cfg *config.Config) erasure.ClerkDeleter {
	if cfg.ClerkSecretKey != "" {
		client := erasure.NewClerkAdminClient(
			cfg.ClerkSecretKey,
			&http.Client{Timeout: 30 * time.Second},
		)
		log.Println("Clerk admin erasure client initialised.")
		return client
	}
	log.Println("CLERK_SECRET_KEY not set — Clerk admin erasure client disabled.")
	return erasure.NoopClerkDeleter{}
}

// buildAccountDeletionHandler constructs the erasure.Service and the
// AccountDeletionHandler, applying the mount-gate (C2).
//
// The mount-gate fires in production and staging when any required erasure
// client is a Noop (meaning its SSM parameter was not provisioned).  In that
// case buildAccountDeletionHandler:
//   - fires a Sentry alert so the team is notified immediately,
//   - returns nil so the route is NOT mounted (DELETE /api/v1/account → 404).
//
// In development the gate is skipped — Noop clients are acceptable and the
// route is always mounted for local testing.
//
// C2 compliance: the gate uses direct type assertions against the concrete Noop
// types (NoopPostHogDeleter, NoopMailchimpDeleter, NoopClerkDeleter).  The Noop
// types are value receivers implementing the interfaces — the assertions match
// the value form, not the pointer form, which is what all three constructors
// return.  The mount_gate_test.go test suite verifies both directions:
// gate fires when a client IS Noop; gate does NOT fire when clients are real.
// buildEmailSender constructs an SESv2Sender using the EC2 instance role
// (ADR-076).  No secret or API key is needed — SES uses the instance role
// which gains ses:SendEmail / ses:SendRawEmail via the IAM stack (ticket #1171
// AC5).  Returns nil when the AWS config cannot be loaded; the erasure cascade
// treats a nil Email sender as a no-op (email skipped, Sentry alert fires
// instead via deps.Reporter).
func buildEmailSender(ctx context.Context) email.Sender {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		log.Printf("WARN: SES email sender not available: LoadDefaultConfig: %v — account-deletion emails disabled", err)
		return nil
	}
	client := sesv2.NewFromConfig(awsCfg)
	log.Println("SES transactional email sender initialised.")
	return email.NewSESv2Sender(client)
}

func buildAccountDeletionHandler(
	cfg *config.Config,
	ph erasure.PostHogDeleter,
	mc erasure.MailchimpPermanentDeleter,
	ck erasure.ClerkDeleter,
	emailSender email.Sender,
	rootCtx context.Context,
	db *repository.DeletionRepository,
	wg *sync.WaitGroup,
) *handlers.AccountDeletionHandler {
	isProd := cfg.Env == "production" || cfg.Env == "staging"

	// C2: type assertions against the concrete Noop value types.
	// Value-receiver assertions (not pointer) — matches how all three
	// constructors return them.
	_, phIsNoop := ph.(erasure.NoopPostHogDeleter)
	_, mcIsNoop := mc.(erasure.NoopMailchimpDeleter)
	_, ckIsNoop := ck.(erasure.NoopClerkDeleter)

	if isProd && (phIsNoop || mcIsNoop || ckIsNoop) {
		// Fail-loud: alert and withhold the route so an Art.17 request is
		// never silently accepted and then not processed.
		errMsg := fmt.Sprintf(
			"[erasure mount-gate] DELETE /api/v1/account NOT mounted: "+
				"one or more erasure clients are Noop in %s "+
				"(posthog_noop=%v mailchimp_noop=%v clerk_noop=%v) — "+
				"check POSTHOG_PERSONAL_API_KEY, POSTHOG_PROJECT_ID, "+
				"MAILCHIMP_API_KEY, MAILCHIMP_LIST_ID, CLERK_SECRET_KEY in SSM",
			cfg.Env, phIsNoop, mcIsNoop, ckIsNoop,
		)
		log.Println(errMsg)
		observability.ReportError(context.Background(), fmt.Errorf("%s", errMsg))
		return nil
	}

	deps := erasure.Deps{
		DB:        db,
		PostHog:   ph,
		Mailchimp: mc,
		Clerk:     ck,
		Reporter:  observability.Reporter{},
		Email:     emailSender,
	}

	var erasureSvc *erasure.Service
	if db != nil {
		erasureSvc = erasure.NewService(rootCtx, db, deps, wg)
	}

	if erasureSvc == nil {
		// No DB — development mode without a database.
		log.Println("WARN: no database — AccountDeletionHandler not wired (development only).")
		return nil
	}

	// resolver uses the DeletionRepository's ResolveAllAccountIDs method (#1333).
	return handlers.NewAccountDeletionHandler(db, erasureSvc)
}
