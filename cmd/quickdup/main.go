package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
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
	uselessPatterns := [][]Entry{
		// } }
		{NewWordIndentEntry(-4, "}"), NewWordIndentEntry(-4, "}")},
		// } } }
		{NewWordIndentEntry(-4, "}"), NewWordIndentEntry(-4, "}"), NewWordIndentEntry(-4, "}")},
		// return }
		{NewWordIndentEntry(0, "return"), NewWordIndentEntry(-4, "}")},
		// +4 return }
		{NewWordIndentEntry(4, "return"), NewWordIndentEntry(-4, "}")},
		// } return }
		{NewWordIndentEntry(-4, "}"), NewWordIndentEntry(0, "return"), NewWordIndentEntry(-4, "}")},
		// } func
		{NewWordIndentEntry(-4, "}"), NewWordIndentEntry(0, "func")},
		// } return
		{NewWordIndentEntry(-4, "}"), NewWordIndentEntry(0, "return")},
	}

	for _, pattern := range uselessPatterns {
		blockedHashes[defaultStrategy.Hash(pattern)] = true
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
	strategyName := flag.String("strategy", "word-indent", "Detection strategy: word-indent, normalized-indent")
	flag.Parse()

	// Select strategy
	strategies := map[string]Strategy{
		"word-indent":       &WordIndentStrategy{},
		"normalized-indent": &NormalizedIndentStrategy{},
	}
	if s, ok := strategies[*strategyName]; ok {
		defaultStrategy = s
	} else {
		fmt.Fprintf(os.Stderr, "Unknown strategy: %s\n", *strategyName)
		os.Exit(1)
	}

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

	// First pass: filter blocked patterns
	type candidate struct {
		hash    uint64
		locs    []PatternLocation
		pattern []Entry
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
			candidates = append(candidates, candidate{hash, locs, pattern})
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
		score := defaultStrategy.Score(c.pattern, r.similarity)
		if score < *minScore {
			skippedLowScore++
			continue
		}
		matches = append(matches, PatternMatch{
			Hash:       c.hash,
			Locations:  c.locs,
			Pattern:    c.pattern,
			Similarity: r.similarity,
			Score:      score,
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
			dimStyle.Render(fmt.Sprintf("[%d lines]", len(m.Pattern))),
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
			Similarity:  m.Similarity,
			Occurrences: len(m.Locations),
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
