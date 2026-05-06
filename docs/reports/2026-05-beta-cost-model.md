# AWS Beta Cost Model — May 2026

**Prepared**: 2026-05-06  
**Ticket**: #1412  
**AWS Account**: 901347789205 (us-east-1)  
**Data source**: AWS Cost Explorer, AWS CLI (`personal` profile)

---

## 1. Current Cost Breakdown (Actuals)

### Method

AWS Cost Explorer returns $0 blended cost for all months prior to May 2026 because AWS Activate credits ($1,000 approved 2026-05-05) offset all usage charges before blended cost is computed. To extract true spend, costs were queried with `RECORD_TYPE=Usage` filter and `UnblendedCost` metric.

The account went live with production infrastructure on **2026-05-05** (EC2 launch confirmed via `LaunchTime`). Six days of data (2026-05-01 through 2026-05-06) were used as the basis for monthly extrapolation.

### Running Infrastructure

| Resource | Spec | Status |
|---|---|---|
| EC2 | t3.small (2 vCPU, 2 GiB) | running |
| RDS | db.t3.micro PostgreSQL, 20 GB gp3 | available |
| CloudFront | 3 distributions | deployed |
| Lambda | `mtga-sync` (256 MB, Go/AL2023) | active |
| Lambda | `mtga-companion-production-cfn-vpc-config` (128 MB, Python) | active |
| S3 | Static SPA assets | active |
| SSM | Parameter Store secrets | active |
| Route 53 | DNS | active |
| Amazon Registrar | Domain registration | $35/yr one-time |

### 6-Day Actuals and Monthly Extrapolation

| Service | 6-Day Actual | ~30-Day Estimate |
|---|---|---|
| RDS (db.t3.micro) | $1.80 | $9.00 |
| EC2 Compute (t3.small) | $1.69 | $8.46 |
| VPC (NAT Gateway / ENI) | $0.41 | $2.07 |
| EC2 - Other (EBS, data transfer) | $0.07 | $0.33 |
| Secrets Manager | $0.05 | $0.23 |
| Route 53 | $0.00 | $0.01 |
| S3 | $0.00 | $0.01 |
| **Total compute/infra** | **$4.02** | **$20.11** |
| Domain registration (amortized) | — | $2.92 |
| **Total monthly run rate** | — | **~$23.03** |

**Note on stated $38/month baseline**: The $38 figure cited in the ticket likely reflects a prior estimate or includes external services (Vercel, domain renewal timing, etc.). The AWS-only run rate from actuals is ~$20/month compute + ~$3/month amortized domain = **~$23/month**. This model uses the actuals.

### Credits Status (as of 2026-05-06)

| Item | Amount |
|---|---|
| Credits granted (2026-05-05) | $1,000.00 |
| Credits applied May 1–6 | $4.02 |
| Credits remaining | ~$995.98 |

---

## 2. Projected Costs at Scale

### Assumptions

1. **Users are MAU (monthly active users)**, not concurrent. Peak concurrency assumed at 10% of MAU.
2. **SPA is served by CloudFront from S3** — CloudFront free tier covers 1 TB/month data transfer and 10M requests/month. This holds through the 5,000-user scenario.
3. **Lambda sync job** runs once daily (EventBridge Scheduler), ~60 seconds, 256 MB memory. Cost is negligible at all scales modeled.
4. **RDS connection count**: db.t3.micro supports ~60 max connections. At 500+ concurrent users a connection pooler (PgBouncer on EC2) is needed, not a larger instance.
5. **EC2 sizing**: t3.small handles single-threaded BFF with burst credit. At 500+ concurrent users the burst bucket drains; t3.medium is required. At 5,000 users, a second instance or larger type is needed.
6. **RDS storage**: assumes 5 MB per user (match history, draft logs, deck data). 5,000 users = 25 GB — within t3.micro's gp3 disk but storage is charged separately.
7. **VPC / NAT Gateway cost** scales only with data volume, not user count. Held flat through 500-user scenario; increases modestly at 5,000.
8. **No ALB provisioned** currently. At 500+ users an ALB ($16/month minimum) would improve health checking and TLS termination. Excluded from 50-user scenario, included at 500+.
9. **Pricing sources**: EC2 on-demand us-east-1 Linux ([Economize](https://www.economize.cloud/resources/aws/pricing/ec2/t3.small/), [Vantage](https://instances.vantage.sh/aws/ec2/t3.medium)); RDS PostgreSQL on-demand ([Vantage](https://instances.vantage.sh/aws/rds/db.t3.micro), [Economize](https://www.economize.cloud/resources/aws/pricing/rds/db.t3.micro/)); CloudFront ([AWS](https://aws.amazon.com/cloudfront/pricing/)); Lambda ([AWS](https://aws.amazon.com/lambda/pricing/)).

### On-Demand Pricing Reference

| Resource | Price | Unit |
|---|---|---|
| EC2 t3.small | $0.0208/hr | $15.18/month |
| EC2 t3.medium | $0.0416/hr | $30.37/month |
| RDS db.t3.micro | $0.018/hr | $13.14/month |
| RDS db.t3.small | $0.036/hr | $26.28/month |
| RDS gp3 storage | $0.115/GB/month | — |
| ALB | $0.008/LCU-hr + $0.0225/hr | ~$16/month minimum |
| CloudFront data transfer | $0.085/GB | first 1 TB/month free |
| CloudFront requests | $0.0100/10,000 HTTPS req | first 10M/month free |
| Lambda | $0.20/1M req + $0.0000167/GB-s | first 1M req/month free |
| NAT Gateway | $0.045/hr + $0.045/GB | ~$2/month base |

---

### Scenario A: 50 Users (First Beta Batch)

**Profile**: 50 MAU, ~5 concurrent peak, light read-heavy workload. Existing architecture holds without changes.

| Service | Spec | Monthly Cost |
|---|---|---|
| EC2 | t3.small (current) | $15.18 |
| RDS | db.t3.micro, 20 GB gp3 | $13.14 + $2.30 = $15.44 |
| VPC / NAT Gateway | base + minimal data | $2.07 |
| S3 | static assets, low traffic | $0.01 |
| CloudFront | within free tier | $0.00 |
| Lambda | daily sync, within free tier | $0.00 |
| Route 53 | DNS queries | $0.01 |
| Secrets Manager | 5 secrets | $0.23 |
| Domain (amortized) | $35/yr | $2.92 |
| **Total** | | **$35.86/month** |

**Architecture verdict**: No changes needed. t3.small + db.t3.micro handles 50 users comfortably.

---

### Scenario B: 500 Users (Successful Beta)

**Profile**: 500 MAU, ~50 concurrent peak. EC2 burst credits deplete under sustained load; t3.medium required. ALB added for health checking. PgBouncer runs on EC2 (no added instance cost). RDS storage grows to ~3 GB (well within 20 GB allocated).

| Service | Spec | Monthly Cost |
|---|---|---|
| EC2 | t3.medium (upgrade from t3.small) | $30.37 |
| RDS | db.t3.micro, 20 GB gp3 | $15.44 |
| ALB | minimum provisioned | $16.00 |
| VPC / NAT Gateway | base + moderate data | $3.50 |
| S3 | static assets | $0.05 |
| CloudFront | ~50 req/user/day = 750K req/mo; within free tier | $0.00 |
| Lambda | within free tier | $0.00 |
| Route 53 | $0.01 | $0.01 |
| Secrets Manager | $0.23 | $0.23 |
| Domain (amortized) | $2.92 | $2.92 |
| **Total** | | **$68.52/month** |

**Architecture change required**: Upgrade EC2 from t3.small to t3.medium before sustained concurrent load. Add ALB or configure EC2 health-check endpoint. No RDS change needed; add PgBouncer on existing EC2.

---

### Scenario C: 5,000 Users (Pre-Launch Growth)

**Profile**: 5,000 MAU, ~500 concurrent peak. Single EC2 instance saturated; need t3.large or two t3.medium instances behind ALB. RDS db.t3.micro hits connection limit even with PgBouncer at high throughput; upgrade to db.t3.small. Storage grows to ~25 GB, slightly above current 20 GB allocation — expand to 30 GB.

| Service | Spec | Monthly Cost |
|---|---|---|
| EC2 | 2x t3.medium (or 1x t3.large ~$60/mo) | $60.74 |
| RDS | db.t3.small, 30 GB gp3 | $26.28 + $3.45 = $29.73 |
| ALB | moderate LCU usage | $22.00 |
| VPC / NAT Gateway | ~10 GB/month data | $6.50 |
| S3 | growing asset storage | $0.50 |
| CloudFront | ~100 req/user/day = 15M req/mo; ~2 GB/mo transfer; above free tier | $0.50 |
| Lambda | still within free tier | $0.00 |
| Route 53 | $0.05 | $0.05 |
| Secrets Manager | $0.23 | $0.23 |
| Domain (amortized) | $2.92 | $2.92 |
| **Total** | | **$123.17/month** |

**Architecture changes required**: Add second EC2 instance or resize to t3.large. Upgrade RDS to db.t3.small. Expand RDS storage to 30+ GB. ALB becomes mandatory for multi-instance routing.

---

## 3. AWS Activate Credits Runway

Credits available: **$1,000.00** (granted 2026-05-05)

| Scenario | Monthly AWS Spend | Credits Runway | Notes |
|---|---|---|---|
| Current (1 user) | $23.03 | **43 months** | Credits never exhaust at this rate |
| 50 users | $35.86 | **27.9 months** | Well within runway |
| 500 users | $68.52 | **14.6 months** | ~14 months from credits grant (~Jul 2027) |
| 5,000 users | $123.17 | **8.1 months** | ~Aug 2026 if growth is rapid |

**MAU level where credits run out in 12 months**: ~$83/month spend. This is reached somewhere between 500 and 5,000 users — modeled at approximately **~1,200 MAU** (linear interpolation between scenarios B and C).

### Free Tier Limits to Watch

| Service | Free Tier Limit | Risk Level |
|---|---|---|
| CloudFront requests | 10M req/month | Low — not hit until ~500 users at 50 req/day |
| CloudFront data transfer | 1 TB/month | Low — SPA assets are small; safe well past 5K users |
| Lambda requests | 1M req/month | None — daily sync job is ~30 invocations/month |
| Lambda compute | 400,000 GB-s/month | None — 30 invocations x 60s x 0.25 GB = 450 GB-s/month; near limit but within |
| S3 GET requests | 20,000/month (12-month free tier, expired) | None — already paying; cost negligible |

**Lambda alert**: At current 256 MB x 60s daily sync, monthly usage is ~450 GB-s against the 400,000 GB-s permanent free tier. No issue. However, if sync job duration grows (more sets, more cards), monitor this.

---

## 4. Recommendations

### Before Beta Launch (50 users)

No architecture changes are required. The current t3.small + db.t3.micro stack handles 50 users with headroom. AWS Activate credits provide ~28 months of runway at this spend level.

**One action**: Enable RDS automated backups if not already configured. At 50 users this is the highest data-loss risk with zero cost impact (automated backups within 1x DB size are included in RDS pricing).

### Before Scaling to 500 Users

1. **Upgrade EC2 from t3.small to t3.medium** — $15/month increase, prevents burst credit depletion under concurrent load. This is the single most important architectural change.
2. **Add PgBouncer** (on existing EC2, no cost) — pools RDS connections, extends db.t3.micro lifetime to well past 500 users.
3. **Provision an ALB** — $16/month, required for health checks and enables future multi-instance scaling without re-architecture.

### Cost Optimization Quick Wins

| Action | Savings | Effort |
|---|---|---|
| 1-year Reserved Instance for EC2 (t3.medium) | ~38% savings → ~$11/month | Low — commit after 500-user milestone is confirmed |
| 1-year Reserved Instance for RDS (db.t3.micro) | ~38% savings → ~$5/month | Low — same timing |
| Review NAT Gateway usage | Potential $1–2/month | Medium — audit which services route through NAT vs. VPC endpoints |

**Do not commit Reserved Instances before the 500-user milestone is validated** — if the architecture needs to change (e.g., to t3.large), reserved commitments become stranded costs.

### Red Flags to Watch

1. **VPC / NAT Gateway at $2.07/month with 1 user** — this is higher than expected for near-zero traffic. Investigate whether Lambda or a scheduled task is routing traffic through NAT unnecessarily. S3 and DynamoDB should use VPC Gateway Endpoints (free) rather than NAT.
2. **Three CloudFront distributions** for a single-SPA app — review whether all three are actively serving traffic. Each distribution has a small per-request cost and management overhead; consolidate if possible.
3. **Credits expire** — confirm with AWS whether the $1,000 Activate credits have an expiration date (typically 1 year from grant). If so, the hard deadline is ~2027-05-05 regardless of consumption rate.

---

## Summary Table

| Metric | Current | 50 Users | 500 Users | 5,000 Users |
|---|---|---|---|---|
| Monthly AWS cost | ~$23 | ~$36 | ~$69 | ~$123 |
| EC2 instance | t3.small | t3.small | **t3.medium** | **2x t3.medium** |
| RDS instance | db.t3.micro | db.t3.micro | db.t3.micro | **db.t3.small** |
| Credits remaining after 6mo | ~$858 | ~$783 | ~$586 | ~$261 |
| Credits runway | 43 mo | 28 mo | 15 mo | 8 mo |
| Architecture changes needed | None | None | EC2 upgrade, ALB, PgBouncer | +RDS upgrade, 2nd EC2 |
