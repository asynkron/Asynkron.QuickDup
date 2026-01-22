package main

import "testing"

func TestNormalizedIndentScoreBalancedFullSimilarity(t *testing.T) {
	strategy := &NormalizedIndentStrategy{}
	entries := []Entry{
		NewNormalizedIndentEntry(1, "if"),
		NewNormalizedIndentEntry(0, "return"),
		NewNormalizedIndentEntry(-1, "}"),
	}

	score := strategy.Score(entries, 1.0)
	if score != 3 {
		t.Fatalf("expected score 3, got %d", score)
	}
}

func TestNormalizedIndentScoreImbalancePenalty(t *testing.T) {
	strategy := &NormalizedIndentStrategy{}
	entries := []Entry{
		NewNormalizedIndentEntry(-1, "}"),
		NewNormalizedIndentEntry(0, "return"),
	}

	score := strategy.Score(entries, 1.0)
	if score != 1 {
		t.Fatalf("expected score 1, got %d", score)
	}
}

func TestNormalizedIndentScoreLowSimilarityDropsWordBonus(t *testing.T) {
	strategy := &NormalizedIndentStrategy{}
	entries := []Entry{
		NewNormalizedIndentEntry(1, "if"),
		NewNormalizedIndentEntry(-1, "}"),
	}

	score := strategy.Score(entries, 0.4)
	if score != 0 {
		t.Fatalf("expected score 0, got %d", score)
	}
}

func TestWordIndentScoreSizeBonusAndSimilarityCurve(t *testing.T) {
	strategy := &WordIndentStrategy{}
	words := []string{
		"if",
		"for",
		"switch",
		"return",
		"case",
		"else",
		"break",
		"continue",
		"goto",
		"defer",
	}
	entries := make([]Entry, 0, 20)
	for i := 0; i < 20; i++ {
		entries = append(entries, NewWordIndentEntry(0, words[i%len(words)]))
	}

	score := strategy.Score(entries, 0.85)
	if score != 4 {
		t.Fatalf("expected score 4, got %d", score)
	}
}
