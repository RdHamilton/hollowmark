# VaultMTG Pricing Strategy

**North Star:** 50,000 Monthly Active Users (MAU)
**Version:** 1.0
**Date:** 2026-05-06
**Status:** Draft — Pending Stakeholder Review

---

## 1. Market Context

### MTG Arena Total Addressable Market

| Signal | Estimate | Source |
|--------|----------|--------|
| Registered MTG Arena accounts | ~13 million | Hasbro investor disclosures |
| Monthly active players (est.) | ~5–7 million | activeplayer.io, Draftsim analysis |
| Steam concurrent peak (May 2026) | ~17,000 | SteamDB |
| Draft/Limited player segment | ~15–25% of MAU | 17Lands community, r/MagicArena surveys |

**Key insight:** The full MTG Arena playerbase is ~5–7M MAU. Steam numbers (~17K peak concurrent) represent a small fraction — the bulk play via the official launcher and mobile. Draft/Limited players — VaultMTG's core audience — represent roughly 1–1.7M of that base. Constructed and collection players add a larger addressable pool.

**50K MAU feasibility:** Capturing ~0.7–1% of the total Arena playerbase or ~3–5% of the draft-focused segment. Achievable in 18–24 months post-launch with consistent SEO, community presence on r/MagicArena, r/spikes, Discord, and Twitch streamer integrations.

---

## 2. Competitor Pricing Landscape

### 17Lands (Draft Analytics — closest competitor)

| Tier | Price | Key Features |
|------|-------|-------------|
| Free (non-patron) | $0 | All historic aggregate stats, card ratings, color performance |
| Common patron | ~$3/mo | Win rate history chart, card breakdown page, personal color performance |
| Uncommon patron | ~$5/mo | Personal "me" filter across all dashboards |
| Rare patron | ~$15/mo | Manabase evaluator |
| Mythic patron | ~$25/mo | All above + upcoming features |

**Model:** Patreon-based voluntary support. Core analytics are free; patron tiers unlock personal/historical features. 641 paid Patreon members as of mid-2026. Tens of thousands use the site daily.

**Strategic gap:** 17Lands is donation-based, not truly monetized. No annual plans, no proper SaaS funnel. VaultMTG can out-execute on UX, onboarding, and conversion.

### Untapped.gg (MTG Arena Tracker)

| Tier | Price |
|------|-------|
| Free | Deck tracker overlay, basic stats |
| Premium | $7.99/month (or 6 months for price of 5 ≈ $6.65/mo effective) |

**Premium features:** Draftsmith AI draft assistant (adaptive pick ratings), deck recommendations post-draft, pick order rankings, color tier lists by set, meta deck suggestions, opponent hand tracking.

**Key insight:** $7.99/mo is the established market ceiling that players accept for an MTG Arena companion. The "6 months for 5" deal is effectively a ~17% annual discount framing.

### MTGGoldfish (Metagame & Prices)

| Tier | Price |
|------|-------|
| Free | Meta decks, price history, budget brews |
| Premium | $6/month |

**Premium features:** SuperBrew deck finder, unlimited collection tracking, unlimited price alerts, CSV import, card price history downloads. 30-day money-back guarantee.

### Moxfield (Deck Building)

- Fully free with voluntary Patreon support ($1+/mo)
- No hard paywalled features; monetizes via community goodwill
- Premium feel drives high user loyalty but low revenue capture

### AetherHub / TappedOut

- Primarily free, minimal monetization
- AetherHub: ad-supported, no meaningful paid tier
- TappedOut: free with optional supporter badge

### Competitive Summary

| Tool | Monthly Price | Model |
|------|--------------|-------|
| 17Lands | Free + $3–25 Patreon | Voluntary / feature-gated |
| Untapped.gg | $7.99 | Hard paywall on premium features |
| MTGGoldfish | $6.00 | Hard paywall on utilities |
| Moxfield | Free ($1+ voluntary) | Goodwill / ad-light |
| AetherHub | Free | Ad-supported |

**Market anchor:** $5.99–$7.99/month is what paying MTG players already accept. VaultMTG should price within this band.

---

## 3. Freemium Conversion Benchmarks

| Segment | Typical Conversion Rate |
|---------|------------------------|
| Broad B2B SaaS freemium | 2–5% |
| Niche / high-intent SaaS | 5–15% |
| Gaming companion tools | 3–8% (estimated) |
| Role-based feature gating (best practice) | ~5.1% median |

**Working assumption for VaultMTG:** 4% conversion rate at steady state. This is conservative for a high-intent niche (draft players actively trying to improve) while accounting for the free alternatives (17Lands, AetherHub) that dilute paid motivation.

Sensitivity range: 2.5% (pessimistic, strong free-tier habit) → 6% (optimistic, AI features differentiate strongly).

---

## 4. Proposed Tier Structure

### Tier 0 — Free (Acquisition Engine)

**Goal:** Remove all friction to sign-up. Build the funnel. Be better than the free competition.

**Included:**
- Draft tracker overlay (auto-detects MTGA game state)
- Match history (last 30 days)
- Win rate stats by format, set, color combination
- Basic deck builder (save up to 10 decks)
- Collection import (CSV / MTGA log)
- Metagame snapshot (top 20 decks by format, updated weekly)
- Public card ratings for current sets (aggregate data)

**Limits (upgrade triggers):**
- Match history capped at 30 days (Pro: unlimited)
- Deck slots capped at 10 (Pro: unlimited)
- No personal card performance breakdown
- No draft pick recommendations / AI guidance
- No export to MTGO / arena-ready formats (Pro)
- No advanced filtering on match history (opponent rank, on-play/draw, etc.)
- No collection-vs-metagame gap analysis

**Rationale:** The free tier must be genuinely useful — better than AetherHub and competitive with the core of 17Lands. Users who experience value are 4–6x more likely to convert.

---

### Tier 1 — VaultMTG Pro

**Price:** $6.99/month | $55.99/year (~$4.67/mo, 33% off)

**Goal:** Convert engaged free users who track drafts regularly and want the edge.

**Everything in Free, plus:**

**Draft & Limited:**
- AI draft pick recommendations (card ratings weighted by picks already made, color signals, set meta)
- Personal ALSA (average last seen at) tracking
- Draft deck optimizer (suggested cuts/includes after pick 45)
- Trophy deck archive (unlimited)
- Historical draft data (full career)
- Color performance by set and event type (personal "me" view)
- Manabase consistency evaluator

**Match & Stats:**
- Unlimited match history with full filtering (rank, opponent archetype, on-play/draw)
- Win rate trend charts (rolling 10, 50, all-time)
- Personal card breakdown page (how each card performs in your decks)
- Mulligan analysis
- Turn-by-turn performance heatmaps

**Deck & Collection:**
- Unlimited deck slots
- Collection vs. metagame gap analysis (cards you need for top decks)
- Deck export (MTGA, MTGO, Moxfield, Archidekt formats)
- Price tracking with alerts (powered by MTGGoldfish feed)

**Perks:**
- Early access to new features
- Pro badge on profile
- Priority support

**Annual plan justification:** 33% discount (vs. Untapped's ~17%) is a stronger incentive to commit. Annual = $55.99 vs. $83.88/year monthly. Reduces churn significantly.

---

### Tier 2 — VaultMTG Lifetime (Optional, Early Adopter)

**Price:** $149 one-time (equivalent to ~21 months of Pro)

**Availability:** Limited to first 500 purchasers (scarcity drives urgency). Re-evaluate after Phase 5 launch.

**Goal:** Generate upfront cash to fund infrastructure; build a loyal early-adopter cohort who will advocate loudly.

**Includes:** All Pro features, for life. As features expand (AI deck optimization, sealed simulator, etc.), Lifetime members get them automatically.

**Rationale for inclusion:**
- Gaming tool communities (Draftsim, Untapped, etc.) respond well to lifetime deals — creates committed advocates
- $149 = ~21 months payback period; at typical 18-month churn, lifetime deal is financially safe
- AppSumo/LTD community data: lifetime launches generate PR and community buzz disproportionate to revenue
- Cap at 500 prevents long-term margin erosion
- Do NOT offer on AppSumo or deal sites — keep it direct to maintain brand positioning

**Risk:** If AI inference costs scale significantly, Lifetime members become expensive. Mitigation: cap at 500, cost model AI calls separately if needed.

---

## 5. Financial Model — 50K MAU Scenario

### Assumptions

| Parameter | Value |
|-----------|-------|
| Target MAU | 50,000 |
| Free-to-Pro conversion rate | 4.0% |
| Monthly Pro subscribers | 2,000 |
| Monthly Pro price | $6.99 |
| Annual Pro subscribers (of 2,000 total) | 40% = 800 |
| Annual Pro revenue per user | $55.99 |
| Monthly Pro revenue per user | $6.99 |
| Lifetime deal sold (one-time, early phase) | 300 |
| Lifetime price | $149 |

### MRR Breakdown at 50K MAU

| Revenue Stream | Calculation | Monthly Value |
|---------------|-------------|---------------|
| Monthly Pro subs | 1,200 × $6.99 | $8,388 |
| Annual Pro subs (amortized) | 800 × $55.99 / 12 | $3,733 |
| **Total Recurring MRR** | | **$12,121** |
| **ARR** | $12,121 × 12 | **$145,452** |

**Lifetime deal revenue (one-time):** 300 × $149 = **$44,700** (recognized over ~21 months)

### Sensitivity Table — MRR by Conversion Rate

| Conversion Rate | Paid Users | Monthly Pro MRR | Annual MRR |
|----------------|------------|-----------------|------------|
| 2.5% (pessimistic) | 1,250 | ~$7,600 | ~$91,000 |
| 4.0% (base case) | 2,000 | ~$12,100 | ~$145,000 |
| 6.0% (optimistic) | 3,000 | ~$18,200 | ~$218,000 |
| 8.0% (AI-differentiated) | 4,000 | ~$24,200 | ~$290,000 |

### Price Point Analysis

| Monthly Price | Implied Annual ARPU | MRR at 4% / 50K MAU |
|---------------|--------------------|--------------------|
| $4.99 | $59.88 | ~$8,640 |
| $5.99 | $71.88 | ~$10,370 |
| **$6.99** | **$83.88** | **~$12,100** |
| $8.99 | $107.88 | Likely suppresses conversion to ~3% → ~$9,700 |
| $9.99 | $119.88 | Likely suppresses conversion to ~2.5% → ~$9,000 |

**Conclusion:** $6.99/month is the revenue-maximizing price point given market anchors. At $8.99+ you lose more in conversion than you gain in ARPU. The sweet spot sits between MTGGoldfish ($6) and Untapped ($7.99).

---

## 6. Key Strategic Recommendations

### Recommendation 1: Price at $6.99/mo | $55.99/year
Sits between the two established market anchors (MTGGoldfish $6, Untapped $7.99). The 33% annual discount (vs. Untapped's 17%) creates a stronger incentive to commit annually and reduces churn.

### Recommendation 2: Free Tier Must Be Genuinely Good
The draft community uses 17Lands for free. To pull users away, VaultMTG's free tier needs to match 17Lands' aggregate stats quality. The upgrade trigger should be the _personal performance_ layer (your data, not community data) — this is the same model 17Lands uses to convert Patreon supporters.

### Recommendation 3: AI Features Are the Primary Differentiator
Untapped's Draftsmith (AI pick assistant) is what drives conversions for them. VaultMTG's planned AI draft pick recommendations and deck optimization are the clearest upgrade motivators. Invest in AI quality early — it's the moat.

### Recommendation 4: Lifetime Deal for First 500 Users (Early Adopter Window)
Run a 60-day launch window at $149 lifetime, capped at 500 seats. Use this to:
- Generate upfront cash for AWS costs
- Create 500 highly vocal advocates
- Validate willingness to pay before committing to pricing model

### Recommendation 5: No Ads. Ever.
The MTG tool community has strong negative reactions to ad-heavy tools (TappedOut and AetherHub are seen as lesser products partly because of this). Positioning as "no ads, no data selling" is a trust differentiator worth the revenue trade-off.

### Recommendation 6: Stripe Integration (Phase 5) Should Support Both Monthly and Annual
Build Stripe billing with monthly and annual price IDs from day one. Annual billing dramatically reduces payment failure churn and improves LTV.

---

## 7. Milestones to 50K MAU

| MAU Milestone | Timeline (est.) | Conversion at 4% | MRR |
|--------------|----------------|-----------------|-----|
| 5,000 | Phase 4 (Clerk auth live) | 200 paid | ~$1,200 |
| 15,000 | Phase 5 (Stripe live) | 600 paid | ~$3,600 |
| 30,000 | AI features + marketing | 1,200 paid | ~$7,300 |
| 50,000 | North Star | 2,000 paid | ~$12,100 |

---

## 8. Risks & Mitigations

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| 17Lands goes freemium-SaaS | Medium | Compete on UX, mobile, AI; 17Lands is volunteer-run and unlikely to pivot quickly |
| Untapped drops price or adds features | Medium | Match with annual discount strength; differentiate on draft-specific depth |
| AI API costs erode margin | Medium | Cache model outputs per set; use batch inference for pick ratings |
| Low conversion (<2.5%) | Low-Medium | A/B test upgrade prompts; tighten free-tier limits; add social proof on upgrade wall |
| Arena API breaks tracker | Low | Community-reported quickly; have fallback log-parsing mode |

---

## Appendix: Sources

- 17Lands Patreon: patreon.com/17lands — 641 paid members, starting at $3/mo
- 17Lands patron exclusives: blog.17lands.com/posts/patron-exclusives/
- Untapped.gg Premium: mtga.untapped.gg/premium — $7.99/month
- MTGGoldfish Premium: mtggoldfish.com/premium — $6/month
- Moxfield: free, Patreon optional ($1+)
- MTG Arena player base: activeplayer.io (~7M MAU est.); Hasbro (13M registered)
- Freemium conversion benchmarks: firstpagesage.com/saas-freemium-conversion-rates/, Profitwell SaaS Monetization Index
- Gaming app pricing psychology: Charisol Pulse / Medium
- Lifetime deal community value: The Bootstrapped Founder
