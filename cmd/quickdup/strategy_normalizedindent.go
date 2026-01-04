package main

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// NormalizedIndentEntry is the Entry implementation for normalized-indent strategy
// IndentDelta is normalized to -1, 0, or +1
type NormalizedIndentEntry struct {
	LineNumber  int
	IndentDelta int    // only -1, 0, or +1
	Word        string
	SourceLine  string
	hashBytes   []byte
}

func (e *NormalizedIndentEntry) GetLineNumber() int { return e.LineNumber }
func (e *NormalizedIndentEntry) GetRaw() string     { return e.SourceLine }
func (e *NormalizedIndentEntry) HashBytes() []byte  { return e.hashBytes }

// NewNormalizedIndentEntry creates a NormalizedIndentEntry with pre-computed hash bytes
func NewNormalizedIndentEntry(indentDelta int, word string) *NormalizedIndentEntry {
	return &NormalizedIndentEntry{
		IndentDelta: indentDelta,
		Word:        word,
		hashBytes:   []byte(fmt.Sprintf("%d|%s\n", indentDelta, word)),
	}
}

// NormalizedIndentStrategy matches patterns by normalized indent delta (-1/0/+1) and first word
type NormalizedIndentStrategy struct{}

func (s *NormalizedIndentStrategy) Name() string {
	return "normalized-indent"
}

func (s *NormalizedIndentStrategy) Preparse(content string) string {
	return cStyleStripper.Preparse(content)
}

func (s *NormalizedIndentStrategy) ParseLine(lineNum int, line string, prevEntry Entry) (Entry, bool) {
	if isWhitespaceOnly(line) || isCommentOnly(line) || shouldSkipByFirstWord(line) {
		return nil, true // skip
	}

	prevIndent := 0
	if prev, ok := prevEntry.(*NormalizedIndentEntry); ok && prev != nil {
		prevIndent = calculateIndent(prev.SourceLine)
	}

	indent := calculateIndent(line)
	word := extractFirstWord(line)

	// Normalize indent delta to -1, 0, or +1
	rawDelta := indent - prevIndent
	var indentDelta int
	if rawDelta > 0 {
		indentDelta = 1
	} else if rawDelta < 0 {
		indentDelta = -1
	} else {
		indentDelta = 0
	}

	hashBytes := []byte(fmt.Sprintf("%d|%s\n", indentDelta, word))

	entry := &NormalizedIndentEntry{
		LineNumber:  lineNum,
		IndentDelta: indentDelta,
		Word:        word,
		SourceLine:  line,
		hashBytes:   hashBytes,
	}
	return entry, false
}

func (s *NormalizedIndentStrategy) Hash(entries []Entry) uint64 {
	h := fnv.New64a()
	for _, e := range entries {
		h.Write(e.HashBytes())
	}
	return h.Sum64()
}

func (s *NormalizedIndentStrategy) Signature(entries []Entry) string {
	var parts []string
	for _, e := range entries {
		entry := e.(*NormalizedIndentEntry)
		parts = append(parts, entry.Word)
	}
	return strings.Join(parts, " ")
}

func (s *NormalizedIndentStrategy) Score(entries []Entry, similarity float64) int {
	seen := make(map[string]bool)
	for _, e := range entries {
		entry := e.(*NormalizedIndentEntry)
		seen[entry.Word] = true
	}
	uniqueWords := len(seen)
	adjustedSim := similarity*2 - 1.0
	if adjustedSim < 0 {
		adjustedSim = 0
	}
	// Base score from unique words + small bonus for length
	return int(float64(uniqueWords)*adjustedSim) + len(entries)/20
}

func (s *NormalizedIndentStrategy) BlockedHashes() map[uint64]bool {
	blocked := make(map[uint64]bool)

	// Common patterns to ignore (closing braces, function boundaries)
	// Using normalized deltas: -1, 0, +1
	uselessPatterns := [][]Entry{
		// } }
		{NewNormalizedIndentEntry(-1, "}"), NewNormalizedIndentEntry(-1, "}")},
		// } } }
		{NewNormalizedIndentEntry(-1, "}"), NewNormalizedIndentEntry(-1, "}"), NewNormalizedIndentEntry(-1, "}")},
		// return }
		{NewNormalizedIndentEntry(0, "return"), NewNormalizedIndentEntry(-1, "}")},
		// +1 return }
		{NewNormalizedIndentEntry(1, "return"), NewNormalizedIndentEntry(-1, "}")},
		// } return }
		{NewNormalizedIndentEntry(-1, "}"), NewNormalizedIndentEntry(0, "return"), NewNormalizedIndentEntry(-1, "}")},
		// } func
		{NewNormalizedIndentEntry(-1, "}"), NewNormalizedIndentEntry(0, "func")},
		// } return
		{NewNormalizedIndentEntry(-1, "}"), NewNormalizedIndentEntry(0, "return")},
	}

	for _, pattern := range uselessPatterns {
		blocked[s.Hash(pattern)] = true
	}

	return blocked
}
