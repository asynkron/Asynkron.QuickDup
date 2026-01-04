package main

import "strings"

// tokenizeLine extracts all tokens from a source line
func tokenizeLine(line string) []string {
	var tokens []string
	var current strings.Builder

	for _, r := range line {
		if strings.ContainsRune(separators, r) || r == '"' || r == '\'' || r == '`' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// tokenizePattern extracts all tokens from a pattern's source lines
func tokenizePattern(pattern []Entry) []string {
	var tokens []string
	for _, entry := range pattern {
		tokens = append(tokens, tokenizeLine(entry.GetRaw())...)
	}
	return tokens
}

// tokenSimilarity computes Jaccard similarity between two token sets
func tokenSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	setA := make(map[string]bool)
	for _, t := range a {
		setA[t] = true
	}

	setB := make(map[string]bool)
	for _, t := range b {
		setB[t] = true
	}

	intersection := 0
	for t := range setA {
		if setB[t] {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// computeAverageTokenSimilarity computes the average pairwise token similarity across all occurrences
func computeAverageTokenSimilarity(locations []PatternLocation) float64 {
	if len(locations) < 2 {
		return 1.0 // Single occurrence = 100% similar to itself
	}

	// Tokenize all patterns
	tokenized := make([][]string, len(locations))
	for i, loc := range locations {
		tokenized[i] = tokenizePattern(loc.Pattern)
	}

	// Compute average pairwise similarity
	totalSim := 0.0
	pairs := 0
	for i := 0; i < len(tokenized); i++ {
		for j := i + 1; j < len(tokenized); j++ {
			totalSim += tokenSimilarity(tokenized[i], tokenized[j])
			pairs++
		}
	}

	if pairs == 0 {
		return 1.0
	}
	return totalSim / float64(pairs)
}
