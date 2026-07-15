package quickdup

// InlineableEntry is the Entry implementation for inlineable strategy
type InlineableEntry = FirstWordEntry

// InlineableStrategy finds duplicate one-liner methods that could be inlined
// Looks for patterns like: public/private/internal/protected ... { return ... }
type InlineableStrategy struct {
	firstWordStrategy
}

func (s *InlineableStrategy) Name() string {
	return "inlineable"
}

// Access modifiers that start inlineable methods
var accessModifiers = map[string]bool{
	"public":    true,
	"private":   true,
	"internal":  true,
	"protected": true,
}

func (s *InlineableStrategy) Score(entries []Entry, similarity float64) int {
	// Only match short patterns: 3-5 lines
	if len(entries) < 3 || len(entries) > 5 {
		return 0
	}

	// Extract words
	words := make([]string, len(entries))
	for i, e := range entries {
		entry := e.(*FirstWordEntry)
		words[i] = entry.Word
	}

	// Check for exact inlineable pattern:
	// Pattern A (4 lines): modifier { return }
	// Pattern B (3 lines): modifier return } (brace on same line)
	// May have 1 extra line at end (closing brace of class)
	matched := false

	if len(words) >= 4 {
		// Check for: modifier { return }
		if accessModifiers[words[0]] && words[1] == "{" && words[2] == "return" && words[3] == "}" {
			matched = true
		}
	}
	if !matched && len(words) >= 3 {
		// Check for: modifier return }
		if accessModifiers[words[0]] && words[1] == "return" && words[2] == "}" {
			matched = true
		}
	}

	if !matched {
		return 0
	}

	// High score for inlineable patterns
	// Base score of 50, plus similarity bonus
	adjustedSim := similarity*2 - 1.0
	if adjustedSim < 0 {
		adjustedSim = 0
	}

	return 50 + int(adjustedSim*50)
}

func (s *InlineableStrategy) BlockedHashes() map[uint64]bool {
	// No blocked patterns for inlineable strategy
	return make(map[uint64]bool)
}
