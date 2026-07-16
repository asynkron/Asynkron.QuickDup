package quickdup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// FilterConfig holds the configuration for filtering patterns
type FilterConfig struct {
	MinOccur      int
	MinRank       int
	MinSimilarity float64
	UserIgnored   map[uint64]bool // user-defined patterns to ignore
}

// FilterStats holds statistics about filtered patterns
type FilterStats struct {
	SkippedBlocked       int
	SkippedLowRank       int
	SkippedLowSimilarity int
}

// FilterPatterns filters raw patterns into scored matches
// Returns sorted matches (by score descending) and filter statistics
func FilterPatterns(patterns map[uint64][]PatternLocation, config FilterConfig) ([]PatternMatch, FilterStats) {
	var stats FilterStats

	// Get blocked hashes from strategy
	blockedHashes := ActiveStrategy.BlockedHashes()

	// First pass: filter blocked patterns and collect candidates
	type candidate struct {
		hash    uint64
		locs    []PatternLocation
		pattern []Entry
	}
	var candidates []candidate

	for hash, locs := range patterns {
		if blockedHashes[hash] || config.UserIgnored[hash] {
			stats.SkippedBlocked++
			continue
		}
		if len(locs) >= config.MinOccur {
			pattern := locs[0].Pattern
			candidates = append(candidates, candidate{hash, locs, pattern})
		}
	}

	// Second pass: parallel clustering by similarity
	type clusterResult struct {
		index    int
		clusters []ClusterResult
	}
	results := make([]clusterResult, len(candidates))
	numWorkers := runtime.NumCPU()

	var wg sync.WaitGroup
	work := make(chan int, len(candidates))
	for i := range candidates {
		work <- i
	}
	close(work)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				clusters := clusterBySimilarity(candidates[idx].locs, config.MinSimilarity)
				results[idx] = clusterResult{idx, clusters}
			}
		}()
	}
	wg.Wait()

	// Third pass: collect matches from clusters that pass thresholds
	var matches []PatternMatch
	for _, r := range results {
		c := candidates[r.index]
		for _, cluster := range r.clusters {
			// Skip clusters that don't meet minimum occurrence threshold
			if len(cluster.Locations) < config.MinOccur {
				stats.SkippedLowSimilarity++
				continue
			}

			score := ActiveStrategy.Score(c.pattern, cluster.Similarity)
			complexity := ActiveStrategy.Complexity(c.pattern)
			rank := score + complexity
			if rank < config.MinRank {
				stats.SkippedLowRank++
				continue
			}

			matches = append(matches, PatternMatch{
				Hash:       c.hash,
				Locations:  cluster.Locations,
				Pattern:    cluster.Locations[0].Pattern,
				Similarity: cluster.Similarity,
				Score:      score,
				Complexity: complexity,
				Rank:       rank,
			})
		}
	}

	// Sort by rank descending, then by hash for deterministic order
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Rank != matches[j].Rank {
			return matches[i].Rank > matches[j].Rank
		}
		return matches[i].Hash < matches[j].Hash
	})

	return matches, stats
}

// TopN returns at most n matches from the slice
func TopN(matches []PatternMatch, n int) []PatternMatch {
	if len(matches) < n {
		n = len(matches)
	}
	return matches[:n]
}

// LoadIgnoredHashes reads <strategy>-ignore.json and returns user-ignored hashes.
func LoadIgnoredHashes(dir string, strategyName string) map[uint64]bool {
	ignorePath := filepath.Join(dir, ".quickdup", strategyName+"-ignore.json")
	data, err := os.ReadFile(ignorePath)
	if err != nil {
		// Create an empty strategy-specific ignore file if it doesn't exist.
		if os.IsNotExist(err) {
			emptyIgnore := IgnoreFile{Ignored: []string{}}
			if jsonData, err := json.MarshalIndent(emptyIgnore, "", "  "); err == nil {
				if mkErr := os.MkdirAll(filepath.Join(dir, ".quickdup"), 0755); mkErr == nil {
					os.WriteFile(ignorePath, jsonData, 0644)
				}
			}
		}
		return map[uint64]bool{}
	}

	var ignoreFile IgnoreFile
	if err := json.Unmarshal(data, &ignoreFile); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not parse %s: %v\n", ignorePath, err)
		return map[uint64]bool{}
	}

	ignored := make(map[uint64]bool)
	for _, hashStr := range ignoreFile.Ignored {
		if hash, ok := parseIgnoredHash(hashStr); ok {
			ignored[hash] = true
		}
	}
	return ignored
}

func parseIgnoredHash(value string) (uint64, bool) {
	hash := strings.TrimSpace(value)
	if strings.HasPrefix(hash, "[") || strings.HasSuffix(hash, "]") {
		if len(hash) < 2 || !strings.HasPrefix(hash, "[") || !strings.HasSuffix(hash, "]") {
			return 0, false
		}
		hash = strings.TrimSpace(hash[1 : len(hash)-1])
	}

	if len(hash) >= 2 && strings.EqualFold(hash[:2], "0x") {
		hash = hash[2:]
	}
	if hash == "" || strings.IndexFunc(hash, func(r rune) bool {
		return !('0' <= r && r <= '9') && !('a' <= r && r <= 'f') && !('A' <= r && r <= 'F')
	}) >= 0 {
		return 0, false
	}

	parsed, err := strconv.ParseUint(hash, 16, 64)
	if err != nil {
		return 0, false
	}
	return parsed, true
}
