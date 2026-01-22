package main

import (
	"fmt"
	"testing"
)

func TestWordOnlyStrategy_Score_SimilarityScaling(t *testing.T) {
	t.Parallel()

	s := &WordOnlyStrategy{}

	entries := make([]Entry, 80)
	for i := range entries {
		entries[i] = NewWordOnlyEntry(fmt.Sprintf("w%d", i))
	}

	// adjustedSim = similarity*2 - 1
	// simFactor = adjustedSim^3
	// score = int(uniqueWords*simFactor) + len(entries)/20
	if got := s.Score(entries, 1.0); got != 84 { // 80*1 + 4
		t.Fatalf("score at 1.0 = %d, want 84", got)
	}
	if got := s.Score(entries, 0.75); got != 14 { // 80*0.125 + 4
		t.Fatalf("score at 0.75 = %d, want 14", got)
	}
	if got := s.Score(entries, 0.5); got != 4 { // 80*0 + 4
		t.Fatalf("score at 0.5 = %d, want 4", got)
	}
}

func TestWordIndentStrategy_Score_ImbalancePenalty(t *testing.T) {
	t.Parallel()

	s := &WordIndentStrategy{}

	balanced := []Entry{
		NewWordIndentEntry(4, "a"),
		NewWordIndentEntry(0, "b"),
		NewWordIndentEntry(-4, "c"),
	}
	if got := s.Score(balanced, 1.0); got != 3 {
		t.Fatalf("balanced score = %d, want 3", got)
	}

	unbalanced := []Entry{
		NewWordIndentEntry(4, "a"),
		NewWordIndentEntry(0, "b"),
		NewWordIndentEntry(0, "c"),
	}
	if got := s.Score(unbalanced, 1.0); got != 0 {
		t.Fatalf("unbalanced score = %d, want 0", got)
	}
}

func TestNormalizedIndentStrategy_Score_ImbalancePenalty(t *testing.T) {
	t.Parallel()

	s := &NormalizedIndentStrategy{}

	balanced := []Entry{
		NewNormalizedIndentEntry(1, "a"),
		NewNormalizedIndentEntry(0, "b"),
		NewNormalizedIndentEntry(-1, "c"),
	}
	if got := s.Score(balanced, 1.0); got != 3 {
		t.Fatalf("balanced score = %d, want 3", got)
	}

	unbalanced := []Entry{
		NewNormalizedIndentEntry(1, "a"),
		NewNormalizedIndentEntry(0, "b"),
		NewNormalizedIndentEntry(0, "c"),
	}
	if got := s.Score(unbalanced, 1.0); got != 2 {
		t.Fatalf("unbalanced score = %d, want 2", got)
	}
}

func TestInlineableStrategy_Score_MatchesOnlyInlineableShapes(t *testing.T) {
	t.Parallel()

	s := &InlineableStrategy{}

	inlineable := []Entry{
		&InlineableEntry{Word: "public"},
		&InlineableEntry{Word: "{"},
		&InlineableEntry{Word: "return"},
		&InlineableEntry{Word: "}"},
	}
	if got := s.Score(inlineable, 1.0); got != 100 {
		t.Fatalf("inlineable score = %d, want 100", got)
	}

	notInlineable := []Entry{
		&InlineableEntry{Word: "public"},
		&InlineableEntry{Word: "if"},
		&InlineableEntry{Word: "return"},
		&InlineableEntry{Word: "}"},
	}
	if got := s.Score(notInlineable, 1.0); got != 0 {
		t.Fatalf("non-inlineable score = %d, want 0", got)
	}
}
