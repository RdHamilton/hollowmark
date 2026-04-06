package search

import (
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected []SearchTerm
	}{
		{
			name:     "empty query",
			query:    "",
			expected: nil,
		},
		{
			name:     "whitespace only",
			query:    "   ",
			expected: nil,
		},
		{
			name:  "plain single word",
			query: "bolt",
			expected: []SearchTerm{
				{Field: FieldAll, Value: "bolt"},
			},
		},
		{
			name:  "plain multi-word grouped as single term",
			query: "lightning bolt",
			expected: []SearchTerm{
				{Field: FieldAll, Value: "lightning bolt"},
			},
		},
		{
			name:  "type prefix",
			query: "t:creature",
			expected: []SearchTerm{
				{Field: FieldType, Value: "creature"},
			},
		},
		{
			name:  "oracle text prefix",
			query: "o:damage",
			expected: []SearchTerm{
				{Field: FieldText, Value: "damage"},
			},
		},
		{
			name:  "keyword prefix",
			query: "k:flying",
			expected: []SearchTerm{
				{Field: FieldKeyword, Value: "flying"},
			},
		},
		{
			name:  "uppercase prefix",
			query: "T:Creature",
			expected: []SearchTerm{
				{Field: FieldType, Value: "Creature"},
			},
		},
		{
			name:  "multiple prefixes are AND-ed",
			query: "t:creature o:damage",
			expected: []SearchTerm{
				{Field: FieldType, Value: "creature"},
				{Field: FieldText, Value: "damage"},
			},
		},
		{
			name:  "mixed prefix and bare words",
			query: "t:creature bolt",
			expected: []SearchTerm{
				{Field: FieldType, Value: "creature"},
				{Field: FieldAll, Value: "bolt"},
			},
		},
		{
			name:  "bare words before prefix are grouped",
			query: "lightning bolt t:instant",
			expected: []SearchTerm{
				{Field: FieldAll, Value: "lightning bolt"},
				{Field: FieldType, Value: "instant"},
			},
		},
		{
			name:  "bare words after prefix",
			query: "t:goblin lightning bolt",
			expected: []SearchTerm{
				{Field: FieldType, Value: "goblin"},
				{Field: FieldAll, Value: "lightning bolt"},
			},
		},
		{
			name:  "quoted value with oracle prefix",
			query: `o:"draw a card"`,
			expected: []SearchTerm{
				{Field: FieldText, Value: "draw a card"},
			},
		},
		{
			name:  "quoted value with type prefix",
			query: `t:"legendary creature"`,
			expected: []SearchTerm{
				{Field: FieldType, Value: "legendary creature"},
			},
		},
		{
			name:  "unclosed quote consumes to end",
			query: `o:"draw a card`,
			expected: []SearchTerm{
				{Field: FieldText, Value: "draw a card"},
			},
		},
		{
			name:  "unknown prefix treated as bare text",
			query: "x:foo",
			expected: []SearchTerm{
				{Field: FieldAll, Value: "x:foo"},
			},
		},
		{
			name:     "prefix with no value is discarded",
			query:    "t:",
			expected: nil,
		},
		{
			name:  "whitespace after colon is lenient",
			query: "t: creature",
			expected: []SearchTerm{
				{Field: FieldType, Value: "creature"},
			},
		},
		{
			name:  "multiple same prefixes",
			query: "t:creature t:enchantment",
			expected: []SearchTerm{
				{Field: FieldType, Value: "creature"},
				{Field: FieldType, Value: "enchantment"},
			},
		},
		{
			name:  "all three prefixes combined",
			query: "t:creature o:damage k:trample",
			expected: []SearchTerm{
				{Field: FieldType, Value: "creature"},
				{Field: FieldText, Value: "damage"},
				{Field: FieldKeyword, Value: "trample"},
			},
		},
		{
			name:  "complex mixed query",
			query: `t:creature o:"deals damage" bolt`,
			expected: []SearchTerm{
				{Field: FieldType, Value: "creature"},
				{Field: FieldText, Value: "deals damage"},
				{Field: FieldAll, Value: "bolt"},
			},
		},
		{
			name:  "bare words sandwiched between prefixes",
			query: "t:goblin lightning bolt o:haste",
			expected: []SearchTerm{
				{Field: FieldType, Value: "goblin"},
				{Field: FieldAll, Value: "lightning bolt"},
				{Field: FieldText, Value: "haste"},
			},
		},
		{
			name:     "prefix with empty quoted value is discarded",
			query:    `t:""`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.query)

			if len(result.Terms) != len(tt.expected) {
				t.Fatalf("expected %d terms, got %d: %+v", len(tt.expected), len(result.Terms), result.Terms)
			}

			for i, term := range result.Terms {
				if term.Field != tt.expected[i].Field {
					t.Errorf("term[%d].Field = %d, want %d", i, term.Field, tt.expected[i].Field)
				}
				if term.Value != tt.expected[i].Value {
					t.Errorf("term[%d].Value = %q, want %q", i, term.Value, tt.expected[i].Value)
				}
			}
		})
	}
}

func TestParsedQuery_IsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{"empty string", "", true},
		{"whitespace", "   ", true},
		{"has content", "bolt", false},
		{"has prefix", "t:creature", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.query)
			if result.IsEmpty() != tt.expected {
				t.Errorf("IsEmpty() = %v, want %v", result.IsEmpty(), tt.expected)
			}
		})
	}
}
