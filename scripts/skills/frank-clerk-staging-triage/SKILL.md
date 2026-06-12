---
name: frank-clerk-staging-triage
description: Frank's staging-failure triage — when a staging CI failure produces a blank/dark screen or an explicit error screen on a Clerk-protected route, diagnose config-sidecar issues before writing any React code. ConfigErrorScreen visible → check /config.json fetch + SSM truth (FF-11). No error screen AND blank page AND pre-ADR-077 → check pk_test_ vs pk_live_ in the bundle.
user-invocable: false
---

# Staging Clerk / Config-Sidecar Triage

When staging deploys and the resulting page is blank, dark, or shows the `ConfigErrorScreen` on a Clerk-protected route, do NOT start debugging React first.

**ADR-077 (Accepted 2026-06-10, hollowmark#1208 shipped):** the SPA now fetches a per-environment `config.json` from its own CloudFront/S3 origin before any services or Clerk initialize. A misconfigured or missing sidecar produces a visible, deterministic `ConfigErrorScreen` — not a blank page. The first diagnostic is the sidecar, not the baked bundle.

## When to invoke

- Staging CI succeeded but the page is blank, dark, or shows an error screen on a Clerk-protected route
- `ConfigErrorScreen` is visible (any branch: "Could not reach VaultMTG" / "VaultMTG has a setup problem")
- Auth-protected routes show a blank or empty screen with no error surface
- Console error mentions Clerk init, publishable key, or config fetch

## Triage Tree

### Step 1 — Is the `ConfigErrorScreen` visible?

Look at the rendered page. The `ConfigErrorScreen` renders in all three failure branches before Clerk initializes:

- **"Could not reach VaultMTG"** — network branch (fetch threw or non-2xx)
- **"VaultMTG has a setup problem"** — parse branch or missing-fields branch

**Yes, error screen is visible → go to Step 2.**

**No error screen AND blank page → go to Step 4** (legacy blank-screen path or React-level issue).

---

### Step 2 — Identify the error branch (if error screen is visible)

The headline text maps directly to the branch:

| Headline | Branch | Root cause class |
|---|---|---|
| "Could not reach VaultMTG" | `network` | CloudFront or DNS issue; or `config.json` missing from S3 bucket |
| "VaultMTG has a setup problem" | `parse` or `missing-fields` | Bad or missing `config.json` at the edge |

**Network branch → Step 3A.**
**Parse or missing-fields branch → Step 3B.**

---

### Step 3A — Network branch: check CloudFront + DNS

The SPA fetch to `/config.json` threw a network error or got a non-2xx. This is a CloudFront, DNS, or missing-file-in-S3 issue.

```bash
# Confirm the distribution is serving the SPA at all
curl -sI "https://stg-app.vaultmtg.app/"

# Check if config.json path is reachable (should return JSON, not HTML)
curl -s "https://stg-app.vaultmtg.app/config.json" | head -5
```

If `curl /config.json` returns the SPA HTML (starts with `<!DOCTYPE html>`), the object is missing from the S3 bucket — CloudFront's 404→index.html rewrite served the shell instead. The deploy job did not write `config.json`. Check the deploy workflow run for the `write-config` / `upload-config` step.

If `curl` fails entirely (connection refused, NXDOMAIN), investigate CloudFront distribution health and Route 53 DNS.

**File a ticket via Pam (assigned to the deploy-pipeline owner — Bob/Bianca/Ben; infra-level issues to Ray). Do not write React code.**

---

### Step 3B — Parse or missing-fields branch: FF-11 check

The SPA received a `config.json` but it is malformed, HTML-masquerading-as-JSON (CloudFront 403/404→index.html passthrough with `response.ok === true`), or is missing required fields.

**Run the FF-11 check:**

```bash
# Fetch the live config.json with cache-busting
curl -s "https://stg-app.vaultmtg.app/config.json?$(date +%s)"
```

Compare `clerkPublishableKey` against SSM truth for this environment:

```bash
# Staging Clerk publishable key in SSM
aws ssm get-parameter \
  --name "/vaultmtg/staging/CLERK_PUBLISHABLE_KEY" \
  --profile personal \
  --query "Parameter.Value" \
  --output text
```

Also verify the Cache-Control header is correct:

```bash
curl -sI "https://stg-app.vaultmtg.app/config.json" | grep -i cache-control
# Expected: cache-control: no-cache, max-age=0
```

| Scenario | Verdict | Action |
|---|---|---|
| Response body is HTML (starts with `<!DOCTYPE html>`) | MISSING SIDECAR | The deploy job did not write `config.json` to `s3://vaultmtg-stg-app-spa/`. Check the workflow run's upload step. File ticket via Pam (Bob/Bianca/Ben). |
| `clerkPublishableKey` does not match `/^pk_(live|test)_[A-Za-z0-9]+$/` | FORMAT-INVALID KEY | The deploy wrote a malformed key — check SSM value at `/vaultmtg/staging/CLERK_PUBLISHABLE_KEY`. File ticket via Pam (Ray for key rotation; Bob/Bianca/Ben for deploy step). |
| `clerkPublishableKey` has valid format but does not match SSM truth | WRONG-ENV KEY | The deploy wrote the wrong environment's key. Verify SSM is correct. File ticket via Pam (Bob/Bianca/Ben deploy fix). |
| `config.json` is valid JSON but missing required fields (`bffUrl`, `sentryEnv`, `envLabel`, `daemonUrl`, `posthogHost`) | MISSING FIELDS | The deploy job is not writing all required fields. Check the config-render step in `deploy-spa.yml`. File ticket via Pam (Bob/Bianca/Ben). |
| `Cache-Control` is not `no-cache` | STALE CACHE | Per-object `--cache-control` flag missing on the `aws s3 cp` call. Invalidate `/*` immediately; file ticket via Pam (Bob/Bianca/Ben). |

**In all parse/missing-fields cases: STOP. Do not write React code. The fix is in the deploy pipeline or SSM — not in `frontend/`.**

---

### Step 4 — No error screen AND blank page

If there is no `ConfigErrorScreen` AND the page is blank, there are two sub-cases:

**Sub-case A: ADR-077 / #1208 has shipped on this environment.**

If the SPA boots to a blank page without showing `ConfigErrorScreen`, the config fetch succeeded (no error screen mounted), but Clerk or the app shell failed to initialize. This is an unexpected state — the config was valid but something downstream broke.

```bash
# Fetch config.json to confirm it loads and parses
curl -s "https://stg-app.vaultmtg.app/config.json?$(date +%s)" | python3 -m json.tool

# Check for Clerk initialization errors in the browser console (DevTools → Console)
```

If `clerkPublishableKey` in the served config is `pk_live_*` and well-formed, this is a real React or Clerk integration issue. Proceed with normal React/Playwright debugging — invoke the staging smoke spec to capture console errors, then debug the failing component.

**Sub-case B: ADR-077 / #1208 has NOT yet shipped on this environment.**

If the SPA predates the config-sidecar (before hollowmark#1208 merged to this environment's deploy), the blank-screen root cause may be the wrong Clerk publishable key baked into the bundle at build time. Run the legacy check:

```bash
# Find the deployed bundle URL
curl -s "https://stg-app.vaultmtg.app/" | grep -o 'src="/assets/[^"]*\.js"' | head -3

# Inspect the publishable key prefix in the bundle
curl -s "https://stg-app.vaultmtg.app/assets/<main-bundle>.js" | grep -o 'pk_[a-z_]*' | head -1
```

| Result | Verdict | Action |
|---|---|---|
| `pk_test_` | INFRASTRUCTURE BUG | **STOP.** The deployed bundle has the test key — a GitHub Actions staging environment misconfiguration. File a ticket via Pam (assigned to Ray) and escalate. Ray fixes the deploy env var in `.github/workflows/deploy-spa.yml` or the GitHub staging environment secrets. |
| `pk_live_` | REAL REACT BUG | The key is correct. Proceed with normal React/Playwright debugging. |
| No match | UNCLEAR | The bundle may be stale or the regex missed. Check `curl -I` headers for cache hits, refresh CloudFront if needed, then re-run. |

---

## Summary: what changed with ADR-077 (#1208)

| Before ADR-077 (pre-#1208) | After ADR-077 (#1208 shipped) |
|---|---|
| Blank screen = first check `VITE_CLERK_PUBLISHABLE_KEY` baked in bundle | Visible `ConfigErrorScreen` = first check `/config.json` fetch + SSM truth (FF-11) |
| Bundle contains per-env Clerk key | Bundle is env-agnostic; per-env key lives in `config.json` only |
| Wrong key → silent Clerk init failure → blank screen | Missing/bad sidecar → deterministic error screen (never blank) |
| Triage: grep bundle for `pk_test_` vs `pk_live_` | Triage: `curl /config.json`, compare against SSM, check deploy workflow |

## Why this matters

After ADR-077, the SPA is built once and promoted byte-identical. The Clerk publishable key is no longer baked into the bundle — it lives exclusively in the per-environment `config.json` served from the SPA's own CloudFront/S3 origin. The `ConfigErrorScreen` (`frontend/src/components/ConfigErrorScreen.tsx`) renders before Clerk initializes, making sidecar failures visible and diagnosable — not a silent blank page.

If the error screen is showing, the fix is infrastructure (deploy pipeline or SSM) — not React. The old blank-screen path still exists as a fallback for React-level failures, but it is no longer the first suspect.

The FF-11 post-deploy check (`curl /config.json` + SSM comparison against `/vaultmtg/<env>/clerk-publishable-key`) is the canonical tool for diagnosing any Clerk-blank-or-error-screen failure in the config-sidecar world.

## References

- ADR-077: `hollowmark-docs/engineering/architecture/adr/2026-06-ADR-077-spa-runtime-config-sidecar.md`
- SPA boot refactor: hollowmark-tickets#1208
- `ConfigErrorScreen` component: `frontend/src/components/ConfigErrorScreen.tsx`
- `runtimeConfig` loader: `frontend/src/config/runtimeConfig.ts`
- FF-11 (served sidecar matches env truth): ADR-077 §Fitness Functions
