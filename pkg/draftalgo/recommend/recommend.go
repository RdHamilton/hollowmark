// Package recommend provides the generative draft recommendation surface.
//
// It answers the question "which card in this pack should I take, and why?"
// using 17Lands GIHWR data injected via the same
// draftalgo.RatingsLookup / CardLookup interfaces pickquality.Analyze uses.
//
// Phase A (v0.3.7 MH-ML1):
//   - GIHWR-ranked TopPicks (top 3) + Alternatives (remainder)
//   - Plain-English Reason strings derived from rank tier — NO raw GIHWR %
//     in the primary Reason field (Prof gate requirement)
//   - Graceful N/A degrade when ratings cache is empty
//   - Archetype field is empty in Phase A (data not retained yet)
//
// Phase B (v0.3.8, ADR-047): color-fit reasons, ALSA signals, pool-color
//
//	awareness, archetype suppression (color-commitment heuristic, re-derived
//	fresh each pick), low_confidence marker (< lowConfidenceGIHFloor games),
//	splash-consideration path, and "why not pick 2" differentiator.
//
// Phase C (post-beta): archetype-conditioned ML win rates.
//
// This package does NOT call pickquality.Analyze — doing so per-card would
// be O(n²) and semantically wrong (Analyze grades a pick, not a pack).
// Instead both surfaces share the pkg/draftalgo/rank.ByGIHWR primitive so
// they agree on pack ordering.
package recommend

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RdHamilton/hollowmark/pkg/draftalgo"
	"github.com/RdHamilton/hollowmark/pkg/draftalgo/rank"
)

// lowConfidenceGIHFloor is the minimum number of games-in-hand needed
// for the recommendation to be considered data-backed. Cards below this
// threshold receive LowConfidence=true. ADR-047 §2: threshold is 500.
// Named constant — never inline (ADR-047 fitness function).
const lowConfidenceGIHFloor = 500

// colorCommitmentThreshold is the number of cards of a single color
// needed for a pool to be considered "committed" to that color. Two
// colors both meeting this threshold = two-color commitment, triggering
// pool-aware (archetype + color-fit) mode.
//
// Raised from 3 → 5 per Prof verdict (#1400): at 3 cards the player is
// barely committed; the advisor was killing correct splice picks and bomb
// recommendations. A player needs ~5 on-color cards before two-color
// commitment is meaningful enough to suppress off-color cards.
const colorCommitmentThreshold = 5

// splashHighGIHWRFloor is the GIHWR above which an off-color card
// qualifies for the splash-consideration reason path instead of an
// off-color penalty. ADR-047 §5 (Prof constraint): "high-GIHWR card
// one step off-color MUST have a dedicated splash consideration reason."
const splashHighGIHWRFloor = 66.0

// topQualityGIHWRFloor is the GIHWR at or above which an off-color card
// is considered top-10% quality for its set. Cards at or above this
// threshold bypass full commitment suppression even when the pool is
// color-committed — they still surface in the recommendation list.
// Calibrated to approximately the top 10% of 17Lands GIHWR distributions
// for PremierDraft formats (typically 63–65%). Named constant per
// ADR-047 fitness function (never inline). (#1400)
const topQualityGIHWRFloor = 63.0

// highALSAFloor is the ALSA above which a card qualifies for the
// "frequently available late" scarcity signal. 7.0 is a round number
// that captures bulk rares and late-format filler.
const highALSAFloor = 7.0

// wubrgOrder maps a color character to its canonical WUBRG position (0=W, 4=G).
// Used as the deterministic tiebreaker of last resort when frequency and quality
// scores are equal. Non-WUBRG colors (e.g. colorless "C") are handled by the
// caller using len(wubrgOrder) as a fallback, sorting them after all five colors.
// Defined package-level so both sort sites in derivePoolColorsWithFreq and
// detectTwoColorCommitment share one definition (#1397).
var wubrgOrder = map[string]int{"W": 0, "U": 1, "B": 2, "R": 3, "G": 4}

// Recommendation is one card recommendation entry.
//
// Reason is plain English — never a raw GIHWR percentage (Prof gate).
// GIHWR is available for callers that surface it in a secondary/tooltip
// field only. Archetype is empty in Phase A and when the pool is not
// yet color-committed.
//
// Phase B additions: LowConfidence, WhyNotTopReason.
type Recommendation struct {
	CardID    string  // Arena card ID (string form)
	CardName  string  // Display name from CardLookup
	Priority  int     // 1–5, where 5 is the strongest recommendation
	Reason    string  // Plain-English explanation; never contains a raw "%" literal
	Archetype string  // Phase A: always empty; Phase B: two-color archetype when pool is committed
	GIHWR     float64 // For callers that surface GIHWR in a secondary/tooltip display only
	HasGIHWR  bool    // false when no rating data is available

	// LowConfidence is true when the GIHCount is below lowConfidenceGIHFloor
	// or nil (no sample data). ADR-047 §2. Framing communicates uncertainty,
	// not weakness — see Reason copy.
	LowConfidence bool

	// WhyNotTopReason is a short plain-English phrase explaining why this card
	// ranks below TopPicks[0]. Only populated on TopPicks[1+] when there is a
	// meaningful gap (color mismatch, archetype fit, or sample confidence).
	// Empty for rank-1, empty for marginal/noise-level gaps, and must never
	// contain raw GIHWR numbers. ADR-047 §5 (Prof).
	WhyNotTopReason string
}

// Recommendations holds the full recommendation output for one pack.
//
// Phase B addition: PoolColors — the two dominant pool colors derived
// from the pool card metas, re-derived on every call.
type Recommendations struct {
	TopPicks     []Recommendation // Up to 3 top-ranked picks
	Alternatives []Recommendation // Remaining pack cards in rank order
	PoolColors   []string         // Phase B: dominant pool colors (re-derived each call)
}

// topPickCount is the number of entries in TopPicks. The rest go to
// Alternatives. This mirrors the original logparse stub's intent (top 3).
const topPickCount = 3

// Recommend ranks every card in packCardIDs by GIHWR descending and
// returns a Recommendations populated with plain-English Reason strings.
//
//   - format is the draft format string (e.g. "PremierDraft") forwarded to
//     ratings.GIHWR; pass "" when format is unknown.
//   - pool is the list of card IDs already in the player's draft pool
//     (previously picked cards). Used for pool-color derivation and
//     archetype detection.
//   - packCardIDs is the current pack being offered.
//   - ratings / cards satisfy the same interfaces pickquality.Analyze uses;
//     nil values produce graceful N/A results.
//   - meta satisfies draftalgo.CardMetaLookup; nil produces graceful
//     Phase-A-level results without crashing.
func Recommend(
	format string,
	pool []string,
	packCardIDs []string,
	ratings draftalgo.RatingsLookup,
	cards draftalgo.CardLookup,
	meta draftalgo.CardMetaLookup,
) Recommendations {
	if len(packCardIDs) == 0 {
		return Recommendations{}
	}

	ranked := rank.ByGIHWR(format, packCardIDs, ratings, cards)

	// Detect whether any card in the pack has real rating data.
	hasAnyRatings := false
	for _, r := range ranked {
		if r.HasGIHWR {
			hasAnyRatings = true
			break
		}
	}

	// Phase B: derive pool colors fresh from pool card metas.
	// Re-derived on every call — never cached (ADR-047 §3 + Prof D).
	poolColors, colorFreq := derivePoolColorsWithFreq(format, pool, ratings, meta)
	isCommitted, color1, color2 := detectTwoColorCommitment(colorFreq)
	archetype := archetypeName(color1, color2)

	// Build a meta-lookup closure that gracefully falls back to zero
	// when meta is nil (Phase A caller path or cold cache).
	lookupMeta := func(id string) (draftalgo.CardMeta, bool) {
		if meta == nil {
			return draftalgo.CardMeta{}, false
		}
		return meta.CardMetaByID(id)
	}

	altCap := len(ranked) - topPickCount
	if altCap < 0 {
		altCap = 0
	}
	out := Recommendations{
		TopPicks:     make([]Recommendation, 0, topPickCount),
		Alternatives: make([]Recommendation, 0, altCap),
		PoolColors:   poolColors,
	}

	// The top-ranked card (rank-1) is needed for the "why not pick 2"
	// differentiator. Capture its meta once.
	var topMeta draftalgo.CardMeta
	var hasTopMeta bool
	var topGIHWR float64
	if len(ranked) > 0 {
		topMeta, hasTopMeta = lookupMeta(ranked[0].CardID)
		topGIHWR = ranked[0].GIHWR
	}

	for _, r := range ranked {
		cm, hasMeta := lookupMeta(r.CardID)

		rec := Recommendation{
			CardID:   r.CardID,
			CardName: r.CardName,
			GIHWR:    r.GIHWR,
			HasGIHWR: r.HasGIHWR,
		}

		// LowConfidence: <500 GIH games or nil sample (ADR-047 §2).
		rec.LowConfidence = isLowConfidence(cm.GIHCount)

		// Archetype: populated post-commitment, empty pre-commitment
		// (ADR-047 §3). Re-derived each call, never cached.
		if isCommitted {
			rec.Archetype = archetype
		}

		rec.Priority = priorityForRank(r.Rank, len(ranked))

		// Reason: Phase B adds color-fit, ALSA, and low-confidence
		// framing. Falls back to Phase A Reason when meta is absent.
		rec.Reason = buildReason(r, cm, hasMeta, isCommitted, poolColors, hasAnyRatings, len(packCardIDs))

		// WhyNotTopReason: only for picks 2+ in TopPicks, and only
		// when there is a meaningful gap (ADR-047 §5 + Prof).
		rank1 := len(out.TopPicks) == 0 // this card will be TopPick[0]
		if !rank1 && len(out.TopPicks) >= 1 && hasTopMeta {
			rec.WhyNotTopReason = buildWhyNotTop(r, cm, hasMeta, topMeta, topGIHWR, poolColors)
		}

		if len(out.TopPicks) < topPickCount {
			out.TopPicks = append(out.TopPicks, rec)
		} else {
			out.Alternatives = append(out.Alternatives, rec)
		}
	}

	return out
}

// ─── low_confidence ───────────────────────────────────────────────────────

// isLowConfidence returns true when the GIH sample is nil or below the
// lowConfidenceGIHFloor constant (ADR-047 §2).
func isLowConfidence(count *int) bool {
	return count == nil || *count < lowConfidenceGIHFloor
}

// ─── pool-color derivation ────────────────────────────────────────────────

// derivePoolColorsWithFreq counts color occurrences across pool card metas
// and returns:
//   - poolColors: dominant colors sorted by frequency descending (unique, deduped)
//   - freq: the full frequency map used by detectTwoColorCommitment
//
// Tie-breaking is deterministic via a 3-key comparator (#1397):
//  1. frequency DESC (primary)
//  2. quality_sum DESC — sum of GIHWR across the color's pool cards (semantic tie-break)
//  3. WUBRG position ASC — canonical fallback when quality data is absent or equal
//
// Re-derived from scratch on every call — never cached (ADR-047 §3 + Prof D).
func derivePoolColorsWithFreq(
	format string,
	pool []string,
	ratings draftalgo.RatingsLookup,
	meta draftalgo.CardMetaLookup,
) ([]string, map[string]int) {
	if meta == nil || len(pool) == 0 {
		return nil, nil
	}
	freq := make(map[string]int)
	qualitySum := make(map[string]float64)
	for _, id := range pool {
		cm, ok := meta.CardMetaByID(id)
		if !ok {
			continue
		}
		for _, c := range cm.Colors {
			if c == "" {
				continue
			}
			freq[c]++
			if ratings != nil {
				if gihwr, ok := ratings.GIHWR(id, format); ok {
					qualitySum[c] += gihwr
				}
			}
		}
	}
	// Build a slice and sort with a deterministic 3-key comparator.
	// Pool is ≤42 cards so N is tiny; sort.SliceStable is stable and correct.
	type cv struct {
		c string
		n int
	}
	sorted := make([]cv, 0, len(freq))
	for c, n := range freq {
		sorted = append(sorted, cv{c, n})
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].n != sorted[j].n {
			return sorted[i].n > sorted[j].n // freq DESC
		}
		qi := qualitySum[sorted[i].c]
		qj := qualitySum[sorted[j].c]
		if qi != qj {
			return qi > qj // quality_sum DESC
		}
		// WUBRG fallback ASC — non-WUBRG colors sort after position 4.
		oi, oki := wubrgOrder[sorted[i].c]
		oj, okj := wubrgOrder[sorted[j].c]
		if !oki {
			oi = len(wubrgOrder)
		}
		if !okj {
			oj = len(wubrgOrder)
		}
		return oi < oj
	})
	out := make([]string, 0, len(sorted))
	for _, cv := range sorted {
		out = append(out, cv.c)
	}
	return out, freq
}

// detectTwoColorCommitment returns true when the pool frequency map shows
// two distinct colors each with at least colorCommitmentThreshold cards.
// Returns the two dominant colors (color1 has the most cards, color2 the
// second most).
// ADR-047 §3 + Prof B: a 5G/2B pool is Simic-leaning, not Mono-Green —
// we detect leaning two-color, not collapsing to the single leading color.
//
// Tie-breaking uses a 2-key comparator (#1397): freq DESC then WUBRG ASC.
// Quality-sum is intentionally absent here — the archetype label needs
// determinism, not quality-weighting. See plan §3c for the accepted tradeoff.
func detectTwoColorCommitment(freq map[string]int) (committed bool, color1, color2 string) {
	if len(freq) < 2 {
		return false, "", ""
	}
	type cv struct {
		c string
		n int
	}
	top := make([]cv, 0, len(freq))
	for c, n := range freq {
		top = append(top, cv{c, n})
	}
	// Sort with a deterministic 2-key comparator: freq DESC, WUBRG ASC.
	sort.SliceStable(top, func(i, j int) bool {
		if top[i].n != top[j].n {
			return top[i].n > top[j].n // freq DESC
		}
		oi, oki := wubrgOrder[top[i].c]
		oj, okj := wubrgOrder[top[j].c]
		if !oki {
			oi = len(wubrgOrder)
		}
		if !okj {
			oj = len(wubrgOrder)
		}
		return oi < oj // WUBRG ASC
	})
	if len(top) < 2 {
		return false, "", ""
	}
	// Both top two colors must meet the threshold.
	if top[0].n >= colorCommitmentThreshold && top[1].n >= colorCommitmentThreshold {
		return true, top[0].c, top[1].c
	}
	return false, "", ""
}

// archetypeName returns a two-color archetype label from the two committed
// colors. Uses the Ravnica guild names for canonical WUBRGC two-color pairs.
func archetypeName(c1, c2 string) string {
	if c1 == "" || c2 == "" {
		return ""
	}
	key := canonicalColorPair(c1, c2)
	names := map[string]string{
		"WU": "Azorius", "UB": "Dimir", "BR": "Rakdos", "RG": "Gruul", "GW": "Selesnya",
		"WB": "Orzhov", "UR": "Izzet", "BG": "Golgari", "RW": "Boros", "GU": "Simic",
	}
	if name, ok := names[key]; ok {
		return name
	}
	// Fallback for non-standard pairs
	return fmt.Sprintf("%s/%s", c1, c2)
}

// canonicalColorPair returns the WUBRG-ordered two-color string for any
// combination of two color chars so the archetype map lookup is order-independent.
func canonicalColorPair(c1, c2 string) string {
	order := "WUBRG"
	i1 := strings.Index(order, c1)
	i2 := strings.Index(order, c2)
	if i1 < 0 {
		i1 = len(order) // non-WUBRG colors sort last
	}
	if i2 < 0 {
		i2 = len(order)
	}
	if i1 <= i2 {
		return c1 + c2
	}
	return c2 + c1
}

// ─── Reason construction ──────────────────────────────────────────────────

// buildReason builds a plain-English Reason string for a card, incorporating
// Phase B signals: color-fit, ALSA scarcity, and low-confidence framing.
// Never contains a "%" literal (Prof gate + existing test contract).
func buildReason(
	r rank.Card,
	cm draftalgo.CardMeta,
	hasMeta bool,
	isCommitted bool,
	poolColors []string,
	hasAnyRatings bool,
	packSize int,
) string {
	// Graceful N/A when no ratings are available for this format/set.
	if !hasAnyRatings {
		return "No rating data available for this set"
	}
	if !r.HasGIHWR {
		return "No rating data for this card"
	}

	// Low-confidence framing takes precedence over pack-size shortcircuit —
	// communicate uncertainty, not weakness (ADR-047 §5, Prof). Rares/mythics
	// fire this marker by default (small samples) — framing must read as
	// "early format data", not "questionable card." (Prof constraint E)
	if isLowConfidence(cm.GIHCount) {
		return lowConfidenceReason(r)
	}

	// Single card in the pack after low-confidence check — trivially the
	// only option unless we have color/ALSA context to add.
	if packSize == 1 && !hasMeta {
		return "Only card in the pack"
	}

	// Land / mana-producer: no color-fit penalty — just standalone signal.
	// ADR-047 §5 + Prof constraint C.
	if hasMeta && cm.IsLand {
		return standalonePowerReason(r)
	}

	// Color-fit + splash reasoning when pool is committed.
	if hasMeta && isCommitted && len(poolColors) > 0 {
		colorReason := colorFitReason(r, cm, poolColors)
		if colorReason != "" {
			// Append ALSA context if the card tables late.
			alsaCtx := alsaContext(cm.ALSA)
			if alsaCtx != "" {
				return colorReason + "; " + alsaCtx
			}
			return colorReason
		}
	}

	// ALSA scarcity signal for any card (committed or not).
	if hasMeta && cm.ALSA > 0 {
		alsaCtx := alsaContext(cm.ALSA)
		if alsaCtx != "" {
			return standalonePowerReason(r) + "; " + alsaCtx
		}
	}

	// Pre-commitment or no meta: standalone-power reason.
	return standalonePowerReason(r)
}

// lowConfidenceReason returns a Reason string for low-sample-size cards.
// Framing communicates uncertainty / early format, not weakness.
// ADR-047 §5 + Prof constraint E (rares/mythics especially).
func lowConfidenceReason(r rank.Card) string {
	switch {
	case r.Rank == 1:
		return "Best pick in pack — small sample, treat as a signal"
	case r.Rank <= 3:
		return "Strong standalone pick — limited data, early format"
	default:
		return "Solid pick — small sample size, treat as a signal"
	}
}

// standalonePowerReason returns a reason based purely on pack rank,
// with no color or pool context. Used pre-commitment and for lands.
func standalonePowerReason(r rank.Card) string {
	switch {
	case r.Rank == 1:
		return "Best pick in the pack"
	case r.Rank <= 3:
		return "Strong standalone pick"
	case r.Rank <= 5:
		return "Solid pick"
	case r.Rank <= 8:
		return "Situational pick"
	default:
		return "Low-rated for this format"
	}
}

// colorFitReason returns a color-context reason string.
// Returns "" when no color-specific framing applies.
// ADR-047 §5: high-GIHWR off-color card gets splash-consideration path;
// off-color penalty MUST NOT apply to lands (caller must check IsLand first).
func colorFitReason(r rank.Card, cm draftalgo.CardMeta, poolColors []string) string {
	// Check if any of the card's colors match the pool.
	isOnColor := false
	for _, cardColor := range cm.Colors {
		for _, poolColor := range poolColors {
			if cardColor == poolColor {
				isOnColor = true
				break
			}
		}
		if isOnColor {
			break
		}
	}

	if isOnColor {
		switch {
		case r.Rank == 1:
			return "Best pick in the pack — fits your colors"
		case r.Rank <= 3:
			return "On-color for your pool — strong pick"
		default:
			return "On-color for your pool"
		}
	}

	// Off-color: check for splash worthiness (ADR-047 §5).
	if r.HasGIHWR && r.GIHWR >= splashHighGIHWRFloor {
		// High-GIHWR off-color bomb → splash consideration path.
		return "Off-color but a strong splash consideration"
	}

	// Quality modifier (#1400): top-10% GIHWR cards bypass full suppression even
	// when the pool is committed. They still surface in the recommendation list
	// with standalone-power framing rather than the off-color penalty.
	// topQualityGIHWRFloor < splashHighGIHWRFloor so this fires only for the
	// "strong but not bomb-level" off-color cards that the old code silently killed.
	if r.HasGIHWR && r.GIHWR >= topQualityGIHWRFloor {
		return standalonePowerReason(r)
	}

	// Off-color, not splash-worthy, not top-quality.
	return "Not your colors"
}

// alsaContext returns an ALSA-based scarcity addendum.
// Never uses probability framing. ADR-047 §5: "frequently available late"
// / "typically taken early" — no P(wheels) percentage.
func alsaContext(alsa float64) string {
	switch {
	case alsa >= highALSAFloor:
		return "frequently available late"
	case alsa > 0 && alsa <= 2.5:
		return "typically taken early"
	default:
		return ""
	}
}

// ─── "Why not pick 2" differentiator ─────────────────────────────────────

// buildWhyNotTop returns a short phrase explaining why this card ranks
// below the top pick. Only fires on meaningful gaps (color, archetype fit,
// or sample-confidence differential); returns "" for noise-level differences.
// ADR-047 §5 + Prof: one sentence max; no raw numbers.
func buildWhyNotTop(
	r rank.Card,
	cm draftalgo.CardMeta,
	hasMeta bool,
	topMeta draftalgo.CardMeta,
	topGIHWR float64,
	poolColors []string,
) string {
	// Color mismatch is always a meaningful gap when pool is committed.
	if hasMeta && len(poolColors) > 0 {
		cardOnColor := colorIsInPool(cm.Colors, poolColors)
		topOnColor := colorIsInPool(topMeta.Colors, poolColors)

		if topOnColor && !cardOnColor {
			return "Off-color vs. the top pick"
		}
		if !topOnColor && cardOnColor {
			// Unusual: pick 2 is on-color but ranked below pick 1 (pick 1 is a bomb)
			// — no differentiator needed; the GIHWR gap explains it.
			return ""
		}
	}

	// Sample-confidence differential: top pick has data, this doesn't.
	if !isLowConfidence(topMeta.GIHCount) && isLowConfidence(cm.GIHCount) {
		return "Top pick has more rating data"
	}

	// GIHWR gap: only fire on a meaningful gap (≥5 percentage points).
	if r.HasGIHWR && topGIHWR > 0 {
		gap := topGIHWR - r.GIHWR
		if gap >= 5.0 {
			return "Lower win rate than the top pick"
		}
	}

	// Marginal / noise-level gap — stay quiet (Prof: no filler).
	return ""
}

// colorIsInPool returns true when any color in the card's color slice
// matches any pool color.
func colorIsInPool(cardColors, poolColors []string) bool {
	for _, cc := range cardColors {
		for _, pc := range poolColors {
			if cc == pc {
				return true
			}
		}
	}
	return false
}

// ─── priority derivation (unchanged from Phase A) ─────────────────────────

// priorityForRank converts a 1-based pack rank to a 1–5 priority value.
// Rank 1 → 5 (highest), rank 2–3 → 4, rank 4–5 → 3, rank 6–8 → 2,
// rank 9+ → 1. Matches the original stub's 1-5 scale.
func priorityForRank(rank, _ int) int {
	switch {
	case rank == 1:
		return 5
	case rank <= 3:
		return 4
	case rank <= 5:
		return 3
	case rank <= 8:
		return 2
	default:
		return 1
	}
}

// reasonForRank produces a plain-English reason string for a card's rank.
// Retained for backward compat with Phase A tests that call it via the
// Recommend public API. New call sites should use buildReason instead.
//
// Contract: the returned string MUST NOT contain a "%" character.
func reasonForRank(cardRank, packSize int, hasGIHWR, anyRatings bool) string {
	if !anyRatings {
		return "No rating data available for this set"
	}
	if packSize == 1 {
		return "Only card in the pack"
	}
	if !hasGIHWR {
		return "No rating data for this card"
	}
	switch {
	case cardRank == 1:
		return "Best pick in the pack"
	case cardRank <= 3:
		return "Strong standalone pick"
	case cardRank <= 5:
		return "Solid pick"
	case cardRank <= 8:
		return "Situational pick"
	default:
		return "Low-rated for this format"
	}
}
