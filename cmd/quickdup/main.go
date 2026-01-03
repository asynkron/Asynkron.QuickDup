package main

import (
	"bufio"
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
}

type PatternLocation struct {
	Filename  string
	LineStart int
	Pattern   []IndentAndWord // the actual pattern at this location
}

type PatternMatch struct {
	Hash        uint64
	Locations   []PatternLocation
	Pattern     []IndentAndWord // representative pattern (first occurrence)
	UniqueWords int             // score: number of unique words in pattern
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

	// Phase 2: Pattern detection
	fmt.Printf("Detecting patterns...\n")
	patterns := detectPatterns(fileData, len(fileData))

	// Filter and collect matches
	var matches []PatternMatch
	skippedBlocked := 0
	for hash, locs := range patterns {
		if blockedHashes[hash] {
			skippedBlocked++
			continue
		}
		if len(locs) >= *minOccur {
			pattern := locs[0].Pattern
			matches = append(matches, PatternMatch{
				Hash:        hash,
				Locations:   locs,
				Pattern:     pattern,
				UniqueWords: countUniqueWords(pattern),
			})
		}
	}
	if skippedBlocked > 0 {
		fmt.Printf("Filtered %d common patterns\n", skippedBlocked)
	}

	// Sort by: unique words (desc), then pattern length (desc), then occurrences (desc)
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].UniqueWords != matches[j].UniqueWords {
			return matches[i].UniqueWords > matches[j].UniqueWords
		}
		if len(matches[i].Pattern) != len(matches[j].Pattern) {
			return len(matches[i].Pattern) > len(matches[j].Pattern)
		}
		return len(matches[i].Locations) > len(matches[j].Locations)
	})

	// Limit to top N
	top := *topN
	if len(matches) < top {
		top = len(matches)
	}

	fmt.Printf("Found %d patterns with %d+ occurrences (showing top %d by length)\n\n", len(matches), *minOccur, top)

	for _, m := range matches[:top] {
		fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Printf("Pattern [%d lines, %d unique words] found %d times:\n", len(m.Pattern), m.UniqueWords, len(m.Locations))
		fmt.Printf("┌─────────────────────────────────────\n")
		for _, entry := range m.Pattern {
			indent := ""
			if entry.IndentDelta > 0 {
				indent = fmt.Sprintf("+%d", entry.IndentDelta)
			} else {
				indent = fmt.Sprintf("%d", entry.IndentDelta)
			}
			fmt.Printf("│ %3s  %s\n", indent, entry.Word)
		}
		fmt.Printf("└─────────────────────────────────────\n")
		fmt.Printf("Locations:\n")
		maxLocs := 5
		for i, loc := range m.Locations {
			if i >= maxLocs {
				fmt.Printf("  ... and %d more\n", len(m.Locations)-maxLocs)
				break
			}
			fmt.Printf("  • %s:%d\n", loc.Filename, loc.LineStart)
		}
		fmt.Println()
	}

	fmt.Printf("Total: %d duplicate patterns\n", len(matches))
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

// Symbols that don't count as "real" starting words
func isSymbolOnly(word string) bool {
	if len(word) == 0 {
		return true
	}
	// Single character symbols
	if len(word) == 1 {
		return strings.ContainsAny(word, "{}[]()<>,.;:!?@#$%^&*+-=/\\|`~\"'")
	}
	return false
}

func detectPatterns(fileData map[string][]IndentAndWord, totalFiles int) map[uint64][]PatternLocation {
	patterns := make(map[uint64][]PatternLocation)

	processed := 0
	for filename, entries := range fileData {
		n := len(entries)

		for i := 0; i < n; i++ {
			// Skip if first word is just a symbol
			if isSymbolOnly(entries[i].Word) {
				continue
			}

			// Window sizes from 2 to 10
			maxJ := i + 10
			if maxJ > n {
				maxJ = n
			}

			for j := i + 2; j <= maxJ; j++ {
				// Skip if last word is just a symbol
				if isSymbolOnly(entries[j-1].Word) {
					continue
				}

				window := entries[i:j]
				hash := hashPattern(window)

				// Copy window to avoid slice aliasing issues
				patternCopy := make([]IndentAndWord, len(window))
				copy(patternCopy, window)

				patterns[hash] = append(patterns[hash], PatternLocation{
					Filename:  filename,
					LineStart: entries[i].LineNumber,
					Pattern:   patternCopy,
				})
			}
		}
		processed++
		printProgress("Analyzing", processed, totalFiles)
	}
	clearProgress()

	return patterns
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
