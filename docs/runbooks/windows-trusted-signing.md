# Runbook: Windows Code Signing (Azure Trusted Signing)

**Status**: ACTIVE -- credentials configured, pipeline live.
**Ticket**: #1649
**Budget approved**: 2026-05-10 (Ray Hamilton)
**Azure identity validation**: approved 2026-05-10
**Cost**: $9.99/mo (~$120/yr)

---

## Overview

This runbook covers the Azure Trusted Signing workflow for the VaultMTG daemon
Windows installer (`.exe`). The Windows binary and NSIS installer are signed
before release, eliminating the SmartScreen "Windows protected your PC" warning
for end users.

Per ADR-011: EV certificates are explicitly rejected. Azure Trusted Signing
achieves equivalent SmartScreen reputation at a fraction of the cost ($120/yr
vs $300-600/yr) after Microsoft removed the SmartScreen EV advantage in 2024.

The signing step (`microsoft/trusted-signing-action@v0`) is wired into
`.github/workflows/daemon-release.yml` and is active. The step configuration
is also documented in `services/daemon/.goreleaser.yml` header comments for
reference.

---

## Prerequisites (already satisfied)

- Azure subscription -- active
- Azure Trusted Signing account (`vaultmtg-signing`, Basic SKU) -- active
- Service principal (`vaultmtg-daemon-signing`) in Azure AD -- active
- Identity validation -- approved 2026-05-10

---

## Step 1: (Reference) Azure Trusted Signing Account Setup

Account details:
- Resource group: `vaultmtg-signing`
- Account name: `vaultmtg-signing`
- Region: East US
- SKU: Basic ($9.99/mo)
- Certificate profile: `vaultmtg-daemon` (Public Trust)

---

## Step 2: (Reference) Identity Validation

Identity validation was completed and approved by Microsoft on 2026-05-10.
No action required unless the account is recreated.

---

## Step 3: (Reference) Service Principal for CI

```bash
# Login to Azure
az login

# Create app registration for CI
az ad app create --display-name "vaultmtg-daemon-signing"

# Get the app ID
APP_ID=$(az ad app list --display-name "vaultmtg-daemon-signing" \
  --query "[0].appId" -o tsv)

# Create service principal
az ad sp create --id "$APP_ID"

# Create client secret (valid 2 years -- set a calendar reminder to rotate)
az ad app credential reset --id "$APP_ID" \
  --display-name "github-actions-signing" \
  --years 2 \
  --query "password" -o tsv
# Save the output password immediately

# Get tenant ID
az account show --query "tenantId" -o tsv
```

---

## Step 4: (Reference) Service Principal Signing Permissions

The service principal has the `Trusted Signing Certificate Profile Signer`
role assigned on the `vaultmtg-signing` account. No action required unless
the service principal is recreated.

---

## Step 5: Rotate or Re-Populate SSM Parameters

```bash
# Tenant ID
aws ssm put-parameter --profile personal --region us-east-1 \
  --name /vaultmtg/prod/azure-tenant-id \
  --value "YOUR_TENANT_ID" \
  --type String --overwrite

# Client ID (app registration)
aws ssm put-parameter --profile personal --region us-east-1 \
  --name /vaultmtg/prod/azure-client-id \
  --value "YOUR_CLIENT_ID" \
  --type String --overwrite

# Client secret
aws ssm put-parameter --profile personal --region us-east-1 \
  --name /vaultmtg/prod/azure-client-secret \
  --value "YOUR_CLIENT_SECRET" \
  --type SecureString --overwrite

# Trusted Signing account name
aws ssm put-parameter --profile personal --region us-east-1 \
  --name /vaultmtg/prod/azure-trusted-signing-account \
  --value "vaultmtg-signing" \
  --type String --overwrite

# Certificate profile name
aws ssm put-parameter --profile personal --region us-east-1 \
  --name /vaultmtg/prod/azure-certificate-profile \
  --value "vaultmtg-daemon" \
  --type String --overwrite
```

---

## Step 6: Sync GitHub Actions Secrets from SSM

| GitHub Secret | SSM Path |
|---|---|
| `AZURE_TENANT_ID` | `/vaultmtg/prod/azure-tenant-id` |
| `AZURE_CLIENT_ID` | `/vaultmtg/prod/azure-client-id` |
| `AZURE_CLIENT_SECRET` | `/vaultmtg/prod/azure-client-secret` |
| `AZURE_TRUSTED_SIGNING_ACCOUNT` | `/vaultmtg/prod/azure-trusted-signing-account` |
| `AZURE_CERTIFICATE_PROFILE` | `/vaultmtg/prod/azure-certificate-profile` |

---

## Step 7: Active Signing Step in daemon-release.yml

The following step is active in the `goreleaser` job in
`.github/workflows/daemon-release.yml`, positioned AFTER the `Run GoReleaser`
step and BEFORE the `Upload darwin universal binary` step.

The step is tag-guarded (only runs on real `daemon/v*` tags):

```yaml
- name: Sign Windows binary (Azure Trusted Signing)
  if: startsWith(github.ref, 'refs/tags/daemon/v')
  uses: microsoft/trusted-signing-action@v0
  with:
    azure-tenant-id: ${{ secrets.AZURE_TENANT_ID }}
    azure-client-id: ${{ secrets.AZURE_CLIENT_ID }}
    azure-client-secret: ${{ secrets.AZURE_CLIENT_SECRET }}
    endpoint: https://eus.codesigning.azure.net/
    trusted-signing-account-name: ${{ secrets.AZURE_TRUSTED_SIGNING_ACCOUNT }}
    certificate-profile-name: ${{ secrets.AZURE_CERTIFICATE_PROFILE }}
    files-folder: dist/
    files-folder-filter: exe
    file-digest: SHA256
    timestamp-rfc3161: http://timestamp.acs.microsoft.com
    timestamp-digest: SHA256
```

Note: The `endpoint` uses `eus` (East US) matching the Trusted Signing
account region.

After signing, GoReleaser's `extra_files` glob picks up the `.exe` installer
produced by the NSIS hook and uploads it to the GitHub Release. The signing
action modifies the `.exe` in-place before the upload step.

---

## Step 8: Verify End-to-End on a Clean Windows VM

1. Push a `daemon/v*` tag
2. Download `vaultmtg-daemon-setup-<version>.exe` from the GitHub Release
3. On a clean Windows 11 VM (or a new user account):
   - Double-click the installer
   - SmartScreen must NOT show "Windows protected your PC"
   - If SmartScreen appears with "More info": signing is present but
     reputation has not yet accumulated (normal for new certificates;
     resolves after a few hundred downloads)
   - SmartScreen warning must NOT appear for established GA releases

---

## Budget

| Item | Cost |
|---|---|
| Azure Trusted Signing (Basic) | $9.99/mo |
| Apple Developer Program | $99/yr |
| **Total GA signing (annualized)** | **~$219/yr** |

Budget approved by Ray Hamilton on 2026-05-10.

---

## References

- ADR-011: docs/architecture/adr/0011-daemon-distribution-strategy.md
- GoReleaser config signing section: services/daemon/.goreleaser.yml (header comments)
- microsoft/trusted-signing-action: https://github.com/microsoft/trusted-signing-action
- Azure Trusted Signing docs: https://learn.microsoft.com/azure/trusted-signing/
- SmartScreen reputation note: https://learn.microsoft.com/windows/security/threat-protection/microsoft-defender-smartscreen/microsoft-defender-smartscreen-overview
