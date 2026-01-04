package main

import "strings"

// UnionFind implements a disjoint-set data structure for clustering
type UnionFind struct {
	parent []int
	rank   []int
}

// NewUnionFind creates a new UnionFind with n elements
func NewUnionFind(n int) *UnionFind {
	uf := &UnionFind{
		parent: make([]int, n),
		rank:   make([]int, n),
	}
	for i := range uf.parent {
		uf.parent[i] = i
	}
	return uf
}

// Find returns the root of the set containing x (with path compression)
func (uf *UnionFind) Find(x int) int {
	if uf.parent[x] != x {
		uf.parent[x] = uf.Find(uf.parent[x])
	}
	return uf.parent[x]
}

// Union merges the sets containing x and y
func (uf *UnionFind) Union(x, y int) {
	px, py := uf.Find(x), uf.Find(y)
	if px == py {
		return
	}
	if uf.rank[px] < uf.rank[py] {
		px, py = py, px
	}
	uf.parent[py] = px
	if uf.rank[px] == uf.rank[py] {
		uf.rank[px]++
	}
}

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

// ClusterResult holds a cluster of similar locations and their average similarity
type ClusterResult struct {
	Locations  []PatternLocation
	Similarity float64
}

// clusterBySimilarity groups locations into clusters where all members have >= threshold similarity
// Returns clusters sorted by size (largest first)
func clusterBySimilarity(locations []PatternLocation, threshold float64) []ClusterResult {
	n := len(locations)
	if n < 2 {
		return []ClusterResult{{Locations: locations, Similarity: 1.0}}
	}

	// Tokenize all patterns
	tokenized := make([][]string, n)
	for i, loc := range locations {
		tokenized[i] = tokenizePattern(loc.Pattern)
	}

	// Compute pairwise similarities and build clusters using Union-Find
	uf := NewUnionFind(n)
	similarities := make(map[[2]int]float64) // store similarities for later

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			sim := tokenSimilarity(tokenized[i], tokenized[j])
			similarities[[2]int{i, j}] = sim
			if sim >= threshold {
				uf.Union(i, j)
			}
		}
	}

	// Group locations by cluster root
	clusterMap := make(map[int][]int)
	for i := 0; i < n; i++ {
		root := uf.Find(i)
		clusterMap[root] = append(clusterMap[root], i)
	}

	// Build cluster results with similarity scores
	var results []ClusterResult
	for _, indices := range clusterMap {
		cluster := make([]PatternLocation, len(indices))
		for i, idx := range indices {
			cluster[i] = locations[idx]
		}

		// Compute average similarity within cluster
		var totalSim float64
		var pairs int
		for i := 0; i < len(indices); i++ {
			for j := i + 1; j < len(indices); j++ {
				a, b := indices[i], indices[j]
				if a > b {
					a, b = b, a
				}
				totalSim += similarities[[2]int{a, b}]
				pairs++
			}
		}

		sim := 1.0
		if pairs > 0 {
			sim = totalSim / float64(pairs)
		}

		results = append(results, ClusterResult{
			Locations:  cluster,
			Similarity: sim,
		})
	}

	// Sort by cluster size (largest first)
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if len(results[j].Locations) > len(results[i].Locations) {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	return results
}
