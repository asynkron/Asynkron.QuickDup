package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type IndentAndWord struct {
	LineNumber  int
	IndentDelta int
	Word        string
	SourceLine  string // actual source line for reconstruction
}

type PatternLocation struct {
	Filename   string
	LineStart  int
	EntryIndex int             // start position in entries array
	Pattern    []IndentAndWord // the actual pattern at this location
}

type PatternMatch struct {
	Hash        uint64
	Locations   []PatternLocation
	Pattern     []IndentAndWord // representative pattern (first occurrence)
	UniqueWords int             // number of unique words in pattern
	Score       int             // combined score: uniqueWords + length
}

// JSON output structures
type JSONLocation struct {
	Filename  string `json:"filename"`
	LineStart int    `json:"line_start"`
}

type JSONPattern struct {
	Hash        string         `json:"hash"`
	Score       int            `json:"score"`
	Lines       int            `json:"lines"`
	UniqueWords int            `json:"unique_words"`
	Occurrences int            `json:"occurrences"`
	Pattern     []string       `json:"pattern"`
	Locations   []JSONLocation `json:"locations"`
}

type JSONOutput struct {
	TotalPatterns int           `json:"total_patterns"`
	Patterns      []JSONPattern `json:"patterns"`
}

// IgnoreFile represents the structure of ignore.json
type IgnoreFile struct {
	Description string   `json:"description"`
	Ignored     []string `json:"ignored"`
}

// Separators for word extraction
const separators = " \t:.;{}()[]#!<>=,\n\r"

// Default comment prefixes by file extension
var commentPrefixes = map[string]string{
	// C-style
	".go":    "//",
	".c":     "//",
	".h":     "//",
	".cpp":   "//",
	".hpp":   "//",
	".cc":    "//",
	".cxx":   "//",
	".java":  "//",
	".js":    "//",
	".jsx":   "//",
	".ts":    "//",
	".tsx":   "//",
	".cs":    "//",
	".swift": "//",
	".kt":    "//",
	".kts":   "//",
	".scala": "//",
	".rs":    "//",
	".php":   "//",
	".m":     "//",
	".mm":    "//",
	".dart":  "//",
	".v":     "//",
	".zig":   "//",
	// Hash-style
	".py":     "#",
	".rb":     "#",
	".sh":     "#",
	".bash":   "#",
	".zsh":    "#",
	".pl":     "#",
	".pm":     "#",
	".r":      "#",
	".R":      "#",
	".yaml":   "#",
	".yml":    "#",
	".toml":   "#",
	".tf":     "#",
	".cmake":  "#",
	".make":   "#",
	".mk":     "#",
	".ps1":    "#",
	".nim":    "#",
	".jl":     "#",
	".ex":     "#",
	".exs":    "#",
	".cr":     "#",
	// Double-dash style
	".sql":  "--",
	".lua":  "--",
	".hs":   "--",
	".elm":  "--",
	".ada":  "--",
	".vhdl": "--",
	// Semicolon style
	".lisp": ";",
	".cl":   ";",
	".scm":  ";",
	".clj":  ";",
	".cljs": ";",
	".el":   ";",
	".asm":  ";",
	// Percent style
	".tex":    "%",
	".mat":    "%", // MATLAB
	".erl":    "%",
	".hrl":    "%",
	".pro":    "%",
	".prolog": "%",
	// Apostrophe style
	".vb":  "'",
	".bas": "'",
	".vbs": "'",
}

// Blocklist of common useless patterns (computed at init)
var blockedHashes map[uint64]bool

func init() {
	blockedHashes = make(map[uint64]bool)

	// Common patterns to ignore (closing braces, function boundaries)
	uselessPatterns := [][]IndentAndWord{
		// } }
		{{IndentDelta: -4, Word: "}"}, {IndentDelta: -4, Word: "}"}},
		// } } }
		{{IndentDelta: -4, Word: "}"}, {IndentDelta: -4, Word: "}"}, {IndentDelta: -4, Word: "}"}},
		// return }
		{{IndentDelta: 0, Word: "return"}, {IndentDelta: -4, Word: "}"}},
		// +4 return }
		{{IndentDelta: 4, Word: "return"}, {IndentDelta: -4, Word: "}"}},
		// } return }
		{{IndentDelta: -4, Word: "}"}, {IndentDelta: 0, Word: "return"}, {IndentDelta: -4, Word: "}"}},
		// } func
		{{IndentDelta: -4, Word: "}"}, {IndentDelta: 0, Word: "func"}},
		// } return
		{{IndentDelta: -4, Word: "}"}, {IndentDelta: 0, Word: "return"}},
	}

	for _, pattern := range uselessPatterns {
		blockedHashes[hashPattern(pattern)] = true
	}
}

var commentPrefix string

func main() {
	path := flag.String("path", ".", "Path to scan")
	ext := flag.String("ext", ".go", "File extension to scan")
	minOccur := flag.Int("min", 3, "Minimum occurrences to report")
	minScore := flag.Int("min-score", 3, "Minimum score to report (uniqueWords)")
	minSize := flag.Int("min-size", 3, "Base pattern size to start growing from")
	topN := flag.Int("top", 10, "Show top N matches by pattern length")
	comment := flag.String("comment", "", "Override comment prefix (auto-detected by extension)")
	flag.Parse()

	// Auto-detect comment prefix from extension, allow override
	if *comment != "" {
		commentPrefix = *comment
	} else if prefix, ok := commentPrefixes[*ext]; ok {
		commentPrefix = prefix
	} else {
		commentPrefix = "//" // fallback default
	}

	folder := *path
	extension := *ext

	// Load user-ignored hashes from ignore.json
	if ignored := loadIgnoredHashes(folder); ignored > 0 {
		fmt.Printf("Loaded %d ignored patterns from ignore.json\n", ignored)
	}

	// First pass: count files
	var files []string
	err := filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, extension) {
			files = append(files, path)
		}
		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error walking directory: %v\n", err)
		os.Exit(1)
	}

	totalFiles := len(files)
	if totalFiles == 0 {
		fmt.Printf("No %s files found in %s\n", extension, folder)
		os.Exit(0)
	}

	// Phase 1: Parse all files with progress
	fileData := make(map[string][]IndentAndWord)

	fmt.Printf("Scanning %d files...\n", totalFiles)
	for i, path := range files {
		entries, err := parseFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nWarning: could not parse %s: %v\n", path, err)
			continue
		}
		fileData[path] = entries
		printProgress("Parsing", i+1, totalFiles)
	}
	clearProgress()
	fmt.Printf("Parsed %d files\n", len(fileData))

	// Phase 2: Pattern detection with growth
	fmt.Printf("Detecting patterns...\n")
	patterns := detectPatterns(fileData, len(fileData), *minOccur, *minSize)

	// Filter and collect matches
	var matches []PatternMatch
	skippedBlocked := 0
	skippedLowScore := 0
	for hash, locs := range patterns {
		if blockedHashes[hash] {
			skippedBlocked++
			continue
		}
		if len(locs) >= *minOccur {
			pattern := locs[0].Pattern
			uniqueWords := countUniqueWords(pattern)
			score := uniqueWords
			if score < *minScore {
				skippedLowScore++
				continue
			}
			matches = append(matches, PatternMatch{
				Hash:        hash,
				Locations:   locs,
				Pattern:     pattern,
				UniqueWords: uniqueWords,
				Score:       score,
			})
		}
	}
	if skippedBlocked > 0 {
		fmt.Printf("Filtered %d common patterns\n", skippedBlocked)
	}
	if skippedLowScore > 0 {
		fmt.Printf("Filtered %d low-score patterns (score < %d)\n", skippedLowScore, *minScore)
	}

	// Sort by combined score (uniqueWords + length), descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	// Limit to top N
	top := *topN
	if len(matches) < top {
		top = len(matches)
	}

	fmt.Printf("Found %d patterns with %d+ occurrences (showing top %d by score)\n\n", len(matches), *minOccur, top)

	for _, m := range matches[:top] {
		fmt.Printf("\nScore %d [%d lines, %d unique] found %d times:\n", m.Score, len(m.Pattern), m.UniqueWords, len(m.Locations))
		fmt.Printf("Locations:\n")
		for _, loc := range m.Locations {
			fmt.Printf("  • %s:%d\n", loc.Filename, loc.LineStart)
		}
	}

	fmt.Printf("Total: %d duplicate patterns\n", len(matches))

	// Build JSON output (includes ALL patterns, not just top N)
	jsonOutput := JSONOutput{
		TotalPatterns: len(matches),
		Patterns:      make([]JSONPattern, 0, len(matches)),
	}

	for _, m := range matches {
		// Convert pattern to string representation
		patternStrs := make([]string, len(m.Pattern))
		for i, entry := range m.Pattern {
			if entry.IndentDelta > 0 {
				patternStrs[i] = fmt.Sprintf("+%d %s", entry.IndentDelta, entry.Word)
			} else {
				patternStrs[i] = fmt.Sprintf("%d %s", entry.IndentDelta, entry.Word)
			}
		}

		// Convert locations
		locs := make([]JSONLocation, len(m.Locations))
		for i, loc := range m.Locations {
			locs[i] = JSONLocation{
				Filename:  loc.Filename,
				LineStart: loc.LineStart,
			}
		}

		jsonOutput.Patterns = append(jsonOutput.Patterns, JSONPattern{
			Hash:        fmt.Sprintf("%016x", m.Hash),
			Score:       m.Score,
			Lines:       len(m.Pattern),
			UniqueWords: m.UniqueWords,
			Occurrences: len(m.Locations),
			Pattern:     patternStrs,
			Locations:   locs,
		})
	}

	// Create .quickdup directory and write JSON
	outputDir := filepath.Join(*path, ".quickdup")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	outputPath := filepath.Join(outputDir, "results.json")
	jsonData, err := json.MarshalIndent(jsonOutput, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(outputPath, jsonData, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing JSON file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Results written to: %s\n", outputPath)

	// Write raw patterns file with actual code
	rawPath := filepath.Join(outputDir, "patterns.md")
	if err := writeRawPatterns(rawPath, matches); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing patterns file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Raw patterns written to: %s\n", rawPath)
}

func writeRawPatterns(path string, matches []PatternMatch) error {
	var sb strings.Builder

	sb.WriteString("# Duplicate Code Patterns\n\n")
	sb.WriteString("This file contains actual code snippets for each detected pattern.\n")
	sb.WriteString("Review these to determine if they represent refactorable duplications.\n\n")

	for i, m := range matches {
		sb.WriteString(fmt.Sprintf("---\n\n## Pattern %d [%016x] (Score: %d, Occurrences: %d)\n\n", i+1, m.Hash, m.Score, len(m.Locations)))

		// Show up to 4 occurrences
		maxShow := 4
		if len(m.Locations) < maxShow {
			maxShow = len(m.Locations)
		}

		for j := 0; j < maxShow; j++ {
			loc := m.Locations[j]
			sb.WriteString(fmt.Sprintf("### %s:%d\n\n", loc.Filename, loc.LineStart))
			sb.WriteString("```\n")

			// Use stored source lines from the pattern, normalized
			normalizedLines := normalizeIndent(loc.Pattern)
			for _, line := range normalizedLines {
				sb.WriteString(line + "\n")
			}
			sb.WriteString("```\n\n")
		}

		if len(m.Locations) > maxShow {
			sb.WriteString(fmt.Sprintf("*... and %d more occurrences*\n\n", len(m.Locations)-maxShow))
		}
	}

	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// loadIgnoredHashes reads ignore.json and adds hashes to the blocked list
func loadIgnoredHashes(dir string) int {
	ignorePath := filepath.Join(dir, ".quickdup", "ignore.json")
	data, err := os.ReadFile(ignorePath)
	if err != nil {
		return 0 // File doesn't exist or can't be read, that's fine
	}

	var ignoreFile IgnoreFile
	if err := json.Unmarshal(data, &ignoreFile); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not parse %s: %v\n", ignorePath, err)
		return 0
	}

	count := 0
	for _, hashStr := range ignoreFile.Ignored {
		var hash uint64
		if _, err := fmt.Sscanf(hashStr, "%x", &hash); err == nil {
			blockedHashes[hash] = true
			count++
		}
	}
	return count
}

func parseFile(path string) ([]IndentAndWord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []IndentAndWord
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	prevIndent := 0

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

		// Skip whitespace-only lines
		if isWhitespaceOnly(line) {
			continue
		}

		// Skip comment-only lines
		if isCommentOnly(line) {
			continue
		}

		indent := calculateIndent(line)
		word := extractFirstWord(line)

		indentDelta := indent - prevIndent
		prevIndent = indent

		entries = append(entries, IndentAndWord{
			LineNumber:  lineNumber,
			IndentDelta: indentDelta,
			Word:        word,
			SourceLine:  line,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func isWhitespaceOnly(line string) bool {
	for _, r := range line {
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}

func isCommentOnly(line string) bool {
	if commentPrefix == "" {
		return false
	}
	trimmed := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(trimmed, commentPrefix)
}

func calculateIndent(line string) int {
	indent := 0
	for _, r := range line {
		switch r {
		case ' ':
			indent++
		case '\t':
			indent += 4
		default:
			return indent
		}
	}
	return indent
}

// normalizeIndent strips the minimum common leading whitespace from all lines
func normalizeIndent(pattern []IndentAndWord) []string {
	if len(pattern) == 0 {
		return nil
	}

	// Find minimum leading whitespace (count actual chars, not visual indent)
	minLeading := -1
	for _, entry := range pattern {
		leading := 0
		for _, r := range entry.SourceLine {
			if r == ' ' || r == '\t' {
				leading++
			} else {
				break
			}
		}
		// Only count non-empty lines
		if len(strings.TrimSpace(entry.SourceLine)) > 0 {
			if minLeading == -1 || leading < minLeading {
				minLeading = leading
			}
		}
	}

	if minLeading <= 0 {
		// No normalization needed
		result := make([]string, len(pattern))
		for i, entry := range pattern {
			result[i] = entry.SourceLine
		}
		return result
	}

	// Strip minLeading characters from each line
	result := make([]string, len(pattern))
	for i, entry := range pattern {
		if len(entry.SourceLine) >= minLeading {
			result[i] = entry.SourceLine[minLeading:]
		} else {
			result[i] = entry.SourceLine
		}
	}
	return result
}

func extractFirstWord(line string) string {
	// Skip leading whitespace
	start := 0
	for i, r := range line {
		if r != ' ' && r != '\t' {
			start = i
			break
		}
	}

	// Find end of word (first separator)
	trimmed := line[start:]
	end := len(trimmed)
	for i, r := range trimmed {
		if strings.ContainsRune(separators, r) {
			end = i
			break
		}
	}

	// If no word found (line starts with separator), use the first character
	if end == 0 && len(trimmed) > 0 {
		return string(trimmed[0])
	}

	return trimmed[:end]
}

// Count unique words in a pattern
func countUniqueWords(pattern []IndentAndWord) int {
	seen := make(map[string]bool)
	for _, entry := range pattern {
		seen[entry.Word] = true
	}
	return len(seen)
}

// OccurrenceKey uniquely identifies an occurrence by file and position
type OccurrenceKey struct {
	Filename   string
	EntryIndex int
}

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

func detectPatterns(fileData map[string][]IndentAndWord, totalFiles int, minOccur int, minSize int) map[uint64][]PatternLocation {
	allPatterns := make(map[uint64][]PatternLocation)

	// Step 1: Generate all minSize-line patterns
	fmt.Printf("Finding %d-line base patterns...\n", minSize)
	basePatterns := make(map[uint64][]PatternLocation)

	processed := 0
	for filename, entries := range fileData {
		n := len(entries)

		for i := 0; i <= n-minSize; i++ {
			window := entries[i : i+minSize]
			hash := hashPattern(window)

			// Copy window to avoid slice aliasing issues
			patternCopy := make([]IndentAndWord, len(window))
			copy(patternCopy, window)

			basePatterns[hash] = append(basePatterns[hash], PatternLocation{
				Filename:   filename,
				LineStart:  entries[i].LineNumber,
				EntryIndex: i,
				Pattern:    patternCopy,
			})
		}
		processed++
		printProgress("Analyzing", processed, totalFiles)
	}
	clearProgress()

	// Step 2: Filter base patterns to >= minOccur
	survivors := make(map[uint64][]PatternLocation)
	for hash, locs := range basePatterns {
		if len(locs) >= minOccur {
			survivors[hash] = locs
		}
	}
	fmt.Printf("  %d-line: %d patterns with %d+ occurrences\n", minSize, len(survivors), minOccur)

	// Track previous generation for deferred processing
	previousGen := survivors

	// Step 3: Grow patterns by extending the window
	currentLen := minSize
	for len(survivors) > 0 {
		currentLen++
		nextPatterns := make(map[uint64][]PatternLocation)

		// For each surviving location, try to extend by 1
		for _, locs := range survivors {
			for _, loc := range locs {
				entries := fileData[loc.Filename]
				endIdx := loc.EntryIndex + currentLen // new end index

				// Check bounds
				if endIdx > len(entries) {
					continue
				}

				// Get the extended window and hash it
				window := entries[loc.EntryIndex:endIdx]
				hash := hashPattern(window)

				// Copy window
				patternCopy := make([]IndentAndWord, len(window))
				copy(patternCopy, window)

				nextPatterns[hash] = append(nextPatterns[hash], PatternLocation{
					Filename:   loc.Filename,
					LineStart:  loc.LineStart,
					EntryIndex: loc.EntryIndex,
					Pattern:    patternCopy,
				})
			}
		}

		// Filter next generation to >= minOccur and track which occurrences grew
		grewToChild := make(map[OccurrenceKey]bool)
		survivors = make(map[uint64][]PatternLocation)
		for hash, locs := range nextPatterns {
			if len(locs) >= minOccur {
				survivors[hash] = locs
				// Mark these occurrences as having grown into surviving children
				for _, loc := range locs {
					grewToChild[OccurrenceKey{loc.Filename, loc.EntryIndex}] = true
				}
			}
		}

		// Add previous generation to results, filtering out occurrences that grew
		prevLen := currentLen - 1
		for hash, locs := range previousGen {
			// Filter out occurrences that grew into children
			filteredLocs := make([]PatternLocation, 0, len(locs))
			for _, loc := range locs {
				if !grewToChild[OccurrenceKey{loc.Filename, loc.EntryIndex}] {
					filteredLocs = append(filteredLocs, loc)
				}
			}

			// Filter out adjacent/overlapping occurrences within same file
			filteredLocs = filterOverlappingOccurrences(filteredLocs, prevLen)

			// Only add if still meets minOccur
			if len(filteredLocs) >= minOccur {
				allPatterns[hash] = filteredLocs
			}
		}

		if len(survivors) > 0 {
			fmt.Printf("  %d-line: %d patterns with %d+ occurrences\n", currentLen, len(survivors), minOccur)
		}

		previousGen = survivors
	}

	fmt.Printf("Growth stopped at %d lines\n", currentLen-1)
	return allPatterns
}

func hashPattern(window []IndentAndWord) uint64 {
	h := fnv.New64a()

	for _, entry := range window {
		// Write indent delta as bytes
		fmt.Fprintf(h, "%d|%s\n", entry.IndentDelta, entry.Word)
	}

	return h.Sum64()
}

const progressBarWidth = 40

func printProgress(label string, current, total int) {
	percent := float64(current) / float64(total)
	filled := int(percent * progressBarWidth)

	bar := strings.Repeat("█", filled) + strings.Repeat("░", progressBarWidth-filled)
	fmt.Printf("\r%s [%s] %3d%% (%d/%d)", label, bar, int(percent*100), current, total)
}

func clearProgress() {
	fmt.Print("\r" + strings.Repeat(" ", 80) + "\r")
}
