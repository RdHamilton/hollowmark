#!/bin/sh
# scripts/deploy/ssm-key-manifest.sh
#
# SINGLE SOURCE OF TRUTH for the BFF env-var <-> SSM parameter mapping.
# ADR-075 D3: converge prod+staging EC2 env provisioning on one manifest.
#
# This file is PURE DECLARATIVE DATA -- no functions, no executable logic,
# no aws CLI calls.  It is sourced by provision-staging-env.sh (staging path)
# and is readable by the FF-2 CI parity check (#1075).
#
# shellcheck disable=SC2034  # Variables used by sourcing scripts, not here.
#
# Column layout (one variable per line; no quoting needed for shell assignment):
#   MANIFEST_KEY_<N>_NAME      -- BFF env-var name written to the env file
#   MANIFEST_KEY_<N>_SSM_VAR   -- variable from deploy-env.sh holding the SSM
#                                  path (the FF-2 check resolves the path via
#                                  deploy-env.sh rather than hardcoding it here)
#   MANIFEST_KEY_<N>_TYPE      -- "plain" (String) or "secret" (SecureString;
#                                  requires --with-decryption on aws ssm)
#   MANIFEST_KEY_<N>_SCOPE     -- "both" | "staging-only" | "prod-only"
#
# MANIFEST_KEY_COUNT -- total number of entries (must equal the highest N+1).
#
# Annotation notes:
#   bootstrap-carried -- the key is written by ec2-bootstrap.sh and survives
#     every deploy because prod uses per-key upsert (no truncation).  The SSM
#     path variable is documented here for FF-2 set-parity purposes but this
#     key is NOT added to the manifest-driven deploy loop (Option B).
#   prod-only -- no staging PostHog (see deploy-env.sh SSM_PROD_POSTHOG_*
#     comment); staging omits these keys intentionally.
#   staging-only -- prod carries the equivalent key via the bootstrap-written
#     env file or via a separately-provisioned path not in scope here.
#
# Provisioning scripts that consume this manifest:
#   scripts/deploy/provision-staging-env.sh
#
# Do NOT add executable logic to this file.  If you need a helper, add it to
# scripts/deploy/provision-lib.sh.
#
# -----------------------------------------------------------------------

MANIFEST_KEY_COUNT=22

# 0 -- ALLOWED_ORIGINS
MANIFEST_KEY_0_NAME=ALLOWED_ORIGINS
MANIFEST_KEY_0_SSM_VAR=SSM_PROD_ALLOWED_ORIGINS
MANIFEST_KEY_0_TYPE=plain
MANIFEST_KEY_0_SCOPE=both

# 1 -- CLERK_SECRET_KEY
MANIFEST_KEY_1_NAME=CLERK_SECRET_KEY
MANIFEST_KEY_1_SSM_VAR=SSM_PROD_CLERK_SECRET_KEY
MANIFEST_KEY_1_TYPE=secret
MANIFEST_KEY_1_SCOPE=both

# 2 -- CLERK_FRONTEND_API
MANIFEST_KEY_2_NAME=CLERK_FRONTEND_API
MANIFEST_KEY_2_SSM_VAR=SSM_PROD_CLERK_FRONTEND_API
MANIFEST_KEY_2_TYPE=plain
MANIFEST_KEY_2_SCOPE=both

# 3 -- CLERK_PUBLISHABLE_KEY (staging-only: prod key served via SPA build env)
MANIFEST_KEY_3_NAME=CLERK_PUBLISHABLE_KEY
MANIFEST_KEY_3_SSM_VAR=SSM_STAGING_CLERK_PUBLISHABLE_KEY
MANIFEST_KEY_3_TYPE=plain
MANIFEST_KEY_3_SCOPE=staging-only

# 4 -- SENTRY_DSN
#   Path differs per env: prod uses SSM_PROD_SENTRY_DSN_BFF;
#   staging uses SSM_VAULTMTG_STAGING_SENTRY_DSN.
#   The SSM_VAR here names the PROD path; provision-staging-env.sh overrides
#   this entry's SSM_VAR to SSM_VAULTMTG_STAGING_SENTRY_DSN for the staging
#   loop (the only manifest entry requiring a per-env path override).
MANIFEST_KEY_4_NAME=SENTRY_DSN
MANIFEST_KEY_4_SSM_VAR=SSM_PROD_SENTRY_DSN_BFF
MANIFEST_KEY_4_TYPE=secret
MANIFEST_KEY_4_SCOPE=both

# 5 -- DAEMON_JWT_SECRET (bootstrap-carried on prod; in manifest for FF-2 parity)
#   On prod: written by ec2-bootstrap.sh; survives deploy via per-key upsert.
#   On staging: provisioned via SSM by provision-staging-env.sh.
#   Option B: NOT in the prod upsert loop; annotated here for FF-2 set-parity.
MANIFEST_KEY_5_NAME=DAEMON_JWT_SECRET
MANIFEST_KEY_5_SSM_VAR=SSM_PROD_DAEMON_JWT_SECRET
MANIFEST_KEY_5_TYPE=secret
MANIFEST_KEY_5_SCOPE=both

# 6 -- RESEND_API_KEY (staging-only)
MANIFEST_KEY_6_NAME=RESEND_API_KEY
MANIFEST_KEY_6_SSM_VAR=SSM_VAULTMTG_STAGING_RESEND_API_KEY
MANIFEST_KEY_6_TYPE=secret
MANIFEST_KEY_6_SCOPE=staging-only

# 7 -- DISCORD_BOT_TOKEN (staging-only)
MANIFEST_KEY_7_NAME=DISCORD_BOT_TOKEN
MANIFEST_KEY_7_SSM_VAR=SSM_VAULTMTG_STAGING_DISCORD_BOT_TOKEN
MANIFEST_KEY_7_TYPE=secret
MANIFEST_KEY_7_SCOPE=staging-only

# 8 -- DISCORD_GUILD_ID (staging-only)
MANIFEST_KEY_8_NAME=DISCORD_GUILD_ID
MANIFEST_KEY_8_SSM_VAR=SSM_VAULTMTG_STAGING_DISCORD_GUILD_ID
MANIFEST_KEY_8_TYPE=plain
MANIFEST_KEY_8_SCOPE=staging-only

# 9 -- MAILCHIMP_API_KEY (both: prod erasure cascade + staging; ticket #887)
#   Prod path: SSM_PROD_MAILCHIMP_API_KEY (/vaultmtg/app/production/mailchimp-api-key).
#   Staging path override: provision-staging-env.sh case statement overrides to
#   SSM_VAULTMTG_STAGING_MAILCHIMP_API_KEY (/vaultmtg/app/staging/mailchimp-api-key).
MANIFEST_KEY_9_NAME=MAILCHIMP_API_KEY
MANIFEST_KEY_9_SSM_VAR=SSM_PROD_MAILCHIMP_API_KEY
MANIFEST_KEY_9_TYPE=secret
MANIFEST_KEY_9_SCOPE=both

# 10 -- MAILCHIMP_LIST_ID (both: prod erasure cascade + staging; ticket #887)
#   Prod path: SSM_PROD_MAILCHIMP_LIST_ID (/vaultmtg/app/production/mailchimp-list-id).
#   Staging path override: provision-staging-env.sh case statement overrides to
#   SSM_VAULTMTG_STAGING_MAILCHIMP_LIST_ID (/vaultmtg/app/staging/mailchimp-list-id).
MANIFEST_KEY_10_NAME=MAILCHIMP_LIST_ID
MANIFEST_KEY_10_SSM_VAR=SSM_PROD_MAILCHIMP_LIST_ID
MANIFEST_KEY_10_TYPE=plain
MANIFEST_KEY_10_SCOPE=both

# 11 -- CRISP_WEBSITE_ID (staging-only)
MANIFEST_KEY_11_NAME=CRISP_WEBSITE_ID
MANIFEST_KEY_11_SSM_VAR=SSM_VAULTMTG_STAGING_CRISP_WEBSITE_ID
MANIFEST_KEY_11_TYPE=plain
MANIFEST_KEY_11_SCOPE=staging-only

# 12 -- POSTHOG_API_KEY (prod-only: no staging PostHog instance)
MANIFEST_KEY_12_NAME=POSTHOG_API_KEY
MANIFEST_KEY_12_SSM_VAR=SSM_PROD_POSTHOG_API_KEY
MANIFEST_KEY_12_TYPE=secret
MANIFEST_KEY_12_SCOPE=prod-only

# 13 -- POSTHOG_HOST (prod-only: no staging PostHog instance)
MANIFEST_KEY_13_NAME=POSTHOG_HOST
MANIFEST_KEY_13_SSM_VAR=SSM_PROD_POSTHOG_HOST
MANIFEST_KEY_13_TYPE=plain
MANIFEST_KEY_13_SCOPE=prod-only

# 14 -- BFF_DAEMON_LATEST_VERSION
MANIFEST_KEY_14_NAME=BFF_DAEMON_LATEST_VERSION
MANIFEST_KEY_14_SSM_VAR=SSM_PROD_BFF_DAEMON_LATEST_VERSION
MANIFEST_KEY_14_TYPE=plain
MANIFEST_KEY_14_SCOPE=both

# 15 -- BFF_DAEMON_RELEASED_AT
MANIFEST_KEY_15_NAME=BFF_DAEMON_RELEASED_AT
MANIFEST_KEY_15_SSM_VAR=SSM_PROD_BFF_DAEMON_RELEASED_AT
MANIFEST_KEY_15_TYPE=plain
MANIFEST_KEY_15_SCOPE=both

# 16 -- PORT (staging-only: prod PORT is set inline in the systemd unit)
MANIFEST_KEY_16_NAME=PORT
MANIFEST_KEY_16_SSM_VAR=SSM_STAGING_PORT
MANIFEST_KEY_16_TYPE=plain
MANIFEST_KEY_16_SCOPE=staging-only

# 17 -- DATABASE_URL (handled by write_database_url(); not a simple SSM entry)
#   Listed here for FF-2 set-parity only.  Provisioning scripts call
#   write_database_url() instead of the generic manifest loop for this entry.
MANIFEST_KEY_17_NAME=DATABASE_URL
MANIFEST_KEY_17_SSM_VAR=SSM_STAGING_DB_SECRET_ARN
MANIFEST_KEY_17_TYPE=secret
MANIFEST_KEY_17_SCOPE=both

# 18 -- ANALYTICS_PII_SALT (ticket #1597, PR #3094)
#   High-entropy secret used to hash user-identifiable analytics values.
#   Required at BFF startup -- missing value aborts config.Load().
#   Path differs per env: prod uses SSM_PROD_ANALYTICS_PII_SALT;
#   staging uses SSM_STAGING_ANALYTICS_PII_SALT.
#   The SSM_VAR here names the PROD path; provision-staging-env.sh overrides
#   this entry's SSM_VAR to SSM_STAGING_ANALYTICS_PII_SALT for the staging loop.
MANIFEST_KEY_18_NAME=ANALYTICS_PII_SALT
MANIFEST_KEY_18_SSM_VAR=SSM_PROD_ANALYTICS_PII_SALT
MANIFEST_KEY_18_TYPE=secret
MANIFEST_KEY_18_SCOPE=both

# 19 -- INTERNAL_SVC_SECRET (ADR-070, tickets #951/#952, PR #3121)
#   HMAC JWT signing secret for internal service-to-service auth.
#   Required at BFF startup in production and staging -- missing value aborts config.Load().
#   Path differs per env: prod uses SSM_PROD_INTERNAL_SVC_SECRET;
#   staging uses SSM_STAGING_INTERNAL_SVC_SECRET.
#   The SSM_VAR here names the PROD path; provision-staging-env.sh overrides
#   this entry's SSM_VAR to SSM_STAGING_INTERNAL_SVC_SECRET for the staging loop.
#   NOT bootstrap-carried (Option A): provisioned by the manifest-driven deploy loop
#   on both prod and staging.
MANIFEST_KEY_19_NAME=INTERNAL_SVC_SECRET
MANIFEST_KEY_19_SSM_VAR=SSM_PROD_INTERNAL_SVC_SECRET
MANIFEST_KEY_19_TYPE=secret
MANIFEST_KEY_19_SCOPE=both

# 20 -- POSTHOG_PERSONAL_API_KEY (ticket #887: GDPR Art.17 PostHog bulk-delete)
#   Prod path: SSM_PROD_POSTHOG_PERSONAL_API_KEY (/vaultmtg/app/production/posthog-personal-api-key).
#   Staging path override: provision-staging-env.sh case statement overrides to
#   SSM_STAGING_POSTHOG_PERSONAL_API_KEY (/vaultmtg/app/staging/posthog-personal-api-key).
#   SecureString: personal API key with bulk-delete scope.
#   DISTINCT from POSTHOG_API_KEY (project key used for analytics emits).
MANIFEST_KEY_20_NAME=POSTHOG_PERSONAL_API_KEY
MANIFEST_KEY_20_SSM_VAR=SSM_PROD_POSTHOG_PERSONAL_API_KEY
MANIFEST_KEY_20_TYPE=secret
MANIFEST_KEY_20_SCOPE=both

# 21 -- POSTHOG_PROJECT_ID (ticket #887: GDPR Art.17 PostHog bulk-delete)
#   Prod path: SSM_PROD_POSTHOG_PROJECT_ID (/vaultmtg/app/production/posthog-project-id).
#   Staging path override: provision-staging-env.sh case statement overrides to
#   SSM_STAGING_POSTHOG_PROJECT_ID (/vaultmtg/app/staging/posthog-project-id).
#   Plain String: numeric project ID (e.g. "12345").
MANIFEST_KEY_21_NAME=POSTHOG_PROJECT_ID
MANIFEST_KEY_21_SSM_VAR=SSM_PROD_POSTHOG_PROJECT_ID
MANIFEST_KEY_21_TYPE=plain
MANIFEST_KEY_21_SCOPE=both
