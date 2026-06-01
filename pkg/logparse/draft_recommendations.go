package logparse

// DraftRecommendation represents a recommended pick for a draft.
//
// Deprecated: The recommendation logic has been relocated and implemented
// in pkg/draftalgo/recommend. The daemon's current-pack handler
// (services/daemon/internal/localapi/drafts.go) calls recommend.Recommend
// directly. This type is retained for package-level compatibility only;
// the stub body of GetDraftRecommendations below is no longer used.
type DraftRecommendation struct {
	CardID    string // Arena 2026.58+: string card ID
	Priority  int    // 1-5, where 5 is highest priority
	Reason    string // Explanation for the recommendation
	Archetype string // Suggested archetype
}

// DraftRecommendations represents recommendations for a draft pick.
//
// Deprecated: See DraftRecommendation.
type DraftRecommendations struct {
	TopPicks     []DraftRecommendation // Top 3 recommended picks
	Alternatives []DraftRecommendation // Alternative picks for different archetypes
}

// GetDraftRecommendations is a stub retained for backward compatibility.
//
// Deprecated: Use pkg/draftalgo/recommend.Recommend instead. This function
// is not called by any production path (confirmed zero callers at the time
// of MH-ML1, vmt-t#399). The real recommendation algorithm now lives in
// pkg/draftalgo/recommend and is wired into the daemon's current-pack
// handler. This stub will be removed in a follow-on cleanup ticket.
func GetDraftRecommendations(packCards []string, _ []DraftPick) DraftRecommendations {
	recommendations := DraftRecommendations{
		TopPicks:     []DraftRecommendation{},
		Alternatives: []DraftRecommendation{},
	}

	if len(packCards) == 0 {
		return recommendations
	}

	for i, cardID := range packCards {
		if i >= 3 {
			break
		}

		priority := 5 - i
		reason := "Basic recommendation"
		if i == 0 {
			reason = "First pick in pack"
		}

		recommendations.TopPicks = append(recommendations.TopPicks, DraftRecommendation{
			CardID:   cardID,
			Priority: priority,
			Reason:   reason,
		})
	}

	return recommendations
}
