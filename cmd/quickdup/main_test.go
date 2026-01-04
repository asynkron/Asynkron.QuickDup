package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestStrategyParityWithDefault verifies that WordIndentStrategy produces
// the same entries as the default parseFile function
func TestStrategyParityWithDefault(t *testing.T) {
	// Create a temporary test file with mixed content
	content := `package main

import "fmt"

// This is a comment
func main() {
	// Another comment
	fmt.Println("Hello")

	if true {
		doSomething()
	}
}

/* Block comment */
func helper() {
	x := 1
}
`
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Parse with original parseFile
	originalEntries, err := parseFile(testFile)
	if err != nil {
		t.Fatalf("parseFile failed: %v", err)
	}

	// Parse with strategy-based approach
	strategy := &WordIndentStrategy{}
	allLines, err := parseFileRaw(testFile)
	if err != nil {
		t.Fatalf("parseFileRaw failed: %v", err)
	}

	// Filter using strategy (same as parseFilesWithStrategy does)
	var strategyEntries []*SourceLine
	for _, line := range allLines {
		if !strategy.ShouldSkip(line) {
			strategyEntries = append(strategyEntries, line)
		}
	}

	// Compare counts
	if len(originalEntries) != len(strategyEntries) {
		t.Errorf("Entry count mismatch: original=%d, strategy=%d",
			len(originalEntries), len(strategyEntries))
		t.Logf("Original entries:")
		for i, e := range originalEntries {
			t.Logf("  [%d] line=%d delta=%d word=%q", i, e.LineNumber, e.IndentDelta, e.Word)
		}
		t.Logf("Strategy entries:")
		for i, e := range strategyEntries {
			t.Logf("  [%d] line=%d delta=%d word=%q", i, e.LineNumber, e.IndentDelta, e.Word)
		}
		return
	}

	// Compare each entry
	for i := range originalEntries {
		orig := originalEntries[i]
		strat := strategyEntries[i]

		if orig.LineNumber != strat.LineNumber {
			t.Errorf("Entry %d: LineNumber mismatch: original=%d, strategy=%d",
				i, orig.LineNumber, strat.LineNumber)
		}
		if orig.IndentDelta != strat.IndentDelta {
			t.Errorf("Entry %d: IndentDelta mismatch: original=%d, strategy=%d",
				i, orig.IndentDelta, strat.IndentDelta)
		}
		if orig.Word != strat.Word {
			t.Errorf("Entry %d: Word mismatch: original=%q, strategy=%q",
				i, orig.Word, strat.Word)
		}
	}
}

// TestStrategyHashParity verifies that hashing produces the same result
// for equivalent slices of entries
func TestStrategyHashParity(t *testing.T) {
	// Create test content
	content := `func foo() {
	x := 1
	y := 2
}
`
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Parse both ways
	originalEntries, _ := parseFile(testFile)
	strategy := &WordIndentStrategy{}
	allLines, _ := parseFileRaw(testFile)
	var strategyEntries []*SourceLine
	for _, line := range allLines {
		if !strategy.ShouldSkip(line) {
			strategyEntries = append(strategyEntries, line)
		}
	}

	// Compute hash using original method
	originalHash := hashPattern(originalEntries[:3])

	// Compute hash using strategy
	strategyHash := strategy.Hash(strategyEntries[:3])

	if originalHash != strategyHash {
		t.Errorf("Hash mismatch: original=%d, strategy=%d", originalHash, strategyHash)
		t.Logf("Original entries for hash:")
		for i, e := range originalEntries[:3] {
			t.Logf("  [%d] delta=%d word=%q", i, e.IndentDelta, e.Word)
		}
		t.Logf("Strategy entries for hash:")
		for i, e := range strategyEntries[:3] {
			t.Logf("  [%d] delta=%d word=%q", i, e.IndentDelta, e.Word)
		}
	}
}
