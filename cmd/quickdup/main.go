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
var debugEnabled bool

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
	".py":    "#",
	".rb":    "#",
	".sh":    "#",
	".bash":  "#",
	".zsh":   "#",
	".pl":    "#",
	".pm":    "#",
	".r":     "#",
	".R":     "#",
	".yaml":  "#",
	".yml":   "#",
	".toml":  "#",
	".tf":    "#",
	".cmake": "#",
	".make":  "#",
	".mk":    "#",
	".ps1":   "#",
	".nim":   "#",
	".jl":    "#",
	".ex":    "#",
	".exs":   "#",
	".cr":    "#",
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
	flags := registerFlags(flag.CommandLine)
	flag.Parse()
	debugEnabled = *flags.debug
	if *flags.timeoutSeconds > 0 {
		timeout := time.Duration(*flags.timeoutSeconds) * time.Second
		go func() {
			time.Sleep(timeout)
			fmt.Fprintf(os.Stderr, "Error: timed out after %s\n", timeout)
			os.Exit(1)
		}()
	}
	if *flags.maxSize > 0 && *flags.maxSize < *flags.minSize {
		fmt.Fprintf(os.Stderr, "Error: --max-size must be >= --min-size\n")
		os.Exit(1)
	}

	// Select strategy
	strategies := map[string]Strategy{
		"word-indent":       &WordIndentStrategy{},
		"normalized-indent": &NormalizedIndentStrategy{},
		"word-only":         &WordOnlyStrategy{},
		"inlineable":        &InlineableStrategy{},
	}
	if s, ok := strategies[*flags.strategyName]; ok {
		activeStrategy = s
	} else {
		fmt.Fprintf(os.Stderr, "Unknown strategy: %s\n", *flags.strategyName)
		os.Exit(1)
	}

	// Handle compare mode
	if *flags.compare != "" {
		parts := strings.Split(*flags.compare, "..")
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "Error: --compare requires format 'base..head'\n")
			os.Exit(1)
		}
		baseRef, headRef := parts[0], parts[1]
		// Extract subdir from path if it's not "."
		subdir := ""
		if *flags.path != "." {
			subdir = *flags.path
		}
		runCompare(baseRef, headRef, subdir, *flags.ext, *flags.exclude, *flags.minOccur, *flags.minScore, *flags.minSize, *flags.maxSize, *flags.minSimilarity, *flags.strategyName)
		return
	}

	// Parse exclude patterns
	var excludePatterns []string
	if *flags.exclude != "" {
		for _, p := range strings.Split(*flags.exclude, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				excludePatterns = append(excludePatterns, p)
			}
		}
	}

	// Build set of changed files if --git-diff is specified
	changedFiles := make(map[string]bool)
	if *flags.gitDiff != "" {
		cmd := exec.Command("git", "diff", "--name-only", *flags.gitDiff)
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

	folder := *flags.path
	extension := *flags.ext
	singleFile := ""
	if *flags.filePath != "" {
		singleFile = *flags.filePath
	} else if info, err := os.Stat(*flags.path); err == nil && !info.IsDir() {
		singleFile = *flags.path
	}
	if singleFile != "" {
		info, err := os.Stat(singleFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: file not found: %s\n", singleFile)
			os.Exit(1)
		}
		if info.IsDir() {
			fmt.Fprintf(os.Stderr, "Error: --file expects a file path: %s\n", singleFile)
			os.Exit(1)
		}
		folder = filepath.Dir(singleFile)
		extension = filepath.Ext(singleFile)
		if extension == "" {
			extension = *flags.ext
		}
	}
	extension = strings.ToLower(extension)

	// Auto-detect comment prefix from extension, allow override
	if *flags.comment != "" {
		commentPrefix = *flags.comment
	} else if prefix, ok := commentPrefixes[extension]; ok {
		commentPrefix = prefix
	} else {
		commentPrefix = "//" // fallback default
	}

	// Load user-ignored hashes from ignore.json
	userIgnored := LoadIgnoredHashes(folder, *flags.strategyName)
	PrintIgnoredPatterns(len(userIgnored))

	// First pass: count files
	var files []string
	var err error
	if singleFile != "" {
		files = []string{singleFile}
	} else {
		err = filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
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
	if !*flags.noCache {
		cache = loadCache(folder, *flags.strategyName)
	}

	fileData, cacheHits, cacheMisses := parseFilesWithCache(files, cache)

	// Save updated cache
	if !*flags.noCache && cacheMisses > 0 {
		saveCache(folder, *flags.strategyName, files, fileData)
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
	patterns := detectPatterns(fileData, len(fileData), *flags.minOccur, *flags.minSize, *flags.maxSize, *flags.keepOverlaps)
	detectTime := time.Since(detectStart)
	PrintDetectComplete(detectTime)

	// Filter and score matches
	filterStart := time.Now()
	matches, filterStats := FilterPatterns(patterns, FilterConfig{
		MinOccur:      *flags.minOccur,
		MinScore:      *flags.minScore,
		MinSimilarity: *flags.minSimilarity,
		UserIgnored:   userIgnored,
	})
	filterTime := time.Since(filterStart)

	// Report results
	PrintFilterComplete(filterTime, filterStats.SkippedBlocked, filterStats.SkippedLowScore, filterStats.SkippedLowSimilarity, *flags.minScore, *flags.minSimilarity)

	top := TopN(matches, *flags.topN)

	if *flags.githubAnnotations {
		PrintGitHubAnnotations(top, len(top), *flags.githubLevel, *flags.gitDiff, changedFiles)
	}

	PrintHotspots(matches)

	if *flags.githubAnnotations {
		elapsed := time.Since(startTime)
		PrintTotalSummary(len(matches), len(fileData), totalLines, elapsed)
		return
	}

	outputPath := filepath.Join(folder, ".quickdup", *flags.strategyName+"-results.json")
	if err := WriteJSONResults(matches, outputPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// If --select was provided, show detailed output from the JSON
	if *flags.selectRange != "" {
		patterns, err := ReadJSONResults(outputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading results: %v\n", err)
			os.Exit(1)
		}
		skip, limit, err := parseSelectRange(*flags.selectRange)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		selected := selectJSONPatterns(patterns, skip, limit)
		PrintDetailedMatchesFromJSON(selected, extension)
		PrintShowingPatterns(skip, limit)
	}

	elapsed := time.Since(startTime)
	PrintTotalSummary(len(matches), len(fileData), totalLines, elapsed)
	PrintResultsPath(outputPath)
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
