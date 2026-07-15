package quickdup

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestFilterOverlappingOccurrencesKeepsFirstNonOverlappingLocation(t *testing.T) {
	locs := []PatternLocation{
		{Filename: "b.go", LineStart: 20, EntryIndex: 0},
		{Filename: "a.go", LineStart: 1, EntryIndex: 0},
		{Filename: "a.go", LineStart: 2, EntryIndex: 1},
		{Filename: "a.go", LineStart: 4, EntryIndex: 3},
	}

	filtered := filterOverlappingOccurrences(locs, 2)
	sortLocations(filtered)

	got := locationKeys(filtered)
	want := []string{"a.go:0:1", "a.go:3:4", "b.go:0:20"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("filtered locations = %v, want %v", got, want)
	}
}

func TestGenerateAndExtendPatternsUseEntryWindows(t *testing.T) {
	withWordOnlyStrategy(t)
	fileData := map[string][]Entry{
		"one.go":   wordEntries(10, "alpha", "beta", "gamma", "tail"),
		"two.go":   wordEntries(20, "alpha", "beta", "gamma", "other"),
		"short.go": wordEntries(30, "alpha", "beta"),
	}
	files := []string{"one.go", "two.go", "short.go"}

	base := generateBasePatternsParallel(fileData, files, 2, 2)
	alphaBetaHash := ActiveStrategy.Hash(wordEntries(0, "alpha", "beta"))
	baseLocs := append([]PatternLocation(nil), base[alphaBetaHash]...)
	sortLocations(baseLocs)
	if got, want := locationKeys(baseLocs), []string{"one.go:0:10", "short.go:0:30", "two.go:0:20"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("base alpha/beta locations = %v, want %v", got, want)
	}

	extended := extendPatternsParallel(map[uint64][]PatternLocation{alphaBetaHash: baseLocs}, fileData, 3, 2)
	alphaBetaGammaHash := ActiveStrategy.Hash(wordEntries(0, "alpha", "beta", "gamma"))
	extendedLocs := append([]PatternLocation(nil), extended[alphaBetaGammaHash]...)
	sortLocations(extendedLocs)
	if got, want := locationKeys(extendedLocs), []string{"one.go:0:10", "two.go:0:20"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("extended alpha/beta/gamma locations = %v, want %v", got, want)
	}
	if got := countLocations(extended); got != 2 {
		t.Fatalf("countLocations(extended) = %d, want 2", got)
	}
	if got := extendPatternsParallel(nil, fileData, 3, 2); len(got) != 0 {
		t.Fatalf("empty survivors extended to %d patterns, want 0", len(got))
	}
}

func TestDetectPatternsCoversThresholdsGrowthMaxSizeAndOverlapMode(t *testing.T) {
	withWordOnlyStrategy(t)

	empty := DetectPatterns(nil, 0, 2, 2, 0, false)
	if len(empty) != 0 {
		t.Fatalf("empty file data produced %d patterns, want 0", len(empty))
	}

	overlapData := map[string][]Entry{
		"overlap.go": wordEntries(1, "same", "same", "same"),
	}
	suppressed := DetectPatterns(overlapData, 1, 2, 2, 2, false)
	if len(suppressed) != 0 {
		t.Fatalf("overlap suppression left %d patterns, want 0", len(suppressed))
	}
	kept := DetectPatterns(overlapData, 1, 2, 2, 2, true)
	if len(kept) != 1 {
		t.Fatalf("keepOverlaps detected %d patterns, want 1", len(kept))
	}

	growingData := map[string][]Entry{
		"one.go": wordEntries(10, "alpha", "beta", "gamma", "tail"),
		"two.go": wordEntries(20, "alpha", "beta", "gamma", "other"),
	}
	grown := DetectPatterns(growingData, 2, 2, 2, 3, false)
	alphaBetaGammaHash := ActiveStrategy.Hash(wordEntries(0, "alpha", "beta", "gamma"))
	locs := append([]PatternLocation(nil), grown[alphaBetaGammaHash]...)
	sortLocations(locs)
	if got, want := locationKeys(locs), []string{"one.go:0:10", "two.go:0:20"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("grown max-size locations = %v, want %v", got, want)
	}
	if got := len(locs[0].Pattern); got != 3 {
		t.Fatalf("grown pattern length = %d, want 3", got)
	}

	tooStrict := DetectPatterns(growingData, 2, 3, 2, 3, false)
	if len(tooStrict) != 0 {
		t.Fatalf("minOccur=3 detected %d patterns, want 0", len(tooStrict))
	}
}

func TestAnalyzeFilesShapesParsedMatchesWithFilters(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.go")
	second := filepath.Join(dir, "second.go")
	body := strings.Join([]string{
		"package main",
		"import \"fmt\"",
		"// scanner comments are skipped",
		"",
		"func alpha() {",
		"\tfmt.Println(\"same\")",
		"}",
	}, "\n")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
	}

	result, err := AnalyzeFiles(AnalyzeOptions{
		Files:         []string{second, first},
		MinOccur:      2,
		MinSize:       3,
		MaxSize:       3,
		MinSimilarity: 1,
	})
	if err != nil {
		t.Fatalf("AnalyzeFiles returned error: %v", err)
	}
	if result.FilesScanned != 2 || result.LinesScanned != 6 {
		t.Fatalf("scan counts = files %d lines %d, want files 2 lines 6", result.FilesScanned, result.LinesScanned)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("matches = %d, want 1: %+v", len(result.Matches), result.Matches)
	}
	match := result.Matches[0]
	if match.Similarity != 1 || match.Rank <= 0 || len(match.Pattern) != 3 {
		t.Fatalf("match shape drifted: similarity=%v rank=%d pattern=%d", match.Similarity, match.Rank, len(match.Pattern))
	}
	locs := append([]PatternLocation(nil), match.Locations...)
	sortLocations(locs)
	if got, want := locationKeys(locs), []string{first + ":0:5", second + ":0:5"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("match locations = %v, want %v", got, want)
	}
}

func withWordOnlyStrategy(t *testing.T) {
	t.Helper()
	previousStrategy := ActiveStrategy
	previousDebug := Debug
	previousPrefix := CommentPrefix
	previousExt := currentFileExt
	ActiveStrategy = &WordOnlyStrategy{}
	Debug = false
	CommentPrefix = "//"
	currentFileExt = ".go"
	t.Cleanup(func() {
		ActiveStrategy = previousStrategy
		Debug = previousDebug
		CommentPrefix = previousPrefix
		currentFileExt = previousExt
	})
}

func wordEntries(startLine int, words ...string) []Entry {
	entries := make([]Entry, 0, len(words))
	for i, word := range words {
		entry := NewWordOnlyEntry(word)
		entry.LineNumber = startLine + i
		entry.SourceLine = word
		entries = append(entries, entry)
	}
	return entries
}

func sortLocations(locs []PatternLocation) {
	sort.Slice(locs, func(i, j int) bool {
		if locs[i].Filename != locs[j].Filename {
			return locs[i].Filename < locs[j].Filename
		}
		return locs[i].EntryIndex < locs[j].EntryIndex
	})
}

func locationKeys(locs []PatternLocation) []string {
	keys := make([]string, 0, len(locs))
	for _, loc := range locs {
		keys = append(keys, loc.Filename+":"+strconv.Itoa(loc.EntryIndex)+":"+strconv.Itoa(loc.LineStart))
	}
	return keys
}
