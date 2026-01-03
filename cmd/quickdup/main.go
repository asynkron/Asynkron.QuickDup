package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Styles
	scoreStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	hashStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	locationStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	lineNumStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("221"))
	summaryStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("82"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
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

	// Phase 1: Parse all files in parallel
	fmt.Printf("Scanning %d files using %d workers...\n", totalFiles, runtime.NumCPU())
	fileData := parseFilesParallel(files)
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

	fmt.Printf("Found %s patterns with %d+ occurrences (showing top %d by score)\n\n",
		summaryStyle.Render(fmt.Sprintf("%d", len(matches))), *minOccur, top)

	for _, m := range matches[:top] {
		fmt.Printf("\n%s %s %s %s:\n",
			scoreStyle.Render(fmt.Sprintf("Score %d", m.Score)),
			dimStyle.Render(fmt.Sprintf("[%d lines, %d unique]", len(m.Pattern), m.UniqueWords)),
			dimStyle.Render(fmt.Sprintf("found %d times", len(m.Locations))),
			hashStyle.Render(fmt.Sprintf("[%016x]", m.Hash)))
		for _, loc := range m.Locations {
			fmt.Printf("  %s%s%s\n",
				locationStyle.Render(loc.Filename),
				dimStyle.Render(":"),
				lineNumStyle.Render(fmt.Sprintf("%d", loc.LineStart)))
		}
	}

	fmt.Printf("\nTotal: %s duplicate patterns\n", summaryStyle.Render(fmt.Sprintf("%d", len(matches))))

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

	fmt.Printf("Results written to: %s\n", locationStyle.Render(outputPath))

	// Write raw patterns file with actual code
	rawPath := filepath.Join(outputDir, "patterns.md")
	if err := writeRawPatterns(rawPath, matches); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing patterns file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Raw patterns written to: %s\n", locationStyle.Render(rawPath))
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

// parseFilesParallel parses all files using a worker pool
func parseFilesParallel(files []string) map[string][]IndentAndWord {
	numWorkers := runtime.NumCPU()
	results := make(map[string][]IndentAndWord)
	var mu sync.Mutex
	var completed atomic.Int64

	// Create work channel
	work := make(chan string, len(files))
	for _, f := range files {
		work <- f
	}
	close(work)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range work {
				entries, err := parseFile(path)
				if err != nil {
					continue // skip files that fail to parse
				}
				mu.Lock()
				results[path] = entries
				mu.Unlock()

				n := completed.Add(1)
				printProgress("Parsing", int(n), len(files))
			}
		}()
	}

	wg.Wait()
	return results
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
			filteredLocs = filterOverlappingOccurrences(filteredLocs, prevLen)
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
func generateBasePatternsParallel(fileData map[string][]IndentAndWord, files []string, minSize int, numWorkers int) map[uint64][]PatternLocation {
	result := make(map[uint64][]PatternLocation)
	var mu sync.Mutex
	var completed atomic.Int64

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
					hash := hashPattern(window)
					patternCopy := make([]IndentAndWord, len(window))
					copy(patternCopy, window)

					local[hash] = append(local[hash], PatternLocation{
						Filename:   filename,
						LineStart:  entries[i].LineNumber,
						EntryIndex: i,
						Pattern:    patternCopy,
					})
				}

				n64 := completed.Add(1)
				printProgress("Analyzing", int(n64), len(files))
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
	clearProgress()
	return result
}

// extendPatternsParallel extends all surviving patterns by 1 line using parallel workers
func extendPatternsParallel(survivors map[uint64][]PatternLocation, fileData map[string][]IndentAndWord, newLen int, numWorkers int) map[uint64][]PatternLocation {
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
				hash := hashPattern(window)
				patternCopy := make([]IndentAndWord, len(window))
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
