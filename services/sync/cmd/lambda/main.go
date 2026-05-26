// Command mtga-sync-lambda is the AWS Lambda entrypoint for the mtga-sync service.
// AWS EventBridge Scheduler invokes this function on a configurable cron schedule.
//
// # Production (IAM auth — Lambda execution role)
//
// The Lambda connects to RDS using AWS IAM authentication as mandated by ADR-003.
// No static password is stored.  The execution role must have rds-db:connect
// permission for the mtga_sync DB user.  Required environment variables:
//
//	DB_HOST    RDS endpoint hostname
//	DB_NAME    PostgreSQL database name
//	DB_USER    PostgreSQL role name (mtga_sync)
//	DB_PORT    PostgreSQL port (default: 5432)
//	AWS_REGION AWS region of the RDS instance (e.g. us-east-1)
//
// # Local development (direct DSN — bypasses IAM)
//
// Set LAMBDA_LOCAL_DSN to a full PostgreSQL connection string.  When this
// variable is present the IAM token flow is skipped entirely.  Never set
// this in production Lambda environment variables.
//
//	LAMBDA_LOCAL_DSN  PostgreSQL DSN for local dev (e.g. postgres://user:pass@localhost/mtga)
//
// # Optional
//
//	SYNC_ACTIVE_SETS  Comma-separated set codes to refresh, e.g. "FDN,BLB,DSK"
//	                  When unset, active sets are queried from the database.
package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/RdHamilton/vault-mtg/services/sync/internal/datasets"
	"github.com/RdHamilton/vault-mtg/services/sync/internal/dbconn"
	"github.com/RdHamilton/vault-mtg/services/sync/internal/handler"
	"github.com/RdHamilton/vault-mtg/services/sync/internal/seventeenlands"
	awslambda "github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	ctx := context.Background()

	dsn, err := resolveDSN(ctx)
	if err != nil {
		log.Fatalf("resolve DB connection: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		log.Fatalf("open db pool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	store := datasets.NewPostgresStore(pool)
	client := seventeenlands.NewClient()
	h := handler.New(client, store, activeSets())

	awslambda.Start(h.Handle)
}

// resolveDSN returns the PostgreSQL DSN to use for this invocation.
//
//   - If LAMBDA_LOCAL_DSN is set, it is returned as-is (local dev only).
//   - Otherwise, a DSN is assembled from DB_HOST / DB_NAME / DB_USER / DB_PORT
//     plus a fresh RDS IAM auth token generated from the Lambda execution role.
//
// IAM tokens expire after 15 minutes.  Fetching once per invocation is safe:
// Lambda invocations are short-lived and the pool is not reused across invocations.
func resolveDSN(ctx context.Context) (string, error) {
	if localDSN := os.Getenv("LAMBDA_LOCAL_DSN"); localDSN != "" {
		log.Println("[sync] LAMBDA_LOCAL_DSN set — skipping IAM auth (local dev mode)")
		return localDSN, nil
	}

	cfg := dbconn.Config{
		Host:   os.Getenv("DB_HOST"),
		Port:   os.Getenv("DB_PORT"),
		DBName: os.Getenv("DB_NAME"),
		User:   os.Getenv("DB_USER"),
		Region: os.Getenv("AWS_REGION"),
	}

	// DIAGNOSTIC (vault-mtg-tickets#37 follow-up): surface the exact inputs
	// used to construct the IAM auth token so a stubborn PAM-auth failure can
	// be root-caused from CloudWatch logs without round-tripping a redeploy
	// per hypothesis. Remove after the live root cause is identified.
	log.Printf("[sync:diag] env DB_HOST=%q DB_PORT=%q DB_USER=%q DB_NAME=%q AWS_REGION=%q",
		cfg.Host, cfg.Port, cfg.DBName, cfg.User, cfg.Region)
	log.Printf("[sync:diag] env AWS_DEFAULT_REGION=%q AWS_LAMBDA_FUNCTION_NAME=%q AWS_EXECUTION_ENV=%q",
		os.Getenv("AWS_DEFAULT_REGION"),
		os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
		os.Getenv("AWS_EXECUTION_ENV"))

	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", err
	}

	log.Printf("[sync:diag] aws.Config Region=%q (LoadDefaultConfig)", awsCfg.Region)

	// Retrieve credentials once just for diagnostics so we can log the issuer
	// info without leaking secrets. The credential cache will satisfy the
	// subsequent Retrieve() call from inside BuildAuthToken.
	creds, credsErr := awsCfg.Credentials.Retrieve(ctx)
	if credsErr != nil {
		log.Printf("[sync:diag] creds.Retrieve error: %v", credsErr)
	} else {
		accessKeyPrefix := creds.AccessKeyID
		if len(accessKeyPrefix) > 4 {
			accessKeyPrefix = accessKeyPrefix[:4] + "..."
		}
		log.Printf("[sync:diag] creds Source=%q AccessKeyIDPrefix=%q HasSessionToken=%t CanExpire=%t Expires=%s",
			creds.Source, accessKeyPrefix, creds.SessionToken != "", creds.CanExpire, creds.Expires.UTC().Format("2006-01-02T15:04:05Z"))
	}

	dsn, err := dbconn.BuildDSN(ctx, cfg, awsCfg.Credentials, instrumentedTokenProvider)
	if err != nil {
		log.Printf("[sync:diag] BuildDSN error: %v", err)
		return "", err
	}

	return dsn, nil
}

// instrumentedTokenProvider wraps auth.BuildAuthToken with logging that
// surfaces the exact endpoint, region, dbUser passed to the signer and the
// generated token shape (host + query keys, never the signature). Used only
// while diagnosing vault-mtg-tickets#37 PAM auth failure.
func instrumentedTokenProvider(
	ctx context.Context, endpoint, region, dbUser string,
	creds aws.CredentialsProvider, optFns ...func(*auth.BuildAuthTokenOptions),
) (string, error) {
	log.Printf("[sync:diag] BuildAuthToken inputs endpoint=%q region=%q dbUser=%q",
		endpoint, region, dbUser)

	token, err := auth.BuildAuthToken(ctx, endpoint, region, dbUser, creds, optFns...)
	if err != nil {
		log.Printf("[sync:diag] BuildAuthToken error: %v", err)
		return token, err
	}

	// Token format: <host>:<port>/?Action=connect&DBUser=...&X-Amz-Algorithm=...
	// Log the host:port + query parameter NAMES so we can verify shape without
	// leaking the signature.
	if i := strings.IndexByte(token, '?'); i >= 0 {
		hostPart := token[:i]
		queryPart := token[i+1:]

		var paramNames []string
		for _, kv := range strings.Split(queryPart, "&") {
			if eq := strings.IndexByte(kv, '='); eq >= 0 {
				paramNames = append(paramNames, kv[:eq])
			} else {
				paramNames = append(paramNames, kv)
			}
		}
		log.Printf("[sync:diag] BuildAuthToken result host=%q len=%d params=%v",
			hostPart, len(token), paramNames)
	} else {
		log.Printf("[sync:diag] BuildAuthToken result UNEXPECTED_SHAPE len=%d", len(token))
	}

	return token, nil
}

// activeSets parses SYNC_ACTIVE_SETS and returns a non-nil slice when the env
// var is set, or nil to fall through to DB-driven active set resolution.
func activeSets() []string {
	v := os.Getenv("SYNC_ACTIVE_SETS")
	if v == "" {
		return nil
	}

	var sets []string

	for _, s := range strings.Split(v, ",") {
		if t := strings.TrimSpace(s); t != "" {
			sets = append(sets, t)
		}
	}

	return sets
}
