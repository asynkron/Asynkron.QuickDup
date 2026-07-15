package quickdup

type WordIndentEntry struct{ indentEntry }

func (e *WordIndentEntry) indentData() *indentEntry { return &e.indentEntry }

func NewWordIndentEntry(indentDelta int, word string) *WordIndentEntry {
	return &WordIndentEntry{indentEntry: newIndentEntry(0, indentDelta, word, "")}
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

func (s *WordIndentStrategy) Name() string { return "word-indent" }

func (s *WordIndentStrategy) Preparse(content string) string { return cStyleStripper.Preparse(content) }

func (s *WordIndentStrategy) ParseLine(lineNum int, line string, prevEntry Entry) (Entry, bool) {
	return wordIndentShared.ParseLine(lineNum, line, prevEntry)
}

func (s *WordIndentStrategy) Hash(entries []Entry) uint64 { return wordIndentShared.Hash(entries) }

func (s *WordIndentStrategy) Signature(entries []Entry) string {
	return wordIndentShared.Signature(entries)
}

func (s *WordIndentStrategy) Score(entries []Entry, similarity float64) int {
	return wordIndentShared.Score(entries, similarity)
}

func (s *WordIndentStrategy) Complexity(entries []Entry) int {
	return wordIndentShared.Complexity(entries)
}

var wordIndentShared = indentStrategy{
	makeEntry: func(lineNum int, indentDelta int, word string, sourceLine string) Entry {
		return &WordIndentEntry{indentEntry: newIndentEntry(lineNum, indentDelta, word, sourceLine)}
	},
	meta:           indentMeta[*WordIndentEntry],
	normalizeDelta: func(raw int) int { return raw },
}

func (s *WordIndentStrategy) BlockedHashes() map[uint64]bool {
	return blockedIndentPatternHashes(func(delta int, word string) Entry {
		return NewWordIndentEntry(delta, word)
	}, -4, 4, s.Hash)
}
