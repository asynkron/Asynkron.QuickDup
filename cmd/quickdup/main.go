package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Active strategy (set from --strategy flag)
var activeStrategy Strategy

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

var commentPrefix string

func main() {
	path := flag.String("path", ".", "Path to scan")
	ext := flag.String("ext", ".go", "File extension to scan")
	minOccur := flag.Int("min", 2, "Minimum occurrences to report")
	minScore := flag.Int("min-score", 5, "Minimum score to report (uniqueWords × adjusted similarity)")
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
	strategyName := flag.String("strategy", "normalized-indent", "Detection strategy: word-indent, normalized-indent")
	selectRange := flag.String("select", "", "Select patterns from results JSON (format: skip..limit, e.g., 0..5)")
	scan := flag.Bool("scan", false, "Force re-scan even if results JSON exists")
	flag.Parse()

	// If --select is provided without --scan, try to read from existing JSON
	jsonPath := filepath.Join(*path, ".quickdup", *strategyName+"-results.json")
	if *selectRange != "" && !*scan {
		if patterns, err := ReadJSONResults(jsonPath); err == nil {
			skip, limit, err := parseSelectRange(*selectRange)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			selected := selectJSONPatterns(patterns, skip, limit)
			PrintDetailedMatchesFromJSON(selected, *ext)
			fmt.Printf("\nShowing %d of %d patterns from %s\n", len(selected), len(patterns), jsonPath)
			return
		}
		// JSON doesn't exist, fall through to scan
	}

	// Select strategy
	strategies := map[string]Strategy{
		"word-indent":       &WordIndentStrategy{},
		"normalized-indent": &NormalizedIndentStrategy{},
		"word-only":         &WordOnlyStrategy{},
	}
	if s, ok := strategies[*strategyName]; ok {
		activeStrategy = s
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
		runCompare(baseRef, headRef, subdir, *ext, *exclude, *minOccur, *minScore, *minSize, *minSimilarity, *strategyName)
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
	userIgnored := LoadIgnoredHashes(folder, *strategyName)
	PrintIgnoredPatterns(len(userIgnored))

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
				// Check if pattern matches basename (glob) or is contained in path (substring)
				if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
					excluded = true
					break
				}
				// Also check if pattern is a substring of the path (for directory patterns like ".Tests/")
				if strings.Contains(path, pattern) {
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
	PrintScanStart(totalFiles, runtime.NumCPU())

	parseStart := time.Now()
	var cache *FileCache
	if !*noCache {
		cache = loadCache(folder, *strategyName)
	}

	fileData, cacheHits, cacheMisses := parseFilesWithCache(files, cache)

	// Save updated cache
	if !*noCache && cacheMisses > 0 {
		saveCache(folder, *strategyName, files, fileData)
	}
	parseTime := time.Since(parseStart)

	// Count total lines of code (non-blank, non-comment)
	totalLines := 0
	for _, entries := range fileData {
		totalLines += len(entries)
	}

	PrintParseComplete(len(fileData), cacheHits, cacheMisses, totalLines, parseTime)

	// Phase 2: Pattern detection with growth
	detectStart := time.Now()
	PrintDetectStart()
	patterns := detectPatterns(fileData, len(fileData), *minOccur, *minSize)
	detectTime := time.Since(detectStart)
	PrintDetectComplete(detectTime)

	// Filter and score matches
	filterStart := time.Now()
	matches, filterStats := FilterPatterns(patterns, FilterConfig{
		MinOccur:      *minOccur,
		MinScore:      *minScore,
		MinSimilarity: *minSimilarity,
		UserIgnored:   userIgnored,
	})
	filterTime := time.Since(filterStart)

	// Report results
	PrintFilterComplete(filterTime, filterStats.SkippedBlocked, filterStats.SkippedLowScore, filterStats.SkippedLowSimilarity, *minScore, *minSimilarity)

	top := TopN(matches, *topN)

	if *githubAnnotations {
		PrintGitHubAnnotations(top, len(top), *githubLevel, *gitDiff, changedFiles)
	}

	PrintMatchSummary(len(matches), *minOccur, len(top))
	PrintMatches(top, len(top))
	PrintHotspots(matches)

	elapsed := time.Since(startTime)
	PrintTotalSummary(len(matches), len(fileData), totalLines, elapsed)

	if *githubAnnotations {
		return
	}

	outputPath := filepath.Join(*path, ".quickdup", *strategyName+"-results.json")
	if err := WriteJSONResults(matches, outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// If --select was provided with --scan, show detailed output from the just-written JSON
	if *selectRange != "" {
		patterns, err := ReadJSONResults(outputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading results: %v\n", err)
			os.Exit(1)
		}
		skip, limit, err := parseSelectRange(*selectRange)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		selected := selectJSONPatterns(patterns, skip, limit)
		PrintDetailedMatchesFromJSON(selected, *ext)
		fmt.Printf("\nShowing %d of %d patterns\n", len(selected), len(patterns))
	}
}

// parseSelectRange parses a "skip..limit" string into skip and limit integers
func parseSelectRange(s string) (skip, limit int, err error) {
	parts := strings.Split(s, "..")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("--select requires format 'skip..limit' (e.g., 0..3)")
	}
	if _, err := fmt.Sscanf(parts[0], "%d", &skip); err != nil {
		return 0, 0, fmt.Errorf("invalid skip value: %s", parts[0])
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &limit); err != nil {
		return 0, 0, fmt.Errorf("invalid limit value: %s", parts[1])
	}
	if skip < 0 || limit < 0 {
		return 0, 0, fmt.Errorf("skip and limit must be non-negative")
	}
	return skip, limit, nil
}

// selectMatches returns a slice of matches starting at skip with at most limit items
func selectMatches(matches []PatternMatch, skip, limit int) []PatternMatch {
	if skip >= len(matches) {
		return nil
	}
	end := skip + limit
	if end > len(matches) {
		end = len(matches)
	}
	return matches[skip:end]
}

// selectJSONPatterns returns a slice of JSON patterns starting at skip with at most limit items
func selectJSONPatterns(patterns []JSONPattern, skip, limit int) []JSONPattern {
	if skip >= len(patterns) {
		return nil
	}
	end := skip + limit
	if end > len(patterns) {
		end = len(patterns)
	}
	return patterns[skip:end]
}
