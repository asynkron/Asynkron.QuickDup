package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Theme defines the color scheme for console output
type Theme struct {
	Score    lipgloss.Style
	Hash     lipgloss.Style
	Location lipgloss.Style
	LineNum  lipgloss.Style
	Summary  lipgloss.Style
	Dim      lipgloss.Style
}

// DefaultTheme is the default color scheme
var DefaultTheme = Theme{
	Score:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")),
	Hash:     lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
	Location: lipgloss.NewStyle().Foreground(lipgloss.Color("39")),
	LineNum:  lipgloss.NewStyle().Foreground(lipgloss.Color("221")),
	Summary:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("82")),
	Dim:      lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
}

// Current theme (can be changed at runtime)
var theme = DefaultTheme

// PrintScanStart prints the initial scanning message
func PrintScanStart(fileCount, workerCount int) {
	fmt.Printf("Scanning %d files using %d workers...\n", fileCount, workerCount)
}

// PrintParseComplete prints parsing completion stats
func PrintParseComplete(fileCount, cacheHits, cacheMisses, totalLines int, duration time.Duration) {
	if cacheHits > 0 {
		fmt.Printf("Parsed %d files (%d cached, %d parsed) in %s (%d lines of code)\n",
			fileCount, cacheHits, cacheMisses, duration.Round(time.Millisecond), totalLines)
	} else {
		fmt.Printf("Parsed %d files in %s (%d lines of code)\n",
			fileCount, duration.Round(time.Millisecond), totalLines)
	}
}

// PrintDetectStart prints pattern detection start message
func PrintDetectStart() {
	fmt.Printf("Detecting patterns...\n")
}

// PrintDetectComplete prints pattern detection completion
func PrintDetectComplete(duration time.Duration) {
	fmt.Printf("Pattern detection took %s\n", duration.Round(time.Millisecond))
}

// PrintFilterComplete prints filtering completion and stats
func PrintFilterComplete(duration time.Duration, skippedBlocked, skippedLowScore, skippedLowSimilarity int, minScore int, minSimilarity float64) {
	fmt.Printf("Filtering took %s\n", duration.Round(time.Millisecond))

	if skippedBlocked > 0 {
		fmt.Printf("Filtered %d common patterns\n", skippedBlocked)
	}
	if skippedLowScore > 0 {
		fmt.Printf("Filtered %d low-score patterns (score < %d)\n", skippedLowScore, minScore)
	}
	if skippedLowSimilarity > 0 {
		fmt.Printf("Filtered %d low-similarity patterns (similarity < %.0f%%)\n", skippedLowSimilarity, minSimilarity*100)
	}
}

// PrintIgnoredPatterns prints count of loaded ignored patterns
func PrintIgnoredPatterns(count int) {
	if count > 0 {
		fmt.Printf("Loaded %d ignored patterns from ignore.json\n", count)
	}
}

// PrintGitHubAnnotations outputs GitHub Actions annotations for matches
func PrintGitHubAnnotations(matches []PatternMatch, top int, githubLevel string, gitDiff string, changedFiles map[string]bool) {
	annotationCount := 0
	for _, m := range matches[:top] {
		loc := m.Locations[0]
		// Skip if --git-diff is set and file is not in changed files
		if gitDiff != "" && !changedFiles[loc.Filename] {
			continue
		}
		otherLocs := make([]string, 0, len(m.Locations)-1)
		for _, other := range m.Locations[1:] {
			otherLocs = append(otherLocs, fmt.Sprintf("%s:%d", filepath.Base(other.Filename), other.LineStart))
		}
		endLine := loc.LineStart + len(m.Pattern) - 1
		msg := fmt.Sprintf("Duplicate code also at: %s", strings.Join(otherLocs, ", "))
		fmt.Printf("::%s file=%s,line=%d,endLine=%d,title=Duplicate (%d lines, %.0f%% similar, score %d)::%s\n",
			githubLevel, loc.Filename, loc.LineStart, endLine, len(m.Pattern), m.Similarity*100, m.Score, msg)
		annotationCount++
	}
	if annotationCount > 0 {
		fmt.Printf("\n")
	}
}

// PrintMatchSummary prints the summary of found patterns
func PrintMatchSummary(matchCount, minOccur, top int) {
	fmt.Printf("Found %s patterns with %d+ occurrences (showing top %d by score)\n\n",
		theme.Summary.Render(fmt.Sprintf("%d", matchCount)), minOccur, top)
}

// PrintMatches prints the top matches with their locations
func PrintMatches(matches []PatternMatch, top int) {
	for _, m := range matches[:top] {
		fmt.Printf("\n%s %s %s %s %s:\n",
			theme.Score.Render(fmt.Sprintf("Score %d", m.Score)),
			theme.Dim.Render(fmt.Sprintf("[%d lines]", len(m.Pattern))),
			theme.Dim.Render(fmt.Sprintf("%.0f%% similar", m.Similarity*100)),
			theme.Dim.Render(fmt.Sprintf("found %d times", len(m.Locations))),
			theme.Hash.Render(fmt.Sprintf("[%016x]", m.Hash)))
		for _, loc := range m.Locations {
			fmt.Printf("  %s%s%s\n",
				theme.Location.Render(loc.Filename),
				theme.Dim.Render(":"),
				theme.LineNum.Render(fmt.Sprintf("%d", loc.LineStart)))
		}
	}
}

// PrintHotspots prints the duplication hotspots
func PrintHotspots(matches []PatternMatch) {
	// Count duplicated lines per file
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
		fmt.Printf("\n%s\n", theme.Summary.Render("Duplication hotspots (lines):"))
		showHotspots := 5
		if len(hotspots) < showHotspots {
			showHotspots = len(hotspots)
		}
		for i := 0; i < showHotspots; i++ {
			fmt.Printf("  %s %s\n",
				theme.LineNum.Render(fmt.Sprintf("%4d", hotspots[i].lines)),
				theme.Location.Render(hotspots[i].filename))
		}
	}
}

// PrintTotalSummary prints the final summary line
func PrintTotalSummary(matchCount, fileCount, totalLines int, elapsed time.Duration) {
	fmt.Printf("\nTotal: %s duplicate patterns in %s files (%s lines) in %s\n",
		theme.Summary.Render(fmt.Sprintf("%d", matchCount)),
		theme.Summary.Render(fmt.Sprintf("%d", fileCount)),
		theme.Summary.Render(fmt.Sprintf("%d", totalLines)),
		theme.Summary.Render(elapsed.Round(time.Millisecond).String()))
	fmt.Printf("\n%s\n", theme.Dim.Render("Tip: Even partial matches may contain extractable sub-sections. Look for common logic that could be refactored into shared helpers, base classes, modules or using generics functuins / types where supported."))
}

// langFromExt maps file extensions to markdown code block language hints
var langFromExt = map[string]string{
	".go":    "go",
	".c":     "c",
	".h":     "c",
	".cpp":   "cpp",
	".hpp":   "cpp",
	".cc":    "cpp",
	".cxx":   "cpp",
	".java":  "java",
	".js":    "javascript",
	".jsx":   "jsx",
	".ts":    "typescript",
	".tsx":   "tsx",
	".cs":    "csharp",
	".swift": "swift",
	".kt":    "kotlin",
	".kts":   "kotlin",
	".scala": "scala",
	".rs":    "rust",
	".php":   "php",
	".py":    "python",
	".rb":    "ruby",
	".sh":    "bash",
	".bash":  "bash",
	".zsh":   "zsh",
	".sql":   "sql",
	".lua":   "lua",
	".hs":    "haskell",
	".elm":   "elm",
	".yaml":  "yaml",
	".yml":   "yaml",
	".toml":  "toml",
	".json":  "json",
	".xml":   "xml",
	".html":  "html",
	".css":   "css",
	".scss":  "scss",
	".dart":  "dart",
	".r":     "r",
	".R":     "r",
	".jl":    "julia",
	".ex":    "elixir",
	".exs":   "elixir",
	".clj":   "clojure",
	".cljs":  "clojure",
	".v":     "v",
	".zig":   "zig",
	".nim":   "nim",
}

// normalizeIndent removes common leading whitespace from lines
func normalizeIndent(entries []Entry) []string {
	if len(entries) == 0 {
		return nil
	}

	// Find minimum indent across all non-empty lines
	minIndent := -1
	for _, e := range entries {
		line := e.GetRaw()
		indent := 0
		for _, r := range line {
			if r == ' ' {
				indent++
			} else if r == '\t' {
				indent += 4
			} else {
				break
			}
		}
		if minIndent < 0 || indent < minIndent {
			minIndent = indent
		}
	}

	// Strip minimum indent from each line
	result := make([]string, len(entries))
	for i, e := range entries {
		line := e.GetRaw()
		stripped := 0
		start := 0
		for j, r := range line {
			if stripped >= minIndent {
				start = j
				break
			}
			if r == ' ' {
				stripped++
			} else if r == '\t' {
				stripped += 4
			} else {
				start = j
				break
			}
			start = j + 1
		}
		result[i] = line[start:]
	}

	return result
}

// PrintDetailedMatches prints detailed pattern matches with source code using glow
func PrintDetailedMatches(matches []PatternMatch, ext string) {
	lang := langFromExt[ext]
	if lang == "" {
		lang = strings.TrimPrefix(ext, ".")
	}

	// Group matches by hash to detect multiple clusters
	hashCounts := make(map[uint64]int)
	for _, m := range matches {
		hashCounts[m.Hash]++
	}

	// Track cluster number per hash
	hashClusterNum := make(map[uint64]int)

	var sb strings.Builder
	for i, m := range matches {
		// Determine if this hash has multiple clusters
		clusterInfo := ""
		if hashCounts[m.Hash] > 1 {
			hashClusterNum[m.Hash]++
			clusterInfo = fmt.Sprintf(" (Cluster %d/%d)", hashClusterNum[m.Hash], hashCounts[m.Hash])
		}

		sb.WriteString(fmt.Sprintf("## Pattern %d%s\n\n", i+1, clusterInfo))
		sb.WriteString(fmt.Sprintf("**Hash:** `%016x`  **Score:** %d  **Similarity:** %.0f%%  **Lines:** %d  **Occurrences:** %d\n\n",
			m.Hash, m.Score, m.Similarity*100, len(m.Pattern), len(m.Locations)))

		for j, loc := range m.Locations {
			sb.WriteString(fmt.Sprintf("### Occurrence %d: `%s:%d`\n\n",
				j+1, loc.Filename, loc.LineStart))

			sb.WriteString(fmt.Sprintf("```%s\n", lang))
			normalizedLines := normalizeIndent(loc.Pattern)
			for _, line := range normalizedLines {
				sb.WriteString(line + "\n")
			}
			sb.WriteString("```\n\n")
		}
		sb.WriteString("---\n\n")
	}

	renderWithGlow(sb.String())
}

// renderWithGlow pipes markdown content through glow for rendering
func renderWithGlow(markdown string) {
	cmd := exec.Command("glow", "-w", "0", "-")
	cmd.Stdin = strings.NewReader(markdown)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		// Fallback to plain output if glow is not available
		fmt.Print(markdown)
	}
}

// ReadJSONResults reads results from a JSON file
func ReadJSONResults(path string) ([]JSONPattern, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var output JSONOutput
	if err := json.Unmarshal(data, &output); err != nil {
		return nil, err
	}

	return output.Patterns, nil
}

// PrintDetailedMatchesFromJSON prints detailed pattern matches from JSON results
func PrintDetailedMatchesFromJSON(patterns []JSONPattern, ext string) {
	lang := langFromExt[ext]
	if lang == "" {
		lang = strings.TrimPrefix(ext, ".")
	}

	// Group patterns by hash to detect multiple clusters
	hashCounts := make(map[string]int)
	for _, p := range patterns {
		hashCounts[p.Hash]++
	}

	// Track cluster number per hash
	hashClusterNum := make(map[string]int)

	var sb strings.Builder
	for i, p := range patterns {
		// Determine if this hash has multiple clusters
		clusterInfo := ""
		if hashCounts[p.Hash] > 1 {
			hashClusterNum[p.Hash]++
			clusterInfo = fmt.Sprintf(" (Cluster %d/%d)", hashClusterNum[p.Hash], hashCounts[p.Hash])
		}

		sb.WriteString(fmt.Sprintf("## Pattern %d%s\n\n", i+1, clusterInfo))
		sb.WriteString(fmt.Sprintf("**Hash:** `%s`  **Score:** %d  **Similarity:** %.0f%%  **Lines:** %d  **Occurrences:** %d\n\n",
			p.Hash, p.Score, p.Similarity*100, p.Lines, p.Occurrences))

		for j, loc := range p.Locations {
			sb.WriteString(fmt.Sprintf("### Occurrence %d: `%s:%d`\n\n",
				j+1, loc.Filename, loc.LineStart))

			// Read source lines from file
			lines := readSourceLines(loc.Filename, loc.LineStart, p.Lines)
			sb.WriteString(fmt.Sprintf("```%s\n", lang))
			for _, line := range lines {
				sb.WriteString(line + "\n")
			}
			sb.WriteString("```\n\n")
		}
		sb.WriteString("---\n\n")
	}

	renderWithGlow(sb.String())
}

// readSourceLines reads specific lines from a file and normalizes indent
func readSourceLines(filename string, startLine, count int) []string {
	data, err := os.ReadFile(filename)
	if err != nil {
		return []string{fmt.Sprintf("// Error reading file: %v", err)}
	}

	allLines := strings.Split(string(data), "\n")
	var result []string

	// Find minimum indent for normalization
	minIndent := -1
	for i := startLine - 1; i < startLine-1+count && i < len(allLines); i++ {
		if i < 0 {
			continue
		}
		line := allLines[i]
		indent := 0
		for _, r := range line {
			if r == ' ' {
				indent++
			} else if r == '\t' {
				indent += 4
			} else {
				break
			}
		}
		if len(strings.TrimSpace(line)) > 0 && (minIndent < 0 || indent < minIndent) {
			minIndent = indent
		}
	}
	if minIndent < 0 {
		minIndent = 0
	}

	// Extract and normalize lines
	for i := startLine - 1; i < startLine-1+count && i < len(allLines); i++ {
		if i < 0 {
			continue
		}
		line := allLines[i]
		// Strip minimum indent
		stripped := 0
		start := 0
		for j, r := range line {
			if stripped >= minIndent {
				start = j
				break
			}
			if r == ' ' {
				stripped++
			} else if r == '\t' {
				stripped += 4
			} else {
				start = j
				break
			}
			start = j + 1
		}
		result = append(result, line[start:])
	}

	return result
}

// WriteJSONResults writes the results to a JSON file
func WriteJSONResults(matches []PatternMatch, outputPath string) error {
	jsonOutput := JSONOutput{
		TotalPatterns: len(matches),
		Patterns:      make([]JSONPattern, 0, len(matches)),
	}

	for _, m := range matches {
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

	// Create output directory
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	jsonData, err := json.MarshalIndent(jsonOutput, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}

	if err := os.WriteFile(outputPath, jsonData, 0o644); err != nil {
		return fmt.Errorf("writing JSON file: %w", err)
	}

	fmt.Printf("Results written to: %s\n", theme.Location.Render(outputPath))
	return nil
}
