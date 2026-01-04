package main

import (
	"fmt"
	"runtime"
	"sort"
	"sync"
)

// filterOverlappingOccurrences removes adjacent occurrences within the same file
// For occurrences at positions N and N+1, only keeps N (the earlier one)
func filterOverlappingOccurrences(locs []PatternLocation, patternLen int) []PatternLocation {
	if len(locs) <= 1 {
		return locs
	}

	// Group by filename
	byFile := make(map[string][]PatternLocation)
	for _, loc := range locs {
		byFile[loc.Filename] = append(byFile[loc.Filename], loc)
	}

	var result []PatternLocation
	for _, fileLocs := range byFile {
		if len(fileLocs) == 1 {
			result = append(result, fileLocs[0])
			continue
		}

		// Sort by EntryIndex
		sort.Slice(fileLocs, func(i, j int) bool {
			return fileLocs[i].EntryIndex < fileLocs[j].EntryIndex
		})

		// Keep non-overlapping: if positions overlap, keep only the first
		lastEnd := -1
		for _, loc := range fileLocs {
			if loc.EntryIndex >= lastEnd {
				result = append(result, loc)
				lastEnd = loc.EntryIndex + patternLen
			}
		}
	}

	return result
}

func detectPatterns(fileData map[string][]Entry, totalFiles int, minOccur int, minSize int, keepOverlaps bool) map[uint64][]PatternLocation {
	allPatterns := make(map[uint64][]PatternLocation)
	numWorkers := runtime.NumCPU()

	// Build file list for parallel iteration
	files := make([]string, 0, len(fileData))
	for f := range fileData {
		files = append(files, f)
	}

	// Step 1: Generate base patterns in parallel (per file)
	basePatterns := generateBasePatternsParallel(fileData, files, minSize, numWorkers)

	// Step 2: Filter base patterns to >= minOccur
	survivors := make(map[uint64][]PatternLocation)
	for hash, locs := range basePatterns {
		if len(locs) >= minOccur {
			survivors[hash] = locs
		}
	}
	previousGen := survivors

	// Step 3: Grow patterns by extending the window
	currentLen := minSize
	for len(survivors) > 0 {
		currentLen++

		// Extend all locations in parallel
		nextPatterns := extendPatternsParallel(survivors, fileData, currentLen, numWorkers)

		// Filter next generation and track which occurrences grew
		grewToChild := make(map[OccurrenceKey]bool)
		survivors = make(map[uint64][]PatternLocation)
		for hash, locs := range nextPatterns {
			if len(locs) >= minOccur {
				survivors[hash] = locs
				for _, loc := range locs {
					grewToChild[OccurrenceKey{loc.Filename, loc.EntryIndex}] = true
				}
			}
		}

		// Add previous generation to results, filtering out occurrences that grew
		prevLen := currentLen - 1
		for hash, locs := range previousGen {
			filteredLocs := make([]PatternLocation, 0, len(locs))
			for _, loc := range locs {
				if !grewToChild[OccurrenceKey{loc.Filename, loc.EntryIndex}] {
					filteredLocs = append(filteredLocs, loc)
				}
			}
			if !keepOverlaps {
				filteredLocs = filterOverlappingOccurrences(filteredLocs, prevLen)
			}
			if len(filteredLocs) >= minOccur {
				allPatterns[hash] = filteredLocs
			}
		}

		previousGen = survivors
	}

	fmt.Printf("Growth stopped at %d lines\n", currentLen-1)
	return allPatterns
}

// generateBasePatternsParallel generates base patterns using parallel workers
func generateBasePatternsParallel(fileData map[string][]Entry, files []string, minSize int, numWorkers int) map[uint64][]PatternLocation {
	result := make(map[uint64][]PatternLocation)
	var mu sync.Mutex

	work := make(chan string, len(files))
	for _, f := range files {
		work <- f
	}
	close(work)

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			local := make(map[uint64][]PatternLocation)

			for filename := range work {
				entries := fileData[filename]
				n := len(entries)

				for i := 0; i <= n-minSize; i++ {
					window := entries[i : i+minSize]
					hash := activeStrategy.Hash(window)
					patternCopy := make([]Entry, len(window))
					copy(patternCopy, window)

					local[hash] = append(local[hash], PatternLocation{
						Filename:   filename,
						LineStart:  entries[i].GetLineNumber(),
						EntryIndex: i,
						Pattern:    patternCopy,
					})
				}
			}

			// Merge local results
			mu.Lock()
			for hash, locs := range local {
				result[hash] = append(result[hash], locs...)
			}
			mu.Unlock()
		}()
	}

	wg.Wait()
	return result
}

// extendPatternsParallel extends all surviving patterns by 1 line using parallel workers
func extendPatternsParallel(survivors map[uint64][]PatternLocation, fileData map[string][]Entry, newLen int, numWorkers int) map[uint64][]PatternLocation {
	// Collect all locations to extend
	var allLocs []PatternLocation
	for _, locs := range survivors {
		allLocs = append(allLocs, locs...)
	}

	if len(allLocs) == 0 {
		return make(map[uint64][]PatternLocation)
	}

	result := make(map[uint64][]PatternLocation)
	var mu sync.Mutex

	// Partition work
	chunkSize := (len(allLocs) + numWorkers - 1) / numWorkers

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		start := i * chunkSize
		if start >= len(allLocs) {
			break
		}
		end := start + chunkSize
		if end > len(allLocs) {
			end = len(allLocs)
		}
		chunk := allLocs[start:end]

		wg.Add(1)
		go func(locs []PatternLocation) {
			defer wg.Done()
			local := make(map[uint64][]PatternLocation)

			for _, loc := range locs {
				entries := fileData[loc.Filename]
				endIdx := loc.EntryIndex + newLen

				if endIdx > len(entries) {
					continue
				}

				window := entries[loc.EntryIndex:endIdx]
				hash := activeStrategy.Hash(window)
				patternCopy := make([]Entry, len(window))
				copy(patternCopy, window)

				local[hash] = append(local[hash], PatternLocation{
					Filename:   loc.Filename,
					LineStart:  loc.LineStart,
					EntryIndex: loc.EntryIndex,
					Pattern:    patternCopy,
				})
			}

			mu.Lock()
			for hash, locs := range local {
				result[hash] = append(result[hash], locs...)
			}
			mu.Unlock()
		}(chunk)
	}

	wg.Wait()
	return result
}
