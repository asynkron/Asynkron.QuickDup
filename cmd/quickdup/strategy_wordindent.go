package main

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// WordIndentEntry is the Entry implementation for word-indent strategy
type WordIndentEntry struct {
	LineNumber  int
	IndentDelta int
	Word        string
	SourceLine  string // actual source line for reconstruction
	hashBytes   []byte // pre-computed hash contribution
}

func (e *WordIndentEntry) GetLineNumber() int { return e.LineNumber }
func (e *WordIndentEntry) GetRaw() string     { return e.SourceLine }
func (e *WordIndentEntry) HashBytes() []byte  { return e.hashBytes }

// NewWordIndentEntry creates a WordIndentEntry with pre-computed hash bytes
func NewWordIndentEntry(indentDelta int, word string) *WordIndentEntry {
	return &WordIndentEntry{
		IndentDelta: indentDelta,
		Word:        word,
		hashBytes:   []byte(fmt.Sprintf("%d|%s\n", indentDelta, word)),
	}
}

// CStyleCommentStripper removes /* ... */ multiline comments
type CStyleCommentStripper struct{}

func (c *CStyleCommentStripper) Preparse(content string) string {
	result := []byte(content)
	i := 0
	for i < len(result) {
		// Look for /*
		if i+1 < len(result) && result[i] == '/' && result[i+1] == '*' {
			// Blank out /*
			result[i] = ' '
			result[i+1] = ' '
			j := i + 2
			// Find closing */ and blank everything
			for j < len(result) {
				if j+1 < len(result) && result[j] == '*' && result[j+1] == '/' {
					result[j] = ' '
					result[j+1] = ' '
					i = j + 2
					break
				}
				if result[j] != '\n' {
					result[j] = ' '
				}
				j++
			}
			if j >= len(result) {
				i = j
			}
		} else {
			i++
		}
	}
	return string(result)
}

// WordIndentStrategy matches patterns by indent delta and first word
type WordIndentStrategy struct{}

var cStyleStripper = &CStyleCommentStripper{}

func (s *WordIndentStrategy) Name() string {
	return "word-indent"
}

func (s *WordIndentStrategy) Preparse(content string) string {
	return cStyleStripper.Preparse(content)
}

func (s *WordIndentStrategy) ParseLine(lineNum int, line string, prevEntry Entry) (Entry, bool) {
	if isWhitespaceOnly(line) || isCommentOnly(line) {
		return nil, true // skip
	}

	prevIndent := 0
	if prev, ok := prevEntry.(*WordIndentEntry); ok && prev != nil {
		prevIndent = calculateIndent(prev.SourceLine)
	}

	indent := calculateIndent(line)
	word := extractFirstWord(line)
	indentDelta := indent - prevIndent

	// Pre-compute hash bytes
	hashBytes := []byte(fmt.Sprintf("%d|%s\n", indentDelta, word))

	entry := &WordIndentEntry{
		LineNumber:  lineNum,
		IndentDelta: indentDelta,
		Word:        word,
		SourceLine:  line,
		hashBytes:   hashBytes,
	}
	return entry, false
}

func (s *WordIndentStrategy) Hash(entries []Entry) uint64 {
	h := fnv.New64a()
	for _, e := range entries {
		h.Write(e.HashBytes())
	}
	return h.Sum64()
}

func (s *WordIndentStrategy) Signature(entries []Entry) string {
	var parts []string
	for _, e := range entries {
		entry := e.(*WordIndentEntry)
		parts = append(parts, entry.Word)
	}
	return strings.Join(parts, " ")
}

func (s *WordIndentStrategy) Score(entries []Entry, similarity float64) int {
	seen := make(map[string]bool)
	for _, e := range entries {
		entry := e.(*WordIndentEntry)
		seen[entry.Word] = true
	}
	uniqueWords := len(seen)
	adjustedSim := similarity*2 - 1.0
	if adjustedSim < 0 {
		adjustedSim = 0
	}
	return int(float64(uniqueWords) * adjustedSim)
}

func (s *WordIndentStrategy) BlockedHashes() map[uint64]bool {
	blocked := make(map[uint64]bool)

	// Common patterns to ignore (closing braces, function boundaries)
	uselessPatterns := [][]Entry{
		// } }
		{NewWordIndentEntry(-4, "}"), NewWordIndentEntry(-4, "}")},
		// } } }
		{NewWordIndentEntry(-4, "}"), NewWordIndentEntry(-4, "}"), NewWordIndentEntry(-4, "}")},
		// return }
		{NewWordIndentEntry(0, "return"), NewWordIndentEntry(-4, "}")},
		// +4 return }
		{NewWordIndentEntry(4, "return"), NewWordIndentEntry(-4, "}")},
		// } return }
		{NewWordIndentEntry(-4, "}"), NewWordIndentEntry(0, "return"), NewWordIndentEntry(-4, "}")},
		// } func
		{NewWordIndentEntry(-4, "}"), NewWordIndentEntry(0, "func")},
		// } return
		{NewWordIndentEntry(-4, "}"), NewWordIndentEntry(0, "return")},
	}

	for _, pattern := range uselessPatterns {
		blocked[s.Hash(pattern)] = true
	}

	return blocked
}
