package main

import (
	"hash/fnv"
	"strings"
)

// WordOnlyEntry is the Entry implementation for word-only strategy
// Ignores indentation entirely, only uses first word
type WordOnlyEntry struct {
	LineNumber int
	Word       string
	SourceLine string
	hashBytes  []byte
}

func (e *WordOnlyEntry) GetLineNumber() int { return e.LineNumber }
func (e *WordOnlyEntry) GetRaw() string     { return e.SourceLine }
func (e *WordOnlyEntry) HashBytes() []byte  { return e.hashBytes }

// NewWordOnlyEntry creates a WordOnlyEntry with pre-computed hash bytes
func NewWordOnlyEntry(word string) *WordOnlyEntry {
	return &WordOnlyEntry{
		Word:      word,
		hashBytes: []byte(word + "\n"),
	}
}

// WordOnlyStrategy matches patterns by first word only, ignoring indentation
type WordOnlyStrategy struct{}

func (s *WordOnlyStrategy) Name() string {
	return "word-only"
}

func (s *WordOnlyStrategy) Preparse(content string) string {
	return cStyleStripper.Preparse(content)
}

func (s *WordOnlyStrategy) ParseLine(lineNum int, line string, prevEntry Entry) (Entry, bool) {
	if isWhitespaceOnly(line) || isCommentOnly(line) || shouldSkipByFirstWord(line) {
		return nil, true // skip
	}

	word := extractFirstWord(line)
	hashBytes := []byte(word + "\n")

	entry := &WordOnlyEntry{
		LineNumber: lineNum,
		Word:       word,
		SourceLine: line,
		hashBytes:  hashBytes,
	}
	return entry, false
}

func (s *WordOnlyStrategy) Hash(entries []Entry) uint64 {
	h := fnv.New64a()
	for _, e := range entries {
		h.Write(e.HashBytes())
	}
	return h.Sum64()
}

func (s *WordOnlyStrategy) Signature(entries []Entry) string {
	var parts []string
	for _, e := range entries {
		entry := e.(*WordOnlyEntry)
		parts = append(parts, entry.Word)
	}
	return strings.Join(parts, " ")
}

func (s *WordOnlyStrategy) Score(entries []Entry, similarity float64) int {
	seen := make(map[string]bool)
	for _, e := range entries {
		entry := e.(*WordOnlyEntry)
		seen[entry.Word] = true
	}
	uniqueWords := len(seen)
	adjustedSim := similarity*2 - 1.0
	if adjustedSim < 0 {
		adjustedSim = 0
	}
	return int(float64(uniqueWords) * adjustedSim)
}

func (s *WordOnlyStrategy) BlockedHashes() map[uint64]bool {
	blocked := make(map[uint64]bool)

	// Common patterns to ignore
	uselessPatterns := [][]Entry{
		// } }
		{NewWordOnlyEntry("}"), NewWordOnlyEntry("}")},
		// } } }
		{NewWordOnlyEntry("}"), NewWordOnlyEntry("}"), NewWordOnlyEntry("}")},
		// return }
		{NewWordOnlyEntry("return"), NewWordOnlyEntry("}")},
		// } return }
		{NewWordOnlyEntry("}"), NewWordOnlyEntry("return"), NewWordOnlyEntry("}")},
		// } func
		{NewWordOnlyEntry("}"), NewWordOnlyEntry("func")},
		// } return
		{NewWordOnlyEntry("}"), NewWordOnlyEntry("return")},
	}

	for _, pattern := range uselessPatterns {
		blocked[s.Hash(pattern)] = true
	}

	return blocked
}
