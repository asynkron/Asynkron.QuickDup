package main

import (
	"bufio"
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Default strategy
	defaultStrategy Strategy = &WordIndentStrategy{}

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
	Similarity  float64         // average token similarity across occurrences (0.0-1.0)
	Score       int             // combined score: uniqueWords + similarity bonus
}

// Strategy defines how patterns are detected and scored
type Strategy interface {
	Name() string
	ShouldSkip(entry IndentAndWord) bool
	Hash(entries []IndentAndWord) uint64
	Signature(entries []IndentAndWord) string
	Score(entries []IndentAndWord, similarity float64) int
}

// WordIndentStrategy matches patterns by indent delta and first word
type WordIndentStrategy struct{}

func (s *WordIndentStrategy) Name() string {
	return "word-indent"
}

func (s *WordIndentStrategy) ShouldSkip(entry IndentAndWord) bool {
	return isWhitespaceOnly(entry.SourceLine) || isCommentOnly(entry.SourceLine)
}

func (s *WordIndentStrategy) Hash(entries []IndentAndWord) uint64 {
	h := fnv.New64a()
	for _, entry := range entries {
		fmt.Fprintf(h, "%d|%s\n", entry.IndentDelta, entry.Word)
	}
	return h.Sum64()
}

func (s *WordIndentStrategy) Signature(entries []IndentAndWord) string {
	var parts []string
	for _, entry := range entries {
		parts = append(parts, entry.Word)
	}
	return strings.Join(parts, " ")
}

func (s *WordIndentStrategy) Score(entries []IndentAndWord, similarity float64) int {
	seen := make(map[string]bool)
	for _, entry := range entries {
		seen[entry.Word] = true
	}
	uniqueWords := len(seen)
	adjustedSim := similarity*2 - 1.0
	if adjustedSim < 0 {
		adjustedSim = 0
	}
	return int(float64(uniqueWords) * adjustedSim)
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
	Similarity  float64        `json:"similarity"`
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

// CachedFile stores parsed entries with mod time for incremental parsing
type CachedFile struct {
	ModTime int64
	Entries []IndentAndWord
}

// FileCache stores all cached file data
type FileCache struct {
	Version int // cache format version for invalidation
	Files   map[string]CachedFile
}

const cacheVersion = 1

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
	minScore := flag.Int("min-score", 3, "Minimum score to report (uniqueWords × adjusted similarity)")
	minSize := flag.Int("min-size", 3, "Base pattern size to start growing from")
	minSimilarity := flag.Float64("min-similarity", 0.5, "Minimum token similarity between occurrences (0.0-1.0)")
	topN := flag.Int("top", 10, "Show top N matches by pattern length")
	comment := flag.String("comment", "", "Override comment prefix (auto-detected by extension)")
	noCache := flag.Bool("no-cache", false, "Disable incremental caching, force full re-parse")
	githubAnnotations := flag.Bool("github-annotations", false, "Output GitHub Actions annotations for inline PR comments")
	githubLevel := flag.String("github-level", "warning", "GitHub annotation level: notice, warning, or error")
	gitDiff := flag.String("git-diff", "", "Only annotate files changed vs this git ref (e.g., origin/main)")
	exclude := flag.String("exclude", "", "Exclude files matching patterns (comma-separated, e.g., '*.pb.go,*_gen.go')")
	compare := flag.String("compare", "", "Compare duplicates between two commits (format: base..head)")
	flag.Parse()

	// Handle compare mode
	if *compare != "" {
		parts := strings.Split(*compare, "..")
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "Error: --compare requires format 'base..head'\n")
			os.Exit(1)
		}
		baseRef, headRef := parts[0], parts[1]
		// Extract subdir from path if it's not "."
		subdir := ""
		if *path != "." {
			subdir = *path
		}
		runCompare(baseRef, headRef, subdir, *ext, *exclude, *minOccur, *minScore, *minSize, *minSimilarity)
		return
	}

	// Parse exclude patterns
	var excludePatterns []string
	if *exclude != "" {
		for _, p := range strings.Split(*exclude, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				excludePatterns = append(excludePatterns, p)
			}
		}
	}

	// Build set of changed files if --git-diff is specified
	changedFiles := make(map[string]bool)
	if *gitDiff != "" {
		cmd := exec.Command("git", "diff", "--name-only", *gitDiff)
		output, err := cmd.Output()
		if err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
				if line != "" {
					changedFiles[line] = true
				}
			}
		}
	}

	startTime := time.Now()

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
		if !info.IsDir() && strings.EqualFold(filepath.Ext(path), extension) {
			// Check exclude patterns
			excluded := false
			for _, pattern := range excludePatterns {
				if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
					excluded = true
					break
				}
			}
			if !excluded {
				files = append(files, path)
			}
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

	// Phase 1: Parse all files in parallel (with caching)
	fmt.Printf("Scanning %d files using %d workers...\n", totalFiles, runtime.NumCPU())

	parseStart := time.Now()
	var cache *FileCache
	if !*noCache {
		cache = loadCache(folder)
	}

	fileData, cacheHits, cacheMisses := parseFilesWithCache(files, cache)

	// Save updated cache
	if !*noCache && cacheMisses > 0 {
		saveCache(folder, files, fileData)
	}
	parseTime := time.Since(parseStart)

	// Count total lines of code (non-blank, non-comment)
	totalLines := 0
	for _, entries := range fileData {
		totalLines += len(entries)
	}

	if cacheHits > 0 {
		fmt.Printf("Parsed %d files (%d cached, %d parsed) in %s (%d lines of code)\n", len(fileData), cacheHits, cacheMisses, parseTime.Round(time.Millisecond), totalLines)
	} else {
		fmt.Printf("Parsed %d files in %s (%d lines of code)\n", len(fileData), parseTime.Round(time.Millisecond), totalLines)
	}

	// Phase 2: Pattern detection with growth
	detectStart := time.Now()
	fmt.Printf("Detecting patterns...\n")
	patterns := detectPatterns(fileData, len(fileData), *minOccur, *minSize)
	detectTime := time.Since(detectStart)
	fmt.Printf("Pattern detection took %s\n", detectTime.Round(time.Millisecond))

	// Filter and collect matches (parallel token similarity)
	filterStart := time.Now()

	// First pass: filter blocked and low-score (cheap checks)
	type candidate struct {
		hash        uint64
		locs        []PatternLocation
		pattern     []IndentAndWord
		uniqueWords int
		score       int
	}
	var candidates []candidate
	skippedBlocked := 0
	for hash, locs := range patterns {
		if blockedHashes[hash] {
			skippedBlocked++
			continue
		}
		if len(locs) >= *minOccur {
			pattern := locs[0].Pattern
			uniqueWords := countUniqueWords(pattern)
			// Pre-filter: need at least 3 unique words to be worth computing similarity
			if uniqueWords < 3 {
				continue
			}
			candidates = append(candidates, candidate{hash, locs, pattern, uniqueWords, 0})
		}
	}

	// Second pass: parallel token similarity check
	type similarityResult struct {
		index      int
		similarity float64
	}
	results := make([]similarityResult, len(candidates))
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
				sim := computeAverageTokenSimilarity(candidates[idx].locs)
				results[idx] = similarityResult{idx, sim}
			}
		}()
	}
	wg.Wait()

	// Collect matches that pass similarity and score thresholds
	var matches []PatternMatch
	skippedLowSimilarity := 0
	skippedLowScore := 0
	for _, r := range results {
		if r.similarity < *minSimilarity {
			skippedLowSimilarity++
			continue
		}
		c := candidates[r.index]
		// Score = uniqueWords × adjusted similarity
		// Adjusted similarity maps 50%→0, 100%→1 (50% is the noise floor)
		adjustedSim := r.similarity*2 - 1.0
		if adjustedSim < 0 {
			adjustedSim = 0
		}
		score := int(float64(c.uniqueWords) * adjustedSim)
		if score < *minScore {
			skippedLowScore++
			continue
		}
		matches = append(matches, PatternMatch{
			Hash:        c.hash,
			Locations:   c.locs,
			Pattern:     c.pattern,
			UniqueWords: c.uniqueWords,
			Similarity:  r.similarity,
			Score:       score,
		})
	}
	filterTime := time.Since(filterStart)
	fmt.Printf("Filtering took %s\n", filterTime.Round(time.Millisecond))

	if skippedBlocked > 0 {
		fmt.Printf("Filtered %d common patterns\n", skippedBlocked)
	}
	if skippedLowScore > 0 {
		fmt.Printf("Filtered %d low-score patterns (score < %d)\n", skippedLowScore, *minScore)
	}
	if skippedLowSimilarity > 0 {
		fmt.Printf("Filtered %d low-similarity patterns (similarity < %.0f%%)\n", skippedLowSimilarity, *minSimilarity*100)
	}

	// Sort by combined score (uniqueWords + similarity bonus), descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	// Limit to top N
	top := *topN
	if len(matches) < top {
		top = len(matches)
	}

	// GitHub Actions annotations output
	if *githubAnnotations {
		annotationCount := 0
		for _, m := range matches[:top] {
			// Emit annotation for first location of each pattern
			loc := m.Locations[0]
			// Skip if --git-diff is set and file is not in changed files
			if *gitDiff != "" && !changedFiles[loc.Filename] {
				continue
			}
			otherLocs := make([]string, 0, len(m.Locations)-1)
			for _, other := range m.Locations[1:] {
				otherLocs = append(otherLocs, fmt.Sprintf("%s:%d", filepath.Base(other.Filename), other.LineStart))
			}
			endLine := loc.LineStart + len(m.Pattern) - 1
			msg := fmt.Sprintf("Duplicate code also at: %s", strings.Join(otherLocs, ", "))
			fmt.Printf("::%s file=%s,line=%d,endLine=%d,title=Duplicate (%d lines, %.0f%% similar, score %d)::%s\n",
				*githubLevel, loc.Filename, loc.LineStart, endLine, len(m.Pattern), m.Similarity*100, m.Score, msg)
			annotationCount++
		}
		if annotationCount > 0 {
			fmt.Printf("\n")
		}
	}

	fmt.Printf("Found %s patterns with %d+ occurrences (showing top %d by score)\n\n",
		summaryStyle.Render(fmt.Sprintf("%d", len(matches))), *minOccur, top)

	for _, m := range matches[:top] {
		fmt.Printf("\n%s %s %s %s %s:\n",
			scoreStyle.Render(fmt.Sprintf("Score %d", m.Score)),
			dimStyle.Render(fmt.Sprintf("[%d lines, %d unique]", len(m.Pattern), m.UniqueWords)),
			dimStyle.Render(fmt.Sprintf("%.0f%% similar", m.Similarity*100)),
			dimStyle.Render(fmt.Sprintf("found %d times", len(m.Locations))),
			hashStyle.Render(fmt.Sprintf("[%016x]", m.Hash)))
		for _, loc := range m.Locations {
			fmt.Printf("  %s%s%s\n",
				locationStyle.Render(loc.Filename),
				dimStyle.Render(":"),
				lineNumStyle.Render(fmt.Sprintf("%d", loc.LineStart)))
		}
	}

	// Hotspot analysis: count duplicated lines per file
	fileDupLines := make(map[string]int)
	for _, m := range matches {
		patternLen := len(m.Pattern)
		for _, loc := range m.Locations {
			fileDupLines[loc.Filename] += patternLen
		}
	}

	// Sort files by duplicated line count
	type fileHotspot struct {
		filename string
		lines    int
	}
	var hotspots []fileHotspot
	for f, lines := range fileDupLines {
		hotspots = append(hotspots, fileHotspot{f, lines})
	}
	sort.Slice(hotspots, func(i, j int) bool {
		return hotspots[i].lines > hotspots[j].lines
	})

	// Show top 5 hotspots
	if len(hotspots) > 0 {
		fmt.Printf("\n%s\n", summaryStyle.Render("Duplication hotspots (lines):"))
		showHotspots := 5
		if len(hotspots) < showHotspots {
			showHotspots = len(hotspots)
		}
		for i := 0; i < showHotspots; i++ {
			fmt.Printf("  %s %s\n",
				lineNumStyle.Render(fmt.Sprintf("%4d", hotspots[i].lines)),
				locationStyle.Render(hotspots[i].filename))
		}
	}

	elapsed := time.Since(startTime)
	fmt.Printf("\nTotal: %s duplicate patterns in %s files (%s lines) in %s\n",
		summaryStyle.Render(fmt.Sprintf("%d", len(matches))),
		summaryStyle.Render(fmt.Sprintf("%d", len(fileData))),
		summaryStyle.Render(fmt.Sprintf("%d", totalLines)),
		summaryStyle.Render(elapsed.Round(time.Millisecond).String()))

	// Skip file output in GitHub Actions mode
	if *githubAnnotations {
		return
	}

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
			Similarity:  m.Similarity,
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
		// Create empty ignore.json if it doesn't exist
		if os.IsNotExist(err) {
			emptyIgnore := IgnoreFile{Ignored: []string{}}
			if jsonData, err := json.MarshalIndent(emptyIgnore, "", "  "); err == nil {
				os.WriteFile(ignorePath, jsonData, 0644)
			}
		}
		return 0
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

// loadCache loads the file cache from disk
func loadCache(dir string) *FileCache {
	cachePath := filepath.Join(dir, ".quickdup", "cache.gob")
	file, err := os.Open(cachePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	var cache FileCache
	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(&cache); err != nil {
		return nil
	}

	// Check version
	if cache.Version != cacheVersion {
		return nil
	}

	return &cache
}

// saveCache saves the file cache to disk
func saveCache(dir string, files []string, fileData map[string][]IndentAndWord) {
	// Build cache from current file data
	cache := FileCache{
		Version: cacheVersion,
		Files:   make(map[string]CachedFile),
	}

	for _, path := range files {
		entries, ok := fileData[path]
		if !ok {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		cache.Files[path] = CachedFile{
			ModTime: info.ModTime().UnixNano(),
			Entries: entries,
		}
	}

	// Ensure directory exists
	cacheDir := filepath.Join(dir, ".quickdup")
	os.MkdirAll(cacheDir, 0755)

	cachePath := filepath.Join(cacheDir, "cache.gob")
	file, err := os.Create(cachePath)
	if err != nil {
		return // silently fail
	}
	defer file.Close()

	encoder := gob.NewEncoder(file)
	encoder.Encode(cache)
}

// parseFilesWithCache parses files using cache when possible
func parseFilesWithCache(files []string, cache *FileCache) (map[string][]IndentAndWord, int, int) {
	numWorkers := runtime.NumCPU()
	results := make(map[string][]IndentAndWord)
	var mu sync.Mutex
	var cacheHits atomic.Int64
	var cacheMisses atomic.Int64

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
				var entries []IndentAndWord
				var fromCache bool

				// Check cache
				if cache != nil {
					if cached, ok := cache.Files[path]; ok {
						info, err := os.Stat(path)
						if err == nil && info.ModTime().UnixNano() == cached.ModTime {
							entries = cached.Entries
							fromCache = true
						}
					}
				}

				// Parse if not cached
				if !fromCache {
					var err error
					entries, err = parseFile(path)
					if err != nil {
						continue // skip files that fail to parse
					}
					cacheMisses.Add(1)
				} else {
					cacheHits.Add(1)
				}

				mu.Lock()
				results[path] = entries
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return results, int(cacheHits.Load()), int(cacheMisses.Load())
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

		indent := calculateIndent(line)
		word := extractFirstWord(line)
		indentDelta := indent - prevIndent

		entry := IndentAndWord{
			LineNumber:  lineNumber,
			IndentDelta: indentDelta,
			Word:        word,
			SourceLine:  line,
		}

		if defaultStrategy.ShouldSkip(entry) {
			continue
		}

		prevIndent = indent
		entries = append(entries, entry)
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
	return defaultStrategy.Hash(window)
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
func tokenizePattern(pattern []IndentAndWord) []string {
	var tokens []string
	for _, entry := range pattern {
		tokens = append(tokens, tokenizeLine(entry.SourceLine)...)
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

// runCompare compares duplicate patterns between two git commits
func runCompare(baseRef, headRef, subdir, ext, exclude string, minOccur, minScore, minSize int, minSimilarity float64) {
	fmt.Printf("Comparing duplicates: %s -> %s\n", baseRef, headRef)
	if subdir != "" {
		fmt.Printf("Subdirectory: %s\n", subdir)
	}
	fmt.Println()

	// Create temporary worktrees
	baseDir, err := os.MkdirTemp("", "quickdup-base-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(baseDir)

	headDir, err := os.MkdirTemp("", "quickdup-head-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(headDir)

	// Create worktrees
	fmt.Printf("Creating worktree for %s...\n", baseRef)
	cmd := exec.Command("git", "worktree", "add", "--detach", baseDir, baseRef)
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating base worktree: %v\n%s\n", err, output)
		os.Exit(1)
	}
	defer exec.Command("git", "worktree", "remove", "--force", baseDir).Run()

	fmt.Printf("Creating worktree for %s...\n", headRef)
	cmd = exec.Command("git", "worktree", "add", "--detach", headDir, headRef)
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating head worktree: %v\n%s\n", err, output)
		os.Exit(1)
	}
	defer exec.Command("git", "worktree", "remove", "--force", headDir).Run()

	// Build args for quickdup
	args := []string{
		"-ext", ext,
		"-min", fmt.Sprintf("%d", minOccur),
		"-min-score", fmt.Sprintf("%d", minScore),
		"-min-size", fmt.Sprintf("%d", minSize),
		"-min-similarity", fmt.Sprintf("%f", minSimilarity),
		"--no-cache",
	}
	if exclude != "" {
		args = append(args, "-exclude", exclude)
	}

	// Determine scan paths (worktree root or subdir within)
	baseScanPath := baseDir
	headScanPath := headDir
	if subdir != "" {
		baseScanPath = filepath.Join(baseDir, subdir)
		headScanPath = filepath.Join(headDir, subdir)
	}

	// Run quickdup on base
	fmt.Printf("\nScanning %s...\n", baseRef)
	baseArgs := append([]string{"-path", baseScanPath}, args...)
	cmd = exec.Command(os.Args[0], baseArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: quickdup on base returned error: %v\n", err)
	}

	// Run quickdup on head
	fmt.Printf("\nScanning %s...\n", headRef)
	headArgs := append([]string{"-path", headScanPath}, args...)
	cmd = exec.Command(os.Args[0], headArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: quickdup on head returned error: %v\n", err)
	}

	// Load results from both
	baseResults := loadJSONResults(filepath.Join(baseScanPath, ".quickdup", "results.json"))
	headResults := loadJSONResults(filepath.Join(headScanPath, ".quickdup", "results.json"))

	// Build hash -> occurrences maps
	baseOccur := make(map[string]int)
	for _, p := range baseResults.Patterns {
		baseOccur[p.Hash] = p.Occurrences
	}

	headOccur := make(map[string]int)
	headPatterns := make(map[string]JSONPattern)
	for _, p := range headResults.Patterns {
		headOccur[p.Hash] = p.Occurrences
		headPatterns[p.Hash] = p
	}

	// Find lingering duplicates (reduced but not eliminated)
	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Printf("COMPARISON RESULTS: %s -> %s\n", baseRef, headRef)
	fmt.Printf("%s\n\n", strings.Repeat("=", 60))

	type lingering struct {
		hash      string
		baseCount int
		headCount int
		removed   int
		pattern   JSONPattern
	}
	var lingeringPatterns []lingering

	for hash, baseCount := range baseOccur {
		headCount := headOccur[hash]
		if headCount > 0 && headCount < baseCount {
			lingeringPatterns = append(lingeringPatterns, lingering{
				hash:      hash,
				baseCount: baseCount,
				headCount: headCount,
				removed:   baseCount - headCount,
				pattern:   headPatterns[hash],
			})
		}
	}

	// Sort by removed count descending
	sort.Slice(lingeringPatterns, func(i, j int) bool {
		return lingeringPatterns[i].removed > lingeringPatterns[j].removed
	})

	if len(lingeringPatterns) == 0 {
		fmt.Printf("No lingering duplicates found. All refactoring appears complete!\n")
	} else {
		fmt.Printf("Found %d patterns with incomplete refactoring:\n\n", len(lingeringPatterns))
		for _, l := range lingeringPatterns {
			fmt.Printf("%s %s removed, %s lingering - potentially missed refactoring?\n",
				hashStyle.Render(fmt.Sprintf("[%s]", l.hash)),
				summaryStyle.Render(fmt.Sprintf("%d", l.removed)),
				scoreStyle.Render(fmt.Sprintf("%d", l.headCount)))
			if len(l.pattern.Pattern) > 0 {
				fmt.Printf("  Pattern preview: %s\n", dimStyle.Render(truncate(l.pattern.Pattern[0], 60)))
			}
			fmt.Printf("  Remaining locations:\n")
			for _, loc := range l.pattern.Locations {
				// Make path relative by stripping worktree prefix
				relPath := strings.TrimPrefix(loc.Filename, headScanPath+"/")
				fmt.Printf("    %s\n", locationStyle.Render(fmt.Sprintf("%s:%d", relPath, loc.LineStart)))
			}
			fmt.Println()
		}
	}

	// Also report completely removed patterns
	var fullyRemoved int
	for hash, baseCount := range baseOccur {
		if headOccur[hash] == 0 {
			fullyRemoved++
			_ = baseCount // unused but shows intent
		}
	}
	if fullyRemoved > 0 {
		fmt.Printf("\n%s duplicate patterns were completely removed.\n", summaryStyle.Render(fmt.Sprintf("%d", fullyRemoved)))
	}

	// Report new patterns
	var newPatterns int
	for hash := range headOccur {
		if baseOccur[hash] == 0 {
			newPatterns++
		}
	}
	if newPatterns > 0 {
		fmt.Printf("%s new duplicate patterns were introduced.\n", scoreStyle.Render(fmt.Sprintf("%d", newPatterns)))
	}
}

func loadJSONResults(path string) JSONOutput {
	data, err := os.ReadFile(path)
	if err != nil {
		return JSONOutput{}
	}
	var output JSONOutput
	json.Unmarshal(data, &output)
	return output
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
