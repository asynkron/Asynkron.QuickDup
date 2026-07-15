package quickdup

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

var analyzeMu sync.Mutex

// commentPrefixesByExt gives a sane default line-comment prefix per file
// extension for AnalyzeFiles. Callers driving the lower-level API
// (ParseFilesWithCache, DetectPatterns, FilterPatterns) set CommentPrefix
// themselves instead.
var commentPrefixesByExt = map[string]string{
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
	".py":    "#",
	".rb":    "#",
	".sh":    "#",
	".bash":  "#",
	".zsh":   "#",
	".yaml":  "#",
	".yml":   "#",
	".toml":  "#",
	".sql":   "--",
}

// AnalyzeOptions configures a one-shot AnalyzeFiles call.
type AnalyzeOptions struct {
	Files         []string
	Strategy      string // "word-indent" | "word-only" | "inlineable" | "" (default: normalized-indent)
	MinOccur      int
	MinRank       int
	MinSize       int
	MaxSize       int
	MinSimilarity float64
	KeepOverlaps  bool
}

// AnalyzeResult is the outcome of an AnalyzeFiles call.
type AnalyzeResult struct {
	FilesScanned int
	LinesScanned int
	Matches      []PatternMatch
}

// AnalyzeFiles is a single-call convenience API over the lower-level
// ActiveStrategy/CommentPrefix/ParseFilesWithCache/DetectPatterns/
// FilterPatterns pipeline: parse the given files, detect repeated patterns,
// and filter/score them, with per-extension comment prefixes and no
// incremental caching. It serializes concurrent calls since it mutates the
// package-level ActiveStrategy/CommentPrefix/Debug state.
func AnalyzeFiles(opts AnalyzeOptions) (AnalyzeResult, error) {
	analyzeMu.Lock()
	defer analyzeMu.Unlock()

	strategy, strategyName := selectStrategy(opts.Strategy)
	ActiveStrategy = strategy
	Debug = false
	minOccur := opts.MinOccur
	if minOccur <= 0 {
		minOccur = 2
	}
	minSize := opts.MinSize
	if minSize <= 0 {
		minSize = 3
	}
	minSimilarity := opts.MinSimilarity
	if minSimilarity <= 0 {
		minSimilarity = 0.75
	}
	minRank := opts.MinRank
	if minRank < 0 {
		minRank = 0
	}

	files := append([]string(nil), opts.Files...)
	sort.Strings(files)
	fileData := make(map[string][]Entry, len(files))
	linesScanned := 0
	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file))
		CommentPrefix = commentPrefixesByExt[ext]
		if CommentPrefix == "" {
			CommentPrefix = "//"
		}
		entries, err := parseFile(file)
		if err != nil {
			return AnalyzeResult{}, err
		}
		if len(entries) == 0 {
			continue
		}
		fileData[file] = entries
		linesScanned += len(entries)
	}
	if len(fileData) == 0 {
		return AnalyzeResult{FilesScanned: len(files)}, nil
	}

	patterns := DetectPatterns(fileData, len(fileData), minOccur, minSize, opts.MaxSize, opts.KeepOverlaps)
	matches, _ := FilterPatterns(patterns, FilterConfig{
		MinOccur:      minOccur,
		MinRank:       minRank,
		MinSimilarity: minSimilarity,
		UserIgnored:   map[uint64]bool{},
	})
	_ = strategyName
	return AnalyzeResult{FilesScanned: len(fileData), LinesScanned: linesScanned, Matches: matches}, nil
}

func selectStrategy(name string) (Strategy, string) {
	switch strings.TrimSpace(name) {
	case "word-indent":
		return &WordIndentStrategy{}, "word-indent"
	case "word-only":
		return &WordOnlyStrategy{}, "word-only"
	case "inlineable":
		return &InlineableStrategy{}, "inlineable"
	default:
		return &NormalizedIndentStrategy{}, "normalized-indent"
	}
}
