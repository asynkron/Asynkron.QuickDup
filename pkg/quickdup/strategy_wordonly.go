package quickdup

// WordOnlyEntry is the Entry implementation for word-only strategy
// Ignores indentation entirely, only uses first word
type WordOnlyEntry = FirstWordEntry

// NewWordOnlyEntry creates a WordOnlyEntry with pre-computed hash bytes
func NewWordOnlyEntry(word string) *WordOnlyEntry {
	return NewFirstWordEntry(word)
}

// WordOnlyStrategy matches patterns by first word only, ignoring indentation
type WordOnlyStrategy struct {
	firstWordStrategy
}

func (s *WordOnlyStrategy) Name() string {
	return "word-only"
}

func (s *WordOnlyStrategy) Score(entries []Entry, similarity float64) int {
	seen := make(map[string]bool)
	for _, e := range entries {
		entry := e.(*FirstWordEntry)
		seen[entry.Word] = true
	}
	uniqueWords := len(seen)
	adjustedSim := similarity*2 - 1.0
	if adjustedSim < 0 {
		adjustedSim = 0
	}
	// Cube similarity factor - heavily rewards high similarity
	// 100%=1.0, 95%=0.73, 90%=0.51, 85%=0.34
	simFactor := adjustedSim * adjustedSim * adjustedSim
	return int(float64(uniqueWords)*simFactor) + len(entries)/20
}

func (s *WordOnlyStrategy) BlockedHashes() map[uint64]bool {
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

	return blockPatternHashes(uselessPatterns, s.Hash)
}
