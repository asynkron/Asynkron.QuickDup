package quickdup

import (
	"fmt"
	"testing"
)

func TestWordOnlyScoreUsesSimilarityCurveAndLengthBonus(t *testing.T) {
	t.Parallel()

	entries := make([]Entry, 80)
	for i := range entries {
		entries[i] = NewWordOnlyEntry(fmt.Sprintf("word%d", i))
	}
	strategy := &WordOnlyStrategy{}

	tests := []struct {
		name       string
		similarity float64
		expected   int
	}{
		{name: "full similarity", similarity: 1, expected: 84},
		{name: "default threshold", similarity: 0.75, expected: 14},
		{name: "curve floor", similarity: 0.5, expected: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := strategy.Score(entries, test.similarity); got != test.expected {
				t.Fatalf("Score(similarity=%v) = %d, want %d", test.similarity, got, test.expected)
			}
		})
	}
}

func TestIndentScoresApplyStrategySpecificImbalance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		strategy   Strategy
		balanced   []Entry
		unbalanced []Entry
		expected   int
	}{
		{
			name:     "word indent",
			strategy: &WordIndentStrategy{},
			balanced: []Entry{
				NewWordIndentEntry(4, "if"),
				NewWordIndentEntry(0, "return"),
				NewWordIndentEntry(-4, "}"),
			},
			unbalanced: []Entry{
				NewWordIndentEntry(4, "if"),
				NewWordIndentEntry(0, "return"),
				NewWordIndentEntry(0, "}"),
			},
			expected: 0,
		},
		{
			name:     "normalized indent",
			strategy: &NormalizedIndentStrategy{},
			balanced: []Entry{
				NewNormalizedIndentEntry(1, "if"),
				NewNormalizedIndentEntry(0, "return"),
				NewNormalizedIndentEntry(-1, "}"),
			},
			unbalanced: []Entry{
				NewNormalizedIndentEntry(1, "if"),
				NewNormalizedIndentEntry(0, "return"),
				NewNormalizedIndentEntry(0, "}"),
			},
			expected: 2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.strategy.Score(test.balanced, 1); got != 3 {
				t.Fatalf("balanced Score() = %d, want 3", got)
			}
			if got := test.strategy.Score(test.unbalanced, 1); got != test.expected {
				t.Fatalf("unbalanced Score() = %d, want %d", got, test.expected)
			}
		})
	}
}

func TestIndentScoresUseSimilarityCurveAndLengthBonus(t *testing.T) {
	t.Parallel()

	wordIndentEntries := make([]Entry, 20)
	normalizedIndentEntries := make([]Entry, 20)
	for i := range wordIndentEntries {
		word := fmt.Sprintf("word%d", i%10)
		wordIndentEntries[i] = NewWordIndentEntry(0, word)
		normalizedIndentEntries[i] = NewNormalizedIndentEntry(0, word)
	}

	tests := []struct {
		name     string
		strategy Strategy
		entries  []Entry
	}{
		{name: "word indent", strategy: &WordIndentStrategy{}, entries: wordIndentEntries},
		{name: "normalized indent", strategy: &NormalizedIndentStrategy{}, entries: normalizedIndentEntries},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := test.strategy.Score(test.entries, 0.85); got != 4 {
				t.Fatalf("Score() = %d, want 4 from the similarity curve and length bonus", got)
			}
		})
	}
}

func TestInlineableScoreRecognizesOnlyInlineableShapes(t *testing.T) {
	t.Parallel()

	strategy := &InlineableStrategy{}
	inlineable := []Entry{
		NewFirstWordEntry("public"),
		NewFirstWordEntry("{"),
		NewFirstWordEntry("return"),
		NewFirstWordEntry("}"),
	}
	notInlineable := []Entry{
		NewFirstWordEntry("public"),
		NewFirstWordEntry("if"),
		NewFirstWordEntry("return"),
		NewFirstWordEntry("}"),
	}

	if got := strategy.Score(inlineable, 1); got != 100 {
		t.Fatalf("inlineable Score() = %d, want 100", got)
	}
	if got := strategy.Score(inlineable, 0.75); got != 75 {
		t.Fatalf("inlineable default-threshold Score() = %d, want 75", got)
	}
	if got := strategy.Score(notInlineable, 1); got != 0 {
		t.Fatalf("non-inlineable Score() = %d, want 0", got)
	}
}
