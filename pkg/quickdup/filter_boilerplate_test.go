package quickdup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// boilerplateTestPattern parses a tiny Go snippet through the real strategy
// pipeline so the returned entries carry the metadata that Score needs.
func boilerplateTestPattern(t *testing.T) []Entry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.go")
	src := "if x {\n\treturn true\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := parseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("snippet produced no entries")
	}
	return entries
}

func boilerplatePatternLocations(t *testing.T, n int) map[uint64][]PatternLocation {
	pattern := boilerplateTestPattern(t)
	patterns := make(map[uint64][]PatternLocation)
	for i := 0; i < n; i++ {
		patterns[0x1234] = append(patterns[0x1234], PatternLocation{
			Filename:  fmt.Sprintf("f%d.go", i),
			LineStart: 1,
			Pattern:   pattern,
		})
	}
	return patterns
}

func TestFilterPatternsSkipsHighFrequencyBoilerplate(t *testing.T) {
	prev := ActiveStrategy
	ActiveStrategy = &NormalizedIndentStrategy{}
	t.Cleanup(func() { ActiveStrategy = prev })

	// 12 distinct files out of 100 total: the threshold is 10, so this
	// corpus-wide shape must be demoted as boilerplate.
	matches, stats := FilterPatterns(boilerplatePatternLocations(t, 12), FilterConfig{
		MinOccur:      2,
		MinRank:       0,
		MinSimilarity: 0.5,
		TotalFiles:    100,
	})
	if len(matches) != 0 {
		t.Fatalf("boilerplate pattern survived filtering: %d matches", len(matches))
	}
	if stats.SkippedBoilerplate != 1 {
		t.Fatalf("SkippedBoilerplate = %d, want 1", stats.SkippedBoilerplate)
	}
}

func TestFilterPatternsKeepsLowFrequencyPattern(t *testing.T) {
	prev := ActiveStrategy
	ActiveStrategy = &NormalizedIndentStrategy{}
	t.Cleanup(func() { ActiveStrategy = prev })

	// 3 distinct files out of 100 total: far below the threshold, so the
	// pattern must survive as a regular match.
	matches, stats := FilterPatterns(boilerplatePatternLocations(t, 3), FilterConfig{
		MinOccur:      2,
		MinRank:       0,
		MinSimilarity: 0.5,
		TotalFiles:    100,
	})
	if len(matches) != 1 {
		t.Fatalf("low-frequency pattern was dropped: %d matches", len(matches))
	}
	if stats.SkippedBoilerplate != 0 {
		t.Fatalf("SkippedBoilerplate = %d, want 0", stats.SkippedBoilerplate)
	}
}

func TestBoilerplateFileThreshold(t *testing.T) {
	cases := []struct{ total, want int }{
		{0, 0},   // disabled
		{-1, 0},  // disabled
		{50, 10}, // floored at 10
		{99, 10},
		{100, 10},
		{1000, 100},
		{2186, 218},
	}
	for _, c := range cases {
		if got := boilerplateFileThreshold(c.total); got != c.want {
			t.Fatalf("boilerplateFileThreshold(%d) = %d, want %d", c.total, got, c.want)
		}
	}
}
