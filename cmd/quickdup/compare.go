package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// runCompare compares duplicate patterns between two git commits
func runCompare(baseRef, headRef, subdir, ext, exclude string, minOccur, minScore, minSize, maxSize int, minSimilarity float64, strategyName string) {
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
		"-strategy", strategyName,
		"--no-cache",
	}
	if maxSize > 0 {
		args = append(args, "-max-size", fmt.Sprintf("%d", maxSize))
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
	baseResults := loadJSONResults(filepath.Join(baseScanPath, ".quickdup", strategyName+"-results.json"))
	headResults := loadJSONResults(filepath.Join(headScanPath, ".quickdup", strategyName+"-results.json"))

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
				theme.Hash.Render(fmt.Sprintf("[%s]", l.hash)),
				theme.Summary.Render(fmt.Sprintf("%d", l.removed)),
				theme.Score.Render(fmt.Sprintf("%d", l.headCount)))
			fmt.Printf("  Remaining locations:\n")
			for _, loc := range l.pattern.Locations {
				// Make path relative by stripping worktree prefix
				relPath := strings.TrimPrefix(loc.Filename, headScanPath+"/")
				fmt.Printf("    %s\n", theme.Location.Render(fmt.Sprintf("%s:%d", relPath, loc.LineStart)))
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
		fmt.Printf("\n%s duplicate patterns were completely removed.\n", theme.Summary.Render(fmt.Sprintf("%d", fullyRemoved)))
	}

	// Report new patterns
	var newPatterns int
	for hash := range headOccur {
		if baseOccur[hash] == 0 {
			newPatterns++
		}
	}
	if newPatterns > 0 {
		fmt.Printf("%s new duplicate patterns were introduced.\n", theme.Score.Render(fmt.Sprintf("%d", newPatterns)))
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
