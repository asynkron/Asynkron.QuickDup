package main

import "flag"

const minScoreHelpText = "Minimum score to report (strategy-specific score)"

type cliFlags struct {
	path              *string
	filePath          *string
	ext               *string
	minOccur          *int
	minScore          *int
	minSize           *int
	maxSize           *int
	minSimilarity     *float64
	topN              *int
	comment           *string
	noCache           *bool
	githubAnnotations *bool
	githubLevel       *string
	gitDiff           *string
	exclude           *string
	compare           *string
	strategyName      *string
	selectRange       *string
	keepOverlaps      *bool
	debug             *bool
	timeoutSeconds    *int
}

func registerFlags(fs *flag.FlagSet) cliFlags {
	return cliFlags{
		path:              fs.String("path", ".", "Path to scan"),
		filePath:          fs.String("file", "", "Scan a single file (overrides --path)"),
		ext:               fs.String("ext", ".go", "File extension to scan"),
		minOccur:          fs.Int("min", 2, "Minimum occurrences to report"),
		minScore:          fs.Int("min-score", 5, minScoreHelpText),
		minSize:           fs.Int("min-size", 3, "Base pattern size to start growing from"),
		maxSize:           fs.Int("max-size", 0, "Maximum pattern size to grow to (0 = no limit)"),
		minSimilarity:     fs.Float64("min-similarity", 0.75, "Minimum token similarity between occurrences (0.0-1.0)"),
		topN:              fs.Int("top", 10, "Show top N matches by pattern length"),
		comment:           fs.String("comment", "", "Override comment prefix (auto-detected by extension)"),
		noCache:           fs.Bool("no-cache", false, "Disable incremental caching, force full re-parse"),
		githubAnnotations: fs.Bool("github-annotations", false, "Output GitHub Actions annotations for inline PR comments"),
		githubLevel:       fs.String("github-level", "warning", "GitHub annotation level: notice, warning, or error"),
		gitDiff:           fs.String("git-diff", "", "Only annotate files changed vs this git ref (e.g., origin/main)"),
		exclude:           fs.String("exclude", "", "Exclude files matching patterns (comma-separated, e.g., '*.pb.go,*_gen.go')"),
		compare:           fs.String("compare", "", "Compare duplicates between two commits (format: base..head)"),
		strategyName:      fs.String("strategy", "normalized-indent", "Detection strategy: word-indent, normalized-indent, word-only, inlineable"),
		selectRange:       fs.String("select", "", "Show detailed output for patterns (format: skip..limit, e.g., 0..5)"),
		keepOverlaps:      fs.Bool("keep-overlaps", false, "Keep overlapping occurrences (don't prune adjacent matches)"),
		debug:             fs.Bool("debug", false, "Print verbose progress for long-running phases"),
		timeoutSeconds:    fs.Int("timeout", 20, "Hard timeout in seconds (0 disables)"),
	}
}
