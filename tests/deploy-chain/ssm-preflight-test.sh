#!/usr/bin/env bash
# tests/deploy-chain/ssm-preflight-test.sh
#
# Deploy-chain SSM pre-flight integration test — verifies that the provision
# scripts FAIL loudly when required SSM parameters are missing or
# misconfigured, rather than silently writing a broken env file and allowing
# a deploy to proceed.
#
# Failure class guarded:
#   A deploy that reaches EC2 with one or more missing SSM parameters should
#   abort in the provision phase, not during BFF startup minutes later.
#   This test catches regressions where a new required SSM parameter is added
#   to the deploy-env.sh inventory but not yet created in Parameter Store, or
#   where an existing parameter is accidentally deleted.
#
# What is exercised:
#
#   provision-env.sh (missing SSM param)
#     A stub aws CLI returns exit 1 + ParameterNotFound error on the targeted
#     SSM call.  provision-env.sh uses `set -e`; the aws call failure MUST
#     propagate as a non-zero exit from the script.  The test asserts exit
#     code != 0 and that the env file does NOT contain the key.
#
#   provision-db-url.sh (missing SSM param for DB credentials)
#     Same pattern: stub returns ParameterNotFound for SSM_PROD_APP_DB_SECRET_ARN.
#     provision-db-url.sh has an explicit empty-value guard after the three SSM
#     reads; the test asserts exit code != 0 and DATABASE_URL NOT written.
#
#   provision-env.sh (positive-path regression guard)
#     After confirming the bad-path fails, re-run with a stub that returns
#     a value — asserts exit 0 and the key IS written to the env file.
#     A test that only exercises the failure case is not sufficient; the
#     positive path guard ensures the test would also catch a stub that
#     rejects all calls indiscriminately.
#
#   provision-env.sh --with-decryption (SecureString missing-param guard)
#     provision-env.sh is called with --with-decryption for SecureString
#     parameters (e.g. CLERK_SECRET_KEY).  If the aws call fails, the script
#     MUST still exit non-zero (the decryption-flag path must not be exempt).
#
# Stub pattern:
#   Identical to the Phase 1 stub in deploy-chain-integration-test.sh —
#   a minimal shell script at the front of PATH that intercepts aws calls
#   and returns canned responses.  Two stubs are written per scenario:
#   BAD_STUB (returns ParameterNotFound + exit 1) and GOOD_STUB (returns a
#   value + exit 0).
#
# Usage:
#   bash tests/deploy-chain/ssm-preflight-test.sh
#
# CI: triggered by .github/workflows/deploy-script-integration-test.yml
#     (same job that runs deploy-chain-integration-test.sh)

set -euo pipefail

# ---- colour helpers --------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

pass() { echo -e "${GREEN}PASS${NC}  $*"; }
fail() { echo -e "${RED}FAIL${NC}  $*"; exit 1; }
info() { echo -e "${YELLOW}INFO${NC}  $*"; }

# ---- repo root -------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# ---- scratch space ---------------------------------------------------------
SCRATCH="$(mktemp -d)"
cleanup() {
    rm -rf "$SCRATCH"
}
trap cleanup EXIT

info "Scratch dir: $SCRATCH"
info "Repo root  : $REPO_ROOT"

# ---------------------------------------------------------------------------
# Install a patched deploy-env.sh at /tmp/deploy-env.sh so the provision
# scripts redirect BFF_ENV_FILE / BFF_ENV_DIR into the scratch space (the
# real /etc/vaultmtg paths require root access and must not be touched).
# DB_PORT and DB_SSL_MODE are also patched to match the test environment.
# ---------------------------------------------------------------------------
STUB_ENV_DIR="${SCRATCH}/etc/vaultmtg"
STUB_ENV_FILE="${SCRATCH}/etc/vaultmtg/env"
mkdir -p "$STUB_ENV_DIR"

DEPLOY_ENV_STUB="/tmp/deploy-env.sh"
sed \
    -e "s|BFF_ENV_DIR=\"/etc/vaultmtg\"|BFF_ENV_DIR=\"${STUB_ENV_DIR}\"|" \
    -e "s|BFF_ENV_FILE=\"/etc/vaultmtg/env\"|BFF_ENV_FILE=\"${STUB_ENV_FILE}\"|" \
    -e 's|DB_PORT="5432"|DB_PORT="15432"|' \
    -e 's|DB_SSL_MODE="sslmode=require"|DB_SSL_MODE="sslmode=disable"|' \
    "${REPO_ROOT}/infra/config/deploy-env.sh" > "$DEPLOY_ENV_STUB"
info "Installed patched deploy-env.sh at $DEPLOY_ENV_STUB"

# ---------------------------------------------------------------------------
# BAD stub: every aws ssm get-parameter call returns ParameterNotFound (exit 1).
# Models the condition where a required SSM parameter does not exist in
# Parameter Store (e.g. newly-required param not yet created, or accidental
# deletion before a deploy).
# ---------------------------------------------------------------------------
BAD_STUB_DIR="${SCRATCH}/bin-bad"
mkdir -p "$BAD_STUB_DIR"
cat > "${BAD_STUB_DIR}/aws" <<'BADSTUB'
#!/usr/bin/env bash
# Bad-path stub: every ssm get-parameter returns ParameterNotFound (exit 1).
# Models a missing or deleted SSM parameter — the condition this test guards.
if [[ "$1" == "ssm" && "$2" == "get-parameter" ]]; then
    # Extract the parameter name for a useful error message.
    NAME=""
    while [[ $# -gt 0 ]]; do
        case "$1" in --name) NAME="$2"; shift 2 ;; *) shift ;; esac
    done
    echo "An error occurred (ParameterNotFound) when calling the GetParameter operation: Parameter ${NAME} not found." >&2
    exit 1
fi
echo "STUB(bad): unhandled aws command: $*" >&2
exit 1
BADSTUB
chmod +x "${BAD_STUB_DIR}/aws"

# ---------------------------------------------------------------------------
# GOOD stub: every aws ssm get-parameter call returns a stub value (exit 0).
# Models the healthy path — parameters are present in Parameter Store.
# ---------------------------------------------------------------------------
GOOD_STUB_DIR="${SCRATCH}/bin-good"
mkdir -p "$GOOD_STUB_DIR"
cat > "${GOOD_STUB_DIR}/aws" <<'GOODSTUB'
#!/usr/bin/env bash
# Good-path stub: ssm get-parameter returns a valid value (exit 0).
# The stub returns a canonical-looking value; the exact value does not matter
# for the positive-path guard — only that the call succeeds and the script
# writes the key to the env file.
if [[ "$1" == "ssm" && "$2" == "get-parameter" ]]; then
    echo "http://localhost:3000"
    exit 0
fi
echo "STUB(good): unhandled aws command: $*" >&2
exit 1
GOODSTUB
chmod +x "${GOOD_STUB_DIR}/aws"

# ===========================================================================
# Test A — provision-env.sh FAILS when the target SSM parameter is missing
#
# AC: when aws ssm get-parameter exits non-zero (ParameterNotFound),
# provision-env.sh MUST exit non-zero and MUST NOT write the key to the env
# file.  A deploy that silently proceeds with a missing parameter will crash
# at BFF startup — the provision phase must be the hard gate.
# ===========================================================================
info "Test A — provision-env.sh exits non-zero on ParameterNotFound..."

PROVISION_ENV_SCRIPT="${SCRATCH}/provision-env-test.sh"
cp "${REPO_ROOT}/scripts/deploy/provision-env.sh" "$PROVISION_ENV_SCRIPT"
chmod +x "$PROVISION_ENV_SCRIPT"

# Reset the env file — must not pre-exist for a clean assertion.
rm -f "$STUB_ENV_FILE"

# Run provision-env.sh with the bad stub at the front of PATH.
# Capture the exit code without triggering set -e in the outer harness.
BAD_EXIT=0
PATH="${BAD_STUB_DIR}:${PATH}" bash "$PROVISION_ENV_SCRIPT" \
    ALLOWED_ORIGINS /vaultmtg/app/production/ALLOWED_ORIGINS \
    >/dev/null 2>&1 || BAD_EXIT=$?

if [[ "$BAD_EXIT" -eq 0 ]]; then
    fail "Test A — provision-env.sh exited 0 on ParameterNotFound (should have failed); missing SSM guard is absent — deploy would proceed with a blank/broken value"
fi
pass "Test A.1 — provision-env.sh exited ${BAD_EXIT} (non-zero) on ParameterNotFound"

# The env file MUST NOT have been written with the key when the ssm call failed.
if [[ -f "$STUB_ENV_FILE" ]] && grep -q "^ALLOWED_ORIGINS=" "$STUB_ENV_FILE" 2>/dev/null; then
    fail "Test A — ALLOWED_ORIGINS was written to env file despite ParameterNotFound (deploy would proceed with a blank or broken value)"
fi
pass "Test A.2 — ALLOWED_ORIGINS not written to env file after ParameterNotFound (correct abort)"

# ===========================================================================
# Test B — provision-db-url.sh FAILS when DB SSM parameters are missing
#
# AC: provision-db-url.sh reads SSM_PROD_APP_DB_SECRET_ARN, SSM_PROD_DB_ENDPOINT,
# and SSM_PROD_DB_NAME.  If any returns ParameterNotFound the script MUST exit
# non-zero.  The script has an explicit empty-value guard; the test ensures the
# aws exit-code propagation (set -e) also triggers the abort, not just the
# empty-value guard.  Both guards must be present for defence in depth.
# ===========================================================================
info "Test B — provision-db-url.sh exits non-zero on ParameterNotFound..."

PROVISION_DB_SCRIPT="${SCRATCH}/provision-db-url-test.sh"
cp "${REPO_ROOT}/scripts/deploy/provision-db-url.sh" "$PROVISION_DB_SCRIPT"
chmod +x "$PROVISION_DB_SCRIPT"

# Reset env file.
rm -f "$STUB_ENV_FILE"

BAD_DB_EXIT=0
PATH="${BAD_STUB_DIR}:${PATH}" bash "$PROVISION_DB_SCRIPT" \
    >/dev/null 2>&1 || BAD_DB_EXIT=$?

if [[ "$BAD_DB_EXIT" -eq 0 ]]; then
    fail "Test B — provision-db-url.sh exited 0 on ParameterNotFound (should have failed); deploy would proceed without DATABASE_URL"
fi
pass "Test B.1 — provision-db-url.sh exited ${BAD_DB_EXIT} (non-zero) on ParameterNotFound"

# DATABASE_URL MUST NOT have been written to the env file.
if [[ -f "$STUB_ENV_FILE" ]] && grep -q "^DATABASE_URL=" "$STUB_ENV_FILE" 2>/dev/null; then
    fail "Test B — DATABASE_URL was written to env file despite ParameterNotFound (BFF would start with no DB connection)"
fi
pass "Test B.2 — DATABASE_URL not written to env file after ParameterNotFound (correct abort)"

# ===========================================================================
# Test C — provision-env.sh SUCCEEDS when the SSM parameter is present
#
# Positive-path regression guard: verifies the test harness does not pass
# simply because the bad stub rejects all calls indiscriminately.  With a
# good stub (exits 0 + returns a value), provision-env.sh MUST exit 0 and
# write the key into the env file.
# ===========================================================================
info "Test C — provision-env.sh exits 0 and writes env key when SSM param is present..."

# Reset env file for a clean write.
rm -f "$STUB_ENV_FILE"

GOOD_EXIT=0
PATH="${GOOD_STUB_DIR}:${PATH}" bash "$PROVISION_ENV_SCRIPT" \
    ALLOWED_ORIGINS /vaultmtg/app/production/ALLOWED_ORIGINS \
    >/dev/null 2>&1 || GOOD_EXIT=$?

if [[ "$GOOD_EXIT" -ne 0 ]]; then
    fail "Test C — provision-env.sh exited ${GOOD_EXIT} with a valid SSM response (expected exit 0)"
fi
pass "Test C.1 — provision-env.sh exited 0 when SSM parameter returned a value"

# The env file MUST contain the key when the ssm call succeeded.
if ! grep -q "^ALLOWED_ORIGINS=" "$STUB_ENV_FILE" 2>/dev/null; then
    fail "Test C — ALLOWED_ORIGINS not written to env file even though SSM returned a value (provision-env.sh is not writing the key)"
fi
pass "Test C.2 — ALLOWED_ORIGINS written to env file when SSM parameter was present"

# ===========================================================================
# Test D — provision-env.sh --with-decryption: same failure guard applies
#
# AC: provision-env.sh is called with --with-decryption for SecureString
# parameters (e.g. CLERK_SECRET_KEY).  If the aws call fails, the script
# MUST still exit non-zero.  This ensures the --with-decryption code path
# is not accidentally exempt from the set -e failure propagation.
# ===========================================================================
info "Test D — provision-env.sh --with-decryption exits non-zero on ParameterNotFound..."

rm -f "$STUB_ENV_FILE"

DECRYPTION_EXIT=0
PATH="${BAD_STUB_DIR}:${PATH}" bash "$PROVISION_ENV_SCRIPT" \
    CLERK_SECRET_KEY /vaultmtg/app/production/CLERK_SECRET_KEY --with-decryption \
    >/dev/null 2>&1 || DECRYPTION_EXIT=$?

if [[ "$DECRYPTION_EXIT" -eq 0 ]]; then
    fail "Test D — provision-env.sh --with-decryption exited 0 on ParameterNotFound (SecureString failure guard missing)"
fi
pass "Test D.1 — provision-env.sh --with-decryption exited ${DECRYPTION_EXIT} (non-zero) on ParameterNotFound"

if [[ -f "$STUB_ENV_FILE" ]] && grep -q "^CLERK_SECRET_KEY=" "$STUB_ENV_FILE" 2>/dev/null; then
    fail "Test D — CLERK_SECRET_KEY written to env file despite ParameterNotFound on --with-decryption path"
fi
pass "Test D.2 — CLERK_SECRET_KEY not written to env file after ParameterNotFound (--with-decryption abort correct)"

# ===========================================================================
# Summary
# ===========================================================================
echo ""
echo -e "${GREEN}==============================================${NC}"
echo -e "${GREEN} SSM pre-flight integration test: ALL PASS   ${NC}"
echo -e "${GREEN}==============================================${NC}"
echo ""
echo "  Test A — provision-env.sh: ParameterNotFound → exit non-zero, key NOT written"
echo "  Test B — provision-db-url.sh: ParameterNotFound → exit non-zero, DATABASE_URL NOT written"
echo "  Test C — provision-env.sh: param present → exit 0, key written (positive-path guard)"
echo "  Test D — provision-env.sh --with-decryption: ParameterNotFound → exit non-zero"
echo ""
