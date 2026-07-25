package quickdup

import (
	"fmt"
	"math/rand"
	"testing"
)

// oldTokenSimilarity is the pre-hoist implementation, kept here only as a
// differential reference so the optimized version can be proven equivalent.
func oldTokenSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	setA := make(map[string]bool)
	for _, t := range a {
		setA[t] = true
	}

	setB := make(map[string]bool)
	for _, t := range b {
		setB[t] = true
	}

	intersection := 0
	for t := range setA {
		if setB[t] {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func randTokens(rng *rand.Rand, maxLen, vocab int) []string {
	n := rng.Intn(maxLen + 1)
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("tok%d", rng.Intn(vocab))
	}
	return out
}

// TestTokenSimilarityMatchesPreHoist asserts the set-based implementation
// returns bit-identical results to the slice-based original, including the
// empty-input edge cases and duplicate-heavy inputs.
func TestTokenSimilarityMatchesPreHoist(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 20000; i++ {
		a := randTokens(rng, 12, 20)
		b := randTokens(rng, 12, 20)

		want := oldTokenSimilarity(a, b)
		got := tokenSimilarity(tokenSet(a), tokenSet(b))
		if got != want {
			t.Fatalf("case %d: a=%v b=%v: got %v, want %v", i, a, b, got, want)
		}
	}

	// Explicit edge cases the random sweep may under-sample.
	edges := [][2][]string{
		{nil, nil},
		{{}, {}},
		{{"x"}, nil},
		{nil, {"x"}},
		{{"x", "x", "x"}, {"x"}},
		{{"a", "b"}, {"b", "a"}},
		{{"a"}, {"b"}},
	}
	for i, e := range edges {
		want := oldTokenSimilarity(e[0], e[1])
		got := tokenSimilarity(tokenSet(e[0]), tokenSet(e[1]))
		if got != want {
			t.Fatalf("edge %d: a=%v b=%v: got %v, want %v", i, e[0], e[1], got, want)
		}
	}
}

// benchLocations builds a realistic pattern-location corpus for the pairwise
// clustering loop.
func benchLocations(n int) []PatternLocation {
	rng := rand.New(rand.NewSource(7))
	locs := make([]PatternLocation, n)
	for i := range locs {
		entries := make([]Entry, 6)
		for j := range entries {
			entries[j] = &FirstWordEntry{
				LineNumber: j,
				SourceLine: fmt.Sprintf("if cfg%d.enabled { handler%d.Invoke(ctx, arg%d) }",
					rng.Intn(40), rng.Intn(40), rng.Intn(40)),
			}
		}
		locs[i] = PatternLocation{Pattern: entries}
	}
	return locs
}

// oldPairLoop reproduces the pre-hoist pairwise cost: tokenize once, then
// rebuild both token maps inside every comparison.
func oldPairLoop(locations []PatternLocation, threshold float64) int {
	n := len(locations)
	tokenized := make([][]string, n)
	for i, loc := range locations {
		tokenized[i] = tokenizePattern(loc.Pattern)
	}
	hits := 0
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if oldTokenSimilarity(tokenized[i], tokenized[j]) >= threshold {
				hits++
			}
		}
	}
	return hits
}

// newPairLoop is the hoisted equivalent, matching the shipped implementation.
func newPairLoop(locations []PatternLocation, threshold float64) int {
	n := len(locations)
	sets := make([]map[string]bool, n)
	for i, loc := range locations {
		sets[i] = tokenSet(tokenizePattern(loc.Pattern))
	}
	hits := 0
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if tokenSimilarity(sets[i], sets[j]) >= threshold {
				hits++
			}
		}
	}
	return hits
}

// TestPairLoopsAgree pins that the A/B benchmark pair computes the same
// clustering decisions, so the timings below compare like with like.
func TestPairLoopsAgree(t *testing.T) {
	for _, n := range []int{50, 200} {
		locs := benchLocations(n)
		if got, want := newPairLoop(locs, 0.8), oldPairLoop(locs, 0.8); got != want {
			t.Fatalf("n=%d: new loop matched %d pairs, old matched %d", n, got, want)
		}
	}
}

func BenchmarkPairLoop(b *testing.B) {
	for _, n := range []int{100, 300} {
		locs := benchLocations(n)
		b.Run(fmt.Sprintf("old/n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				oldPairLoop(locs, 0.8)
			}
		})
		b.Run(fmt.Sprintf("new/n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				newPairLoop(locs, 0.8)
			}
		})
	}
}
