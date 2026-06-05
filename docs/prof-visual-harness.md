# Prof Visual Capture Harness

Self-service authenticated screenshot tool for MTGA Enthusiast Consultant reviews.

**Tools needed:** Bash only. No local Node, no local credentials, no browser setup.

---

## What this does

The harness authenticates as the `ci-smoke-token` account against the live staging SPA (`stg-app.vaultmtg.app`), navigates to whichever player-facing surface you specify, performs the interaction you specify, and uploads screenshots as a downloadable CI artifact.

**Read-only guarantee:** the harness navigates and screenshots only. It performs no mutations — no crafting, no settings changes, no destructive actions.

Auth (`CLERK_SECRET_KEY`) is handled entirely in CI. You do not need any credentials locally.

---

## Step-by-step usage

### 1. Trigger the workflow

```bash
gh workflow run prof-visual-capture.yml \
  --repo RdHamilton/vault-mtg \
  -f route=wildcard-advisor \
  -f action=open-wildcard-advisor \
  -f label=wildcard-v1-review
```

Replace `route`, `action`, and `label` with your chosen values (see tables below).

`label` is a short hyphenated string that names the artifact and prefixes screenshot filenames. Choose something descriptive, e.g. `wildcard-v1-review`, `match-history-q2`, `draft-analytics-grade`.

### 2. Get the run ID

```bash
gh run list --repo RdHamilton/vault-mtg --workflow=prof-visual-capture.yml --limit=5
```

The most recent run is at the top. Note the run ID (a number in the first column).

### 3. Wait for completion

```bash
gh run watch <RUN_ID> --repo RdHamilton/vault-mtg
```

This streams live log output. The workflow takes roughly 3-5 minutes. Wait until you see a green checkmark or a failure banner.

Alternatively, poll without streaming:

```bash
gh run view <RUN_ID> --repo RdHamilton/vault-mtg --json status,conclusion -q '.status + " / " + .conclusion'
```

Repeat until it returns `completed / success`.

### 4. Download the artifact

```bash
gh run download <RUN_ID> \
  --repo RdHamilton/vault-mtg \
  --name prof-visual-capture-<YOUR_LABEL> \
  --dir ./prof-screenshots
```

Replace `<YOUR_LABEL>` with the exact label you passed in step 1.

Screenshots land in `./prof-screenshots/`. Each file is named `<label>-<suffix>.png`.

### 5. View the screenshots

```bash
ls ./prof-screenshots/
```

Then Read each PNG by its absolute path. Example:

```
Read /absolute/path/to/prof-screenshots/wildcard-v1-review-wildcard-advisor-open.png
```

---

## Supported routes

| `route` value | Surface visited |
|---|---|
| `collection` | `/collection` — base collection page |
| `wildcard-advisor` | `/collection` — wildcard advisor panel entry point |
| `match-history` | `/match-history` — full match history list |
| `match-details` | `/match-history` then clicks first row → match detail view |
| `draft-analytics` | `/draft-analytics` — draft session analytics |
| `home` | `/home` — dashboard |
| `decks` | `/decks` — deck list |
| `meta` | `/meta` — meta archetypes overview |
| `charts-win-rate` | `/charts/win-rate-trend` |
| `charts-deck-perf` | `/charts/deck-performance` |

---

## Supported actions

| `action` value | What it does |
|---|---|
| `screenshot-only` | Navigates to the route and screenshots with no further interaction |
| `open-wildcard-advisor` | Clicks "Show Wildcard Advisor" button on /collection, waits for panel + data to load |
| `expand-rec` | Opens wildcard advisor (if needed), then expands the first recommendation card to show the drill-down / GIHWR row |
| `switch-format-historic` | Opens wildcard advisor (if needed), then clicks the Historic format tab and waits for data |
| `switch-format-standard` | Opens wildcard advisor (if needed), then clicks the Standard format tab and waits for data |
| `grade-pill` | On `/draft-analytics`: waits for the draft grade pill to appear and screenshots it |
| `open-first-row` | Captures the list view, then clicks the first row to open the detail view and screenshots both |

---

## Route + action pairings (recommended)

| Goal | `route` | `action` |
|---|---|---|
| See the wildcard advisor panel loaded state | `wildcard-advisor` | `open-wildcard-advisor` |
| See a wildcard recommendation expanded | `wildcard-advisor` | `expand-rec` |
| See the wildcard advisor in Historic mode | `wildcard-advisor` | `switch-format-historic` |
| See the collection page baseline | `collection` | `screenshot-only` |
| See the match history list | `match-history` | `screenshot-only` |
| See a match detail view | `match-details` | `open-first-row` |
| See the draft grade pill | `draft-analytics` | `grade-pill` |
| See the home dashboard | `home` | `screenshot-only` |

---

## Screenshot filenames

Screenshots are named `<label>-<suffix>.png`. Suffixes per action:

| Action | Filename(s) |
|---|---|
| `screenshot-only` | `<label>-page.png` |
| `open-wildcard-advisor` | `<label>-wildcard-advisor-open.png` |
| `expand-rec` | `<label>-wildcard-advisor-open.png` + `<label>-wildcard-advisor-rec-expanded.png` |
| `switch-format-historic` | `<label>-wildcard-advisor-open.png` (if panel not yet open) + `<label>-wildcard-advisor-historic.png` |
| `switch-format-standard` | `<label>-wildcard-advisor-standard.png` |
| `grade-pill` | `<label>-draft-analytics-grade-pill.png` |
| `open-first-row` | `<label>-list-view.png` + `<label>-detail-view.png` |

---

## Empty states

The `ci-smoke-token` account has collection data (~10k cards, 3 Standard archetypes) but no match or draft session history on staging. Surfaces that depend on match or draft history will show authenticated empty states rather than populated data. Screenshots of empty states are valid — they document the UX for a new player.

If the wildcard advisor shows a sync-CTA (no MTGA sync yet), the harness captures that state and labels the file `<label>-wildcard-advisor-no-recs.png`.

---

## If the run fails

Check the Playwright failure report artifact:

```bash
gh run download <RUN_ID> \
  --repo RdHamilton/vault-mtg \
  --name playwright-report-prof-visual-<YOUR_LABEL> \
  --dir ./prof-playwright-report
```

Common causes:
- `INCONCLUSIVE: CLERK_SECRET_KEY is not set` — SSM read failed. Route to Tim.
- `Session not established — redirected to /sign-in` — Clerk session injection failed. Route to Tim.
- `Unknown PROF_ROUTE` or `Unknown PROF_ACTION` — typo in inputs. Retry with correct value.
- Timeout waiting for a data-testid — staging may be slow or the element changed. Route to Tim.

---

## Spec and workflow file locations

- Playwright spec: `frontend/tests/e2e/staging/prof-visual-capture.spec.ts`
- GHA workflow: `.github/workflows/prof-visual-capture.yml`
- Playwright config: `frontend/playwright.staging-spa.config.ts`
