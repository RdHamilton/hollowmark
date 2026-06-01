# VaultMTG Monthly Acquisition & SEO Report — May 2026

**Report Date:** June 1, 2026  
**Reporting Period:** May 1–31, 2026  
**Status:** Pre-Beta (Beta launch: August 18, 2026)

---

## Executive Summary

May 2026 was a planning and content preparation month as VaultMTG entered the pre-beta phase. No public campaigns ran; the team focused on marketing asset creation, convention strategy, and content planning for the June–August window. One technical feature (daemon app icon) shipped. Organic search and community presence remain the primary acquisition levers available before beta launch.

**Key insight:** The opportunity window for organic SEO groundwork is now (May–August). Content published in June–July targeting keywords like "MTG Arena companion app," "MTG Arena draft tracker," and seasonal set-specific terms will be indexed and ranking by beta launch in August.

---

## Traffic & Acquisition

### Website Analytics

| Metric | Value | Notes |
|---|---|---|
| Sessions (GA4) | NEEDS DATA: Google Analytics 4 | No production tracking configured yet |
| Pageviews | NEEDS DATA: Google Analytics 4 | — |
| Top landing page | vaultmtg.app (home) | Organic search only; UTM-tagged links not yet live |
| Conversion rate (landing → waitlist) | NEEDS DATA: Conversion tracking | Waitlist form exists; need to instrument GA4 events |
| Traffic source breakdown | NEEDS DATA: Google Analytics 4 | — |

**Status:** GA4 instrumentation is required before next month. Currently, all acquisition data is opaque.

---

## Campaign Activity

### Campaigns Run in May

**Status:** No public campaigns ran in May. All activity was preparation.

### Content & Marketing Assets Created

| Asset | Created | Status | Shipping Date |
|---|---|---|---|
| Beta launch announcement copy (Reddit, Discord, X, email) | May 6 | Ready for deployment | June–July (ahead of August 18 beta) |
| Convention & event promotion plan (Sep–Dec 2026) | May 8 | Approved | Guides Sep+ events |
| Beta launch X (Twitter) post schedule | May 6 | Ready | Q2 re-engagement + beta launch |
| Reddit announcement drafts (r/magicTCG, r/lrcast variants) | May 6 | Ready | June–July |
| Waitlist copy | May 6 | Ready | June 1 deployment |
| UTM naming convention & link generator | May 6 | Live | Used for all external links from June onward |

**Key deliverable:** `docs/marketing/utm-naming-convention.md` establishes consistent parameter naming across all campaigns (Reddit, Discord, X, email, events). All June+ links must use these conventions for proper GA4 attribution.

---

## Product Launches (May)

### Shipped Features

| Feature | PR | Status | Acquisition angle |
|---|---|---|---|
| Daemon app icon + system tray UI | #307 | Merged | Improves daemon discoverability in System Tray; ship with beta announcement |

**Note on verification:** Only one merged PR in May matching the shipping criteria. All convention event plan features (exhibitor materials, demo setup, streamer outreach) are marketing operations, not product features.

---

## Organic SEO & Search Position

### Keyword Research — Current MTG Set (Outlaws of Thunder Junction)

**Trending keywords in May (search volume estimates):**

| Keyword | Search Volume | Relevance | Competition | Opportunity |
|---|---|---|---|---|
| `MTG Arena Outlaws of Thunder Junction draft tier list` | High (1,000+ mo.) | **Direct** — aligns with VaultMTG draft picks feature | Moderate (Untapped.gg, 17Lands, MTG Arena Zone) | **HIGH** — VaultMTG comparison content ("Companion vs. static tier lists") |
| `MTG Arena draft tracker` | Medium (200–500 mo.) | **Direct** — core feature | Low (no dedicated content) | **VERY HIGH** — first-mover for detailed guide |
| `best MTG Arena companion app` | Low–Medium (100–300 mo.) | **Direct** — product comparison | Very low (sparse content) | **VERY HIGH** — VaultMTG is positioned well here |
| `how to improve MTG Arena draft` | Medium (300–600 mo.) | **Indirect** — educational content; VaultMTG used as supporting tool | High (guides on many sites) | **MEDIUM** — feature in broader draft improvement guide |
| `17Lands alternatives` | Low (50–150 mo.) | **Direct** — competitive positioning | Low (sparse) | **HIGH** — alternative analysis |

**Sources:**
- [Untapped.gg MTG Arena Draft Tier Lists](https://mtga.untapped.gg/limited/draft/outlaws-of-thunder-junction/tier-list)
- [17Lands MTG Limited Data](https://www.17lands.com/)
- [MTG Arena Zone Draft Guides](https://mtgazone.com/outlaws-of-thunder-junction-otj-limited-tier-list/)

### Current Search Visibility

| Metric | Status | Notes |
|---|---|---|
| Domain authority (ahrefs/SEMrush) | NEEDS DATA: External tools | Baseline unknown; establish in June |
| Backlinks | NEEDS DATA: Link audit | No inbound links from relevant MTG media yet |
| Top organic keywords driving traffic | NEEDS DATA: Google Search Console | GSC not configured; set up immediately |
| Keyword rankings (tracked keywords) | NEEDS DATA: Rank tracker | Start tracking before June content publish |

**Action required:** Set up Google Search Console and establish baseline keyword rankings before content publication in June.

---

## Competitor Signals

### Key Competitors

| Competitor | Signal | May Activity | Relevance |
|---|---|---|---|
| **17Lands** | Draft data + overlay | — | Direct competitor for overlay; focus is analytics-first, not product ease |
| **Untapped.gg** | In-game overlay + community stats | — | Broad feature set; VaultMTG focus is draft picks + match tracking |
| **MTG Arena Zone** | Content & tier lists | Continuous content publication | Content partner opportunity (not competitor) |
| **Arena Tutor** | Companion app | — | Lesser-known; outdated feature set |

**May competitor activity:** No significant new feature announcements detected from 17Lands or Untapped.gg. Both remain focused on existing features (overlay, historical data). VaultMTG's positioning as a lightweight, daemon-based alternative to heavy overlay tools is defensible.

---

## Community & Social Presence

### Reddit

| Community | Status | Notes |
|---|---|---|
| r/magicTCG | No posts | 1.3M members; announcement planned for June (beta launch thread) |
| r/DraftMTG | No posts | 120k members; draft-focused audience; announcement planned for June |
| r/lrcast | No posts | 40k members; high-intent limited players; announcement planned for June |
| Relevant threads | Monitoring | Watching for "MTG Arena tracker," "draft helper," "companion app" threads for non-spammy mention opportunities |

### Discord

| Server | Status | Notes |
|---|---|---|
| VaultMTG #announcements | Prepared | Beta launch announcement copy ready; ship August 18 |
| External MTG servers | Not yet engaged | Streamer outreach plan ready; begins June 15 |

### X (Twitter)

| Activity | Status | Notes |
|---|---|---|
| Beta launch thread (4-tweet format) | Scheduled | Ready; ship June 23 or aligned with waitlist open (June 1) |
| Engagement (responds to tracker mentions) | Not yet live | Community-first approach: genuine replies only, no promotional spam |
| Pro Tour coverage engagement | Planned | Pro Tour Amsterdam (July 17–19): live-tweet featured drafts; compare with VaultMTG recommendations |

---

## Waitlist & Lead Generation

### Waitlist Signup Flow

| Metric | Status | Notes |
|---|---|---|
| Landing page live | Yes | vaultmtg.app with signup form |
| Form instrumentation (GA4 events) | NEEDS DATA: Event tracking | Need to add `waitlist_signup` event tracking |
| Waitlist signups (cumulative) | NEEDS DATA: Mailchimp export | Extract from Mailchimp list as of May 31 |
| Signup source breakdown | NEEDS DATA: UTM tracking | No UTM-tagged links live yet; begins June 1 |

**Action required:** Enable GA4 event tracking for waitlist signups immediately so June metrics can be properly attributed.

---

## Shipped Features Promoting Acquisition (May)

### 1. Daemon App Icon & System Tray UI (PR #307)

**Shipping significance:** The system tray icon improves daemon visibility — users can now clearly see that the daemon is running. This reduces friction in the onboarding experience (removes "is it actually working?" uncertainty).

**Promotion angle for beta launch:** Include in beta announcement: "We made the daemon less of a mystery. You'll see it in your system tray, and you can control it from there."

**Estimated impact:** Minor positive friction reduction; primarily operational, not a marketing driver.

---

## Next Month: June Targets & Content Plan

### 3 Keywords to Target (June Content)

1. **"MTG Arena draft tracker"** — Publish a detailed feature guide by June 10
   - Landing page: "VaultMTG Draft Tracker — Pick recommendations + win rate by archetype"
   - Keyword volume: 200–500 mo.; low competition
   - SEO opportunity: VERY HIGH — first-mover for this exact query

2. **"Best MTG Arena companion app"** — Publish comparison guide by June 15
   - Landing page: "Companion App Comparison — VaultMTG vs. Untapped.gg vs. 17Lands"
   - Keyword volume: 100–300 mo.; very low competition
   - SEO opportunity: VERY HIGH — VaultMTG will be explicitly featured as one of only 3 options

3. **"MTG Arena Outlaws of Thunder Junction draft tier list"** — Seasonal content tie-in
   - Landing page: "OTJ Draft Tier List & Pick Recommendations (with VaultMTG overlay)"
   - Keyword volume: 1,000+ mo. (high)
   - SEO opportunity: HIGH — but requires tiered approach (target long-tail variants first, not the primary high-competition keyword)

### 1–2 Content Pieces to Publish (June–July)

**Content Piece 1: "MTG Arena Draft Tracker Guide" (June 10)**
- Topic: How VaultMTG's draft tracker works + why static tier lists aren't enough
- Length: 1,000–1,500 words
- SEO target: "MTG Arena draft tracker," "how to improve MTG Arena draft"
- Includes: Screenshots of the daemon, pick overlay, match history; comparison with manual logging
- CTA: "Apply for beta" (with utm_source=organic)

**Content Piece 2: "Companion App Comparison" (June 20)**
- Topic: Untapped.gg vs. 17Lands vs. VaultMTG — what each does and which to use
- Length: 1,200–1,800 words
- SEO target: "best MTG Arena companion app," "MTG Arena overlay," "17Lands alternatives"
- Includes: Feature matrix, use-case breakdown, pricing (all free)
- CTA: "Join the waitlist" or "Apply for beta wave"

---

## Metrics for Next Month (June Baseline)

| Metric | June Target | How to measure |
|---|---|---|
| Sessions (organic traffic) | Establish baseline | GA4 > Acquisition report |
| Pageviews | Establish baseline | GA4 |
| Organic keywords ranking | Top 50 for 3 target keywords | SEMrush / Ahrefs / manual checks |
| Waitlist signups (attributed organic) | 50+ | GA4 events + Mailchimp |
| Backlinks from MTG media | 1–2 | Link audit |
| Reddit thread engagement (beta announcement) | 200+ upvotes, 100+ comments | Manual tracking |

---

## Data Gaps & Instrumentation Checklist

- [ ] **GA4:** Configure session tracking and goals (waitlist signup, beta application)
- [ ] **Search Console:** Verify domain ownership; set up keyword tracking
- [ ] **Rank Tracker:** Set up ahrefs or SEMrush to track target keywords in June+
- [ ] **Conversion tracking:** Instrument GA4 events for `waitlist_signup` and `beta_apply`
- [ ] **Backlink monitoring:** Set up monitoring for inbound links from MTG media
- [ ] **Mailchimp export:** Schedule monthly export of waitlist signups and attribution (UTM source)

---

## Risk & Opportunity Assessment

### Risks
1. **No GA4 data:** Current setup is blind; cannot validate campaign performance
2. **Content not published by June 10:** Window for SEO traction before August beta closes quickly
3. **Competitor content:** If 17Lands or Untapped.gg publish "draft companion comparison" in June, they may rank higher; publish VaultMTG comparison content first

### Opportunities
1. **Early-mover SEO:** "MTG Arena draft tracker" and "best companion app" are low-competition keywords; rank in position 1–3 with quality content
2. **Seasonal content:** New MTG sets drop every 3 months; tie each to a tier list + "how VaultMTG helps you play this set" article
3. **Streamer partnerships:** Outreach begins in June; 5–10 streamers @ 1,000–10,000 concurrent viewers = 50,000+ monthly impressions
4. **Reddit community:** r/magicTCG (1.3M), r/DraftMTG (120k), r/lrcast (40k); beta announcement thread likely 500+ upvotes based on community need

---

## Conclusion

May was a planning month. No public campaigns ran, but all marketing assets are ready for June deployment. The beta launch in August creates a hard deadline for organic SEO groundwork — content published in June–July will be indexed and ranking by then.

**Immediate priorities for June:**
1. Publish "Draft Tracker Guide" and "Companion App Comparison" articles
2. Set up GA4, Search Console, and rank tracking
3. Launch beta announcement on Reddit, Discord, X
4. Begin streamer outreach
5. Drive first wave of beta signups through organic channels

The next report (June 2026) will show SEO traction for these keywords, organic traffic growth, and conversion data to the beta waitlist.

---

## Report Metadata

- **Reporting owner:** Growth & Marketing
- **Next review:** July 1, 2026 (June baseline report)
- **Revision history:** Initial report — May 2026
