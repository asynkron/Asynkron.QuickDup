package quickdup

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// FirstWordEntry is the Entry implementation shared by first-word strategies.
type FirstWordEntry struct {
	LineNumber int
	Word       string
	SourceLine string
	hashBytes  []byte
}

func (e *FirstWordEntry) GetLineNumber() int { return e.LineNumber }
func (e *FirstWordEntry) GetRaw() string     { return e.SourceLine }
func (e *FirstWordEntry) HashBytes() []byte  { return e.hashBytes }

// NewFirstWordEntry creates a FirstWordEntry with pre-computed hash bytes.
func NewFirstWordEntry(word string) *FirstWordEntry {
	return &FirstWordEntry{
		Word:      word,
		hashBytes: []byte(word + "\n"),
	}
}

type firstWordStrategy struct{}

func (s *firstWordStrategy) Preparse(content string) string {
	return cStyleStripper.Preparse(content)
}

func (s *firstWordStrategy) ParseLine(lineNum int, line string, prevEntry Entry) (Entry, bool) {
	_ = prevEntry
	if isWhitespaceOnly(line) || isCommentOnly(line) || shouldSkipByFirstWord(line) {
		return nil, true // skip
	}

	word := extractFirstWord(line)
	entry := NewFirstWordEntry(word)
	entry.LineNumber = lineNum
	entry.SourceLine = line
	return entry, false
}

func (s *firstWordStrategy) Hash(entries []Entry) uint64 {
	return hashEntryBytes(entries)
}

func (s *firstWordStrategy) Signature(entries []Entry) string {
	return signatureWords(entries, func(e Entry) string {
		return e.(*FirstWordEntry).Word
	})
}

func (s *firstWordStrategy) Complexity(entries []Entry) int {
	_ = entries
	return 0
}

func hashEntryBytes(entries []Entry) uint64 {
	h := fnv.New64a()
	for _, e := range entries {
		h.Write(e.HashBytes())
	}
	return h.Sum64()
}

func signatureWords(entries []Entry, wordOf func(Entry) string) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, wordOf(e))
	}
	return strings.Join(parts, " ")
}

func scoreIndentShape(entries []Entry, similarity float64, inspect func(Entry) (word string, indentDelta int)) int {
	seen := make(map[string]bool)
	running := 0
	minRunning := 0
	for _, e := range entries {
		word, indentDelta := inspect(e)
		seen[word] = true
		running += indentDelta
		if running < minRunning {
			minRunning = running
		}
	}

	unopenedCloses := -minRunning
	unclosedOpens := running
	if unclosedOpens < 0 {
		unclosedOpens = 0
	}
	imbalance := unopenedCloses + unclosedOpens

	effectiveWords := len(seen) - imbalance
	if effectiveWords < 0 {
		effectiveWords = 0
	}

	adjustedSim := similarity*2 - 1.0
	if adjustedSim < 0 {
		adjustedSim = 0
	}
	simFactor := adjustedSim * adjustedSim * adjustedSim
	return int(float64(effectiveWords)*simFactor) + len(entries)/20
}

func positiveIndentComplexity(entries []Entry, indentDeltaOf func(Entry) int) int {
	complexity := 0
	for _, e := range entries {
		if indentDeltaOf(e) > 0 {
			complexity++
		}
	}
	return complexity
}

type indentEntry struct {
	LineNumber  int
	IndentDelta int
	Word        string
	SourceLine  string
	hashBytes   []byte
}

func (e *indentEntry) GetLineNumber() int { return e.LineNumber }
func (e *indentEntry) GetRaw() string     { return e.SourceLine }
func (e *indentEntry) HashBytes() []byte  { return e.hashBytes }

func newIndentEntry(lineNum int, indentDelta int, word string, sourceLine string) indentEntry {
	return indentEntry{
		LineNumber:  lineNum,
		IndentDelta: indentDelta,
		Word:        word,
		SourceLine:  sourceLine,
		hashBytes:   []byte(fmt.Sprintf("%d|%s\n", indentDelta, word)),
	}
}

type indentCarrier interface {
	Entry
	indentData() *indentEntry
}

type indentStrategy struct {
	makeEntry      func(lineNum int, indentDelta int, word string, sourceLine string) Entry
	meta           func(Entry) (*indentEntry, bool)
	normalizeDelta func(int) int
}

func (s indentStrategy) ParseLine(lineNum int, line string, prevEntry Entry) (Entry, bool) {
	if isWhitespaceOnly(line) || isCommentOnly(line) || shouldSkipByFirstWord(line) {
		return nil, true
	}

	prevIndent := 0
	if prev, ok := s.meta(prevEntry); ok {
		prevIndent = calculateIndent(prev.SourceLine)
	}

	indentDelta := s.normalizeDelta(calculateIndent(line) - prevIndent)
	return s.makeEntry(lineNum, indentDelta, extractFirstWord(line), line), false
}

func (s indentStrategy) Hash(entries []Entry) uint64 {
	return hashEntryBytes(entries)
}

func (s indentStrategy) Signature(entries []Entry) string {
	return signatureWords(entries, func(e Entry) string {
		meta, _ := s.meta(e)
		return meta.Word
	})
}

func (s indentStrategy) Score(entries []Entry, similarity float64) int {
	return scoreIndentShape(entries, similarity, func(e Entry) (string, int) {
		meta, _ := s.meta(e)
		return meta.Word, meta.IndentDelta
	})
}

func (s indentStrategy) Complexity(entries []Entry) int {
	return positiveIndentComplexity(entries, func(e Entry) int {
		meta, _ := s.meta(e)
		return meta.IndentDelta
	})
}

func indentMeta[T indentCarrier](e Entry) (*indentEntry, bool) {
	entry, ok := e.(T)
	if !ok {
		return nil, false
	}
	return entry.indentData(), true
}

func blockedIndentPatternHashes(newEntry func(int, string) Entry, closeDelta int, openReturnDelta int, hash func([]Entry) uint64) map[uint64]bool {
	patterns := [][]Entry{
		{newEntry(closeDelta, "}"), newEntry(closeDelta, "}")},
		{newEntry(closeDelta, "}"), newEntry(closeDelta, "}"), newEntry(closeDelta, "}")},
		{newEntry(0, "return"), newEntry(closeDelta, "}")},
		{newEntry(openReturnDelta, "return"), newEntry(closeDelta, "}")},
		{newEntry(closeDelta, "}"), newEntry(0, "return"), newEntry(closeDelta, "}")},
		{newEntry(closeDelta, "}"), newEntry(0, "func")},
		{newEntry(closeDelta, "}"), newEntry(0, "return")},
	}
	return blockPatternHashes(patterns, hash)
}

func blockPatternHashes(patterns [][]Entry, hash func([]Entry) uint64) map[uint64]bool {
	blocked := make(map[uint64]bool, len(patterns))
	for _, pattern := range patterns {
		blocked[hash(pattern)] = true
	}
	return blocked
}
