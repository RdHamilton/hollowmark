package seventeenlands_test

import (
	"encoding/json"
	"testing"

	"github.com/RdHamilton/vault-mtg/services/sync/internal/seventeenlands"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realFeedSample is a verbatim excerpt of the 17Lands /color_ratings/data payload
// as observed in production. It contains:
//   - summary rows where short_name is a JSON number (1, 2, 3, 4, 5)
//   - summary rows where short_name is a string with a "+" suffix ("1+", "2+", "All")
//   - non-summary rows where short_name is a plain string ("W", "WU", "WUBRG")
//
// This is the shape that triggered the unmarshal error:
//
//	cannot unmarshal number into Go struct field ColorRating.short_name
const realFeedSample = `[
  {"is_summary":true,  "color_name":"Mono-color",           "short_name":1,     "wins":1352,   "games":2440},
  {"is_summary":false, "color_name":"Mono-White",            "short_name":"W",   "wins":658,    "games":1121},
  {"is_summary":false, "color_name":"Mono-Blue",             "short_name":"U",   "wins":216,    "games":415},
  {"is_summary":true,  "color_name":"Mono-color + Splash",   "short_name":"1+",  "wins":4969,   "games":8618},
  {"is_summary":false, "color_name":"Mono-White + Splash",   "short_name":"W+",  "wins":2178,   "games":3553},
  {"is_summary":true,  "color_name":"Two-color",             "short_name":2,     "wins":297332, "games":534086},
  {"is_summary":false, "color_name":"Azorius (WU)",          "short_name":"WU",  "wins":10434,  "games":19758},
  {"is_summary":false, "color_name":"Dimir (UB)",            "short_name":"UB",  "wins":20298,  "games":37417},
  {"is_summary":true,  "color_name":"Two-color + Splash",    "short_name":"2+",  "wins":36000,  "games":65000},
  {"is_summary":true,  "color_name":"Three-color",           "short_name":3,     "wins":12000,  "games":22000},
  {"is_summary":true,  "color_name":"Four-color",            "short_name":4,     "wins":3000,   "games":6000},
  {"is_summary":true,  "color_name":"Five-color",            "short_name":5,     "wins":500,    "games":1000},
  {"is_summary":false, "color_name":"WUBRG",                 "short_name":"WUBRG","wins":500,   "games":1000},
  {"is_summary":true,  "color_name":"All",                   "short_name":"All", "wins":999999, "games":1800000}
]`

// TestColorRating_UnmarshalJSON_MixedShortName verifies that the full live feed
// shape — integer short_name on some summary rows, string on all others — unmarshals
// without error and yields the expected string values.
func TestColorRating_UnmarshalJSON_MixedShortName(t *testing.T) {
	var ratings []seventeenlands.ColorRating
	err := json.Unmarshal([]byte(realFeedSample), &ratings)
	require.NoError(t, err, "unmarshal must succeed for the real feed payload shape")
	require.Len(t, ratings, 14)

	cases := []struct {
		idx       int
		wantShort string
		wantSumm  bool
	}{
		{0, "1", true},       // integer 1 → "1"
		{1, "W", false},      // string "W" preserved
		{2, "U", false},      // string "U" preserved
		{3, "1+", true},      // string "1+" preserved (is_summary but already a string)
		{4, "W+", false},     // string "W+" preserved
		{5, "2", true},       // integer 2 → "2"
		{6, "WU", false},     // string "WU" preserved
		{7, "UB", false},     // string "UB" preserved
		{8, "2+", true},      // string "2+" preserved
		{9, "3", true},       // integer 3 → "3"
		{10, "4", true},      // integer 4 → "4"
		{11, "5", true},      // integer 5 → "5"
		{12, "WUBRG", false}, // string "WUBRG" preserved
		{13, "All", true},    // string "All" preserved
	}

	for _, tc := range cases {
		r := ratings[tc.idx]
		assert.Equal(t, tc.wantShort, r.ShortName,
			"index %d: ShortName mismatch", tc.idx)
		assert.Equal(t, tc.wantSumm, r.IsSummary,
			"index %d: IsSummary mismatch", tc.idx)
	}
}

// TestColorRating_UnmarshalJSON_NonSummaryFieldsPopulated verifies that all
// non-short_name fields are populated correctly from the JSON payload.
func TestColorRating_UnmarshalJSON_NonSummaryFieldsPopulated(t *testing.T) {
	const payload = `[{"is_summary":false,"color_name":"Azorius (WU)","short_name":"WU","wins":10434,"games":19758}]`

	var ratings []seventeenlands.ColorRating
	require.NoError(t, json.Unmarshal([]byte(payload), &ratings))
	require.Len(t, ratings, 1)

	r := ratings[0]
	assert.Equal(t, "Azorius (WU)", r.ColorName)
	assert.Equal(t, "WU", r.ShortName)
	assert.Equal(t, 10434, r.Wins)
	assert.Equal(t, 19758, r.Games)
	assert.False(t, r.IsSummary)
	assert.InDelta(t, 0.5281, r.WinRate(), 0.001)
}

// TestColorRating_UnmarshalJSON_IntegerSummaryFieldsPopulated verifies that a
// summary row with an integer short_name has its other fields decoded correctly.
func TestColorRating_UnmarshalJSON_IntegerSummaryFieldsPopulated(t *testing.T) {
	const payload = `[{"is_summary":true,"color_name":"Two-color","short_name":2,"wins":297332,"games":534086}]`

	var ratings []seventeenlands.ColorRating
	require.NoError(t, json.Unmarshal([]byte(payload), &ratings))
	require.Len(t, ratings, 1)

	r := ratings[0]
	assert.Equal(t, "Two-color", r.ColorName)
	assert.Equal(t, "2", r.ShortName)
	assert.Equal(t, 297332, r.Wins)
	assert.Equal(t, 534086, r.Games)
	assert.True(t, r.IsSummary)
}

// TestColorRating_WinRate verifies the computed win-rate helper for both the zero
// and non-zero denominator cases.
func TestColorRating_WinRate(t *testing.T) {
	t.Run("non-zero games", func(t *testing.T) {
		r := seventeenlands.ColorRating{Wins: 658, Games: 1121}
		assert.InDelta(t, 0.587, r.WinRate(), 0.001)
	})

	t.Run("zero games returns 0", func(t *testing.T) {
		r := seventeenlands.ColorRating{Wins: 0, Games: 0}
		assert.Equal(t, 0.0, r.WinRate())
	})
}
