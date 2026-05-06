# VaultMTG CloudFormation Infrastructure

## Stacks

| File | Stack name | Purpose |
|---|---|---|
| `vaultmtg-app-cdn.yaml` | `vaultmtg-app-cdn` | S3 buckets, ACM cert, CloudFront distributions for vaultmtg.app and app.vaultmtg.app |

---

## vaultmtg-app-cdn

### What it creates

| Resource | Details |
|---|---|
| S3 bucket `vaultmtg-app-marketing` | Static assets for vaultmtg.app marketing site. No public access -- served via CloudFront OAC only. |
| S3 bucket `vaultmtg-app-spa` | Static assets for app.vaultmtg.app React SPA. No public access -- served via CloudFront OAC only. |
| ACM certificate | Covers `vaultmtg.app`, `www.vaultmtg.app`, `app.vaultmtg.app`. DNS-validated. |
| CloudFront distribution (marketing) | Aliases: `vaultmtg.app`, `www.vaultmtg.app`. Redirect HTTP to HTTPS. 404 -> /index.html. |
| CloudFront distribution (SPA) | Alias: `app.vaultmtg.app`. Redirect HTTP to HTTPS. 404 -> /index.html (React Router fallback). |

### Parameters

| Parameter | Default | Description |
|---|---|---|
| `Environment` | `production` | Tag value applied to all resources. Allowed: `production`, `staging`. |

### Deploy

This stack must be deployed in **us-east-1** (ACM certificates used by CloudFront must be in us-east-1).

```bash
aws cloudformation deploy \
  --stack-name vaultmtg-app-cdn \
  --template-file infrastructure/cloudformation/vaultmtg-app-cdn.yaml \
  --parameter-overrides Environment=production \
  --capabilities CAPABILITY_IAM \
  --region us-east-1 \
  --profile personal
```

To do a dry-run (change set preview) before executing:

```bash
aws cloudformation deploy \
  --stack-name vaultmtg-app-cdn \
  --template-file infrastructure/cloudformation/vaultmtg-app-cdn.yaml \
  --parameter-overrides Environment=production \
  --capabilities CAPABILITY_IAM \
  --region us-east-1 \
  --profile personal \
  --no-execute-changeset
```

Then review the change set in the AWS Console or via:

```bash
aws cloudformation describe-change-set \
  --stack-name vaultmtg-app-cdn \
  --change-set-name <change-set-name> \
  --region us-east-1 \
  --profile personal
```

---

## Manual steps after deploying vaultmtg-app-cdn

### Step 1 -- Add ACM DNS validation CNAME records to Route 53

After the stack deploys, the ACM certificate will be in `PENDING_VALIDATION` state.

1. Go to **AWS Console > Certificate Manager** (us-east-1).
2. Find the certificate for `vaultmtg.app`.
3. Expand the certificate details. You will see three CNAME records (one per domain: `vaultmtg.app`, `www.vaultmtg.app`, `app.vaultmtg.app`).
4. For each CNAME record, add it to the **Route 53 hosted zone** for `vaultmtg.app`.
5. Wait 5-30 minutes for DNS to propagate. The certificate status will change to `Issued` automatically.

The CloudFront distributions will remain in a `Disabled` state (aliases not usable) until the certificate is issued.

### Step 2 -- Update Route 53 A records to point at CloudFront

Once the certificate is issued, retrieve the CloudFront domain names from the stack outputs:

```bash
aws cloudformation describe-stacks \
  --stack-name vaultmtg-app-cdn \
  --region us-east-1 \
  --profile personal \
  --query "Stacks[0].Outputs"
```

Look for `MarketingDistributionDomain` and `SPADistributionDomain`. These will be values like `d1234abcd.cloudfront.net`.

In Route 53, for the `vaultmtg.app` hosted zone:

| Record | Type | Value |
|---|---|---|
| `vaultmtg.app` | A (Alias) | `MarketingDistributionDomain` from stack outputs |
| `www.vaultmtg.app` | A (Alias) | `MarketingDistributionDomain` from stack outputs |
| `app.vaultmtg.app` | A (Alias) | `SPADistributionDomain` from stack outputs |

Use **Alias** records pointing to the CloudFront distribution, not CNAME records for the apex domain.

### Step 3 -- Upload content and verify

Upload a test `index.html` to each S3 bucket:

```bash
# Marketing site
echo "<h1>VaultMTG</h1>" | aws s3 cp - s3://vaultmtg-app-marketing/index.html \
  --content-type text/html --profile personal

# SPA
echo "<h1>VaultMTG App</h1>" | aws s3 cp - s3://vaultmtg-app-spa/index.html \
  --content-type text/html --profile personal
```

Then visit `https://vaultmtg.app` and `https://app.vaultmtg.app` to confirm both distributions serve content over HTTPS.
