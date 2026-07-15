package quickdup

type NormalizedIndentEntry struct{ indentEntry }

func (e *NormalizedIndentEntry) indentData() *indentEntry { return &e.indentEntry }

func NewNormalizedIndentEntry(indentDelta int, word string) *NormalizedIndentEntry {
	return &NormalizedIndentEntry{indentEntry: newIndentEntry(0, indentDelta, word, "")}
}

// NormalizedIndentStrategy matches patterns by normalized indent delta (-1/0/+1) and first word
type NormalizedIndentStrategy struct{}

func (s *NormalizedIndentStrategy) Name() string { return "normalized-indent" }

func (s *NormalizedIndentStrategy) Preparse(content string) string {
	return cStyleStripper.Preparse(content)
}

func (s *NormalizedIndentStrategy) ParseLine(lineNum int, line string, prevEntry Entry) (Entry, bool) {
	return normalizedIndentShared.ParseLine(lineNum, line, prevEntry)
}

func (s *NormalizedIndentStrategy) Hash(entries []Entry) uint64 {
	return normalizedIndentShared.Hash(entries)
}

func (s *NormalizedIndentStrategy) Signature(entries []Entry) string {
	return normalizedIndentShared.Signature(entries)
}

func (s *NormalizedIndentStrategy) Score(entries []Entry, similarity float64) int {
	return normalizedIndentShared.Score(entries, similarity)
}

func (s *NormalizedIndentStrategy) Complexity(entries []Entry) int {
	return normalizedIndentShared.Complexity(entries)
}

var normalizedIndentShared = indentStrategy{
	makeEntry: func(lineNum int, indentDelta int, word string, sourceLine string) Entry {
		return &NormalizedIndentEntry{indentEntry: newIndentEntry(lineNum, indentDelta, word, sourceLine)}
	},
	meta:           indentMeta[*NormalizedIndentEntry],
	normalizeDelta: normalizeIndentDelta,
}

func normalizeIndentDelta(raw int) int {
	if raw > 0 {
		return 1
	}
	if raw < 0 {
		return -1
	}
	return 0
}

func (s *NormalizedIndentStrategy) BlockedHashes() map[uint64]bool {
	return blockedIndentPatternHashes(func(delta int, word string) Entry {
		return NewNormalizedIndentEntry(delta, word)
	}, -1, 1, s.Hash)
}
