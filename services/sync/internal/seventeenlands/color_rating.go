package seventeenlands

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// ColorRating holds per-color-combination statistics returned by the 17Lands
// /color_ratings/data endpoint.
//
// The API returns raw wins/games integers, not a pre-computed win rate. The
// endpoint also includes summary rows (is_summary=true) with an integer short_name
// that callers must filter out before persisting. See WinRate() for the computed
// ratio and the plan on vault-mtg-tickets#46 for the full root-cause analysis.
//
// short_name is mixed-type in the feed: integer (e.g. 1, 2, 3) for some summary
// rows, string (e.g. "W", "WU", "1+", "All") for all other rows. UnmarshalJSON
// coerces the integer form to its string representation so callers always see a
// string value.
type ColorRating struct {
	ColorName string `json:"color_name"` // e.g. "Mono-White"
	ShortName string `json:"short_name"` // e.g. "W", "WU" — canonical MTG color key; populated by UnmarshalJSON
	Wins      int    `json:"wins"`
	Games     int    `json:"games"`
	IsSummary bool   `json:"is_summary"`
}

// colorRatingWire is an intermediate type used by UnmarshalJSON so the standard
// decoder handles every field except short_name.
type colorRatingWire struct {
	ColorName string          `json:"color_name"`
	ShortName json.RawMessage `json:"short_name"`
	Wins      int             `json:"wins"`
	Games     int             `json:"games"`
	IsSummary bool            `json:"is_summary"`
}

// UnmarshalJSON handles the mixed-type short_name field. The 17Lands feed sends
// it as a JSON number for some summary rows (e.g. 1, 2) and as a JSON string for
// all other rows (e.g. "W", "WU", "1+", "All"). Both are coerced to string.
func (c *ColorRating) UnmarshalJSON(data []byte) error {
	var wire colorRatingWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	c.ColorName = wire.ColorName
	c.Wins = wire.Wins
	c.Games = wire.Games
	c.IsSummary = wire.IsSummary

	if len(wire.ShortName) == 0 {
		return nil
	}

	// Try string first (the common case).
	var s string
	if err := json.Unmarshal(wire.ShortName, &s); err == nil {
		c.ShortName = s
		return nil
	}

	// Fall back to number — coerce to its decimal string representation.
	var n json.Number
	if err := json.Unmarshal(wire.ShortName, &n); err == nil {
		// Use integer representation when the value has no fractional part.
		if i, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
			c.ShortName = strconv.FormatInt(i, 10)
			return nil
		}
		c.ShortName = n.String()
		return nil
	}

	return fmt.Errorf("color_rating: cannot unmarshal short_name %s", wire.ShortName)
}

// WinRate returns the computed win rate (wins/games). Returns 0 when Games == 0
// to avoid division by zero.
func (c ColorRating) WinRate() float64 {
	if c.Games == 0 {
		return 0
	}
	return float64(c.Wins) / float64(c.Games)
}
