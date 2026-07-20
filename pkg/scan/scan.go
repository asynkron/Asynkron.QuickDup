package quickdup

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/asynkron/Asynkron.QuickDup/internal/sourcepath"
	asynkronquickdup "github.com/asynkron/Asynkron.QuickDup/pkg/quickdup"
	"github.com/bmatcuk/doublestar/v4"
)

const (
	SchemaVersion   = "quickdup.v1"
	defaultTopN     = 20
	maxTopN         = 200
	defaultMinLines = 3
	defaultMaxFiles = 5000

	duplicateFamilyOverlapPercent = 80
)

type Options struct {
	RepoRoot            string
	SourceDirs          []string
	SourceFiles         []FileSummary
	Languages           []string
	Extensions          []string
	Excludes            []string
	InputSource         string
	CodegraphLanguages  []string
	CodegraphExtensions []string
	FallbackLanguages   []string
	FallbackExtensions  []string
	TopN                int
	Limit               int
	MinLines            int
	MaxFiles            int
	Now                 func() time.Time
}

type Query struct {
	SourceDirs []string `json:"source_dirs"`
	Languages  []string `json:"languages,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
	Excludes   []string `json:"excludes,omitempty"`
	TopN       int      `json:"top_n"`
	MinLines   int      `json:"min_lines"`
	MaxFiles   int      `json:"max_files"`
}

type Response struct {
	SchemaVersion      string      `json:"schema_version"`
	GeneratedAt        time.Time   `json:"generated_at"`
	RepoRoot           string      `json:"repo_root,omitempty"`
	Query              Query       `json:"query"`
	ScanInputs         ScanInputs  `json:"scan_inputs"`
	Summary            Summary     `json:"summary"`
	TotalCandidates    int         `json:"total_candidates"`
	Candidates         []Candidate `json:"candidates"`
	Tooling            Tooling     `json:"tooling"`
	UnavailableReasons []string    `json:"unavailable_reasons,omitempty"`
}

type ScanInputs struct {
	SourceDirs          []string `json:"source_dirs"`
	SourceFiles         int      `json:"source_files,omitempty"`
	Languages           []string `json:"languages,omitempty"`
	Extensions          []string `json:"extensions,omitempty"`
	Excludes            []string `json:"excludes,omitempty"`
	InputSource         string   `json:"input_source,omitempty"`
	CodegraphLanguages  []string `json:"codegraph_languages,omitempty"`
	CodegraphExtensions []string `json:"codegraph_extensions,omitempty"`
	FallbackLanguages   []string `json:"fallback_languages,omitempty"`
	FallbackExtensions  []string `json:"fallback_extensions,omitempty"`
	Limit               int      `json:"limit"`
	MaxFiles            int      `json:"max_files"`
	MinLines            int      `json:"min_lines"`
}

type Summary struct {
	FilesScanned                    int `json:"files_scanned"`
	LinesScanned                    int `json:"lines_scanned"`
	DuplicateBlocks                 int `json:"duplicate_blocks"`
	DuplicateInstances              int `json:"duplicate_instances"`
	OverlappingCandidatesSuppressed int `json:"overlapping_candidates_suppressed,omitempty"`
	CandidatesReturned              int `json:"candidates_returned"`
}

type Tooling struct {
	Engine       string `json:"engine"`
	Mode         string `json:"mode"`
	Source       string `json:"source,omitempty"`
	SourceCommit string `json:"source_commit,omitempty"`
	Strategy     string `json:"strategy,omitempty"`
	ArtifactRoot string `json:"artifact_root,omitempty"`
}

type Candidate struct {
	Rank        int          `json:"rank"`
	Score       int          `json:"score"`
	Fingerprint string       `json:"fingerprint"`
	LineCount   int          `json:"line_count"`
	Occurrences []Occurrence `json:"occurrences"`
	Sample      []string     `json:"sample,omitempty"`
	Preview     string       `json:"preview,omitempty"`
}

type Occurrence struct {
	Path      string `json:"path"`
	Language  string `json:"language"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

type languageSpec struct {
	Name       string
	Aliases    []string
	Extensions []string
}

var languageRegistry = []languageSpec{
	{Name: "go", Extensions: []string{".go"}},
	{Name: "typescript", Aliases: []string{"ts"}, Extensions: []string{".ts", ".tsx", ".mts", ".cts"}},
	{Name: "tsx", Extensions: []string{".tsx"}},
	{Name: "javascript", Aliases: []string{"js"}, Extensions: []string{".js", ".jsx", ".mjs", ".cjs"}},
	{Name: "java", Extensions: []string{".java"}},
	{Name: "c", Extensions: []string{".c", ".h"}},
	{Name: "cpp", Aliases: []string{"c++"}, Extensions: []string{".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx"}},
	{Name: "csharp", Aliases: []string{"cs", "c#"}, Extensions: []string{".cs"}},
	{Name: "python", Aliases: []string{"py"}, Extensions: []string{".py"}},
	{Name: "rust", Aliases: []string{"rs"}, Extensions: []string{".rs"}},
	{Name: "kotlin", Aliases: []string{"kt"}, Extensions: []string{".kt", ".kts"}},
	{Name: "swift", Extensions: []string{".swift"}},
	{Name: "ruby", Aliases: []string{"rb"}, Extensions: []string{".rb"}},
	{Name: "shell", Aliases: []string{"sh", "bash", "zsh"}, Extensions: []string{".sh", ".bash", ".zsh"}},
	{Name: "sql", Extensions: []string{".sql"}},
}

type sourceFile struct {
	Path     string
	Language string
}

type FileSummary struct {
	Path     string
	Language string
}

type DiscoveryOptions struct {
	RepoRoot   string
	SourceDirs []string
	Excludes   []string
	MaxFiles   int
}

type DiscoveredInputs struct {
	Languages  []string
	Extensions []string
}

func Run(ctx context.Context, opts Options) (*Response, error) {
	opts = normalizeOptions(opts)
	root, err := sourcepath.CleanRoot(opts.RepoRoot)
	if err != nil {
		return nil, err
	}
	sourceDirs, err := sourcepath.CleanSourceDirs(root, opts.SourceDirs)
	if err != nil {
		return nil, err
	}
	codegraphFiles := sourceFilesFromSummaries(root, opts.SourceFiles, opts.Excludes)
	if len(codegraphFiles) > opts.MaxFiles {
		codegraphFiles = codegraphFiles[:opts.MaxFiles]
	}
	codegraphInputs := inputsFromSourceFiles(codegraphFiles)
	languages, extensions := enabledInputs(opts.Languages, opts.Extensions)
	extensions = unionStrings(extensions, codegraphInputs.Extensions)
	discoveryExtensions := differenceStrings(extensions, codegraphInputs.Extensions)
	files := codegraphFiles
	discovered, err := discoverFiles(root, sourceDirs, discoveryExtensions, opts.MaxFiles-len(files), opts.Excludes)
	if err != nil {
		return nil, err
	}
	files = append(files, discovered...)
	sortSourceFiles(files)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	candidates, summary, totalCandidates, err := runAsynkronQuickDup(ctx, root, files, opts)
	if err != nil {
		return nil, err
	}
	displaySourceDirs := displaySourceDirs(sourceDirs)
	summary.CandidatesReturned = len(candidates)
	return &Response{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   opts.Now().UTC(),
		RepoRoot:      root,
		Query:         Query{SourceDirs: displaySourceDirs, Languages: languages, Extensions: extensions, Excludes: opts.Excludes, TopN: opts.TopN, MinLines: opts.MinLines, MaxFiles: opts.MaxFiles},
		ScanInputs: ScanInputs{
			SourceDirs:          displaySourceDirs,
			SourceFiles:         len(codegraphFiles),
			Languages:           languages,
			Extensions:          extensions,
			Excludes:            opts.Excludes,
			InputSource:         strings.TrimSpace(opts.InputSource),
			CodegraphLanguages:  normalizeMetadataList(opts.CodegraphLanguages),
			CodegraphExtensions: normalizeExtensions(opts.CodegraphExtensions),
			FallbackLanguages:   normalizeMetadataList(opts.FallbackLanguages),
			FallbackExtensions:  normalizeExtensions(opts.FallbackExtensions),
			Limit:               opts.TopN,
			MaxFiles:            opts.MaxFiles,
			MinLines:            opts.MinLines,
		},
		Summary:         summary,
		TotalCandidates: totalCandidates,
		Candidates:      candidates,
		Tooling: Tooling{
			Engine:       "Asynkron.QuickDup",
			Mode:         "go-module-dependency",
			Source:       "github.com/asynkron/Asynkron.QuickDup/pkg/quickdup",
			SourceCommit: quickDupModuleVersion(),
			Strategy:     "normalized-indent",
		},
	}, nil
}

func Analyze(ctx context.Context, opts Options) (*Response, error) {
	return Run(ctx, opts)
}

// quickDupModuleVersion reports the resolved version of the
// github.com/asynkron/Asynkron.QuickDup module dependency (following a
// replace directive when one applies), so Tooling.SourceCommit reflects
// what actually built the running binary instead of a manually maintained
// pin.
func quickDupModuleVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, dep := range info.Deps {
		if dep.Path != "github.com/asynkron/Asynkron.QuickDup" {
			continue
		}
		if dep.Replace != nil && dep.Replace.Version != "" {
			return dep.Replace.Version
		}
		return dep.Version
	}
	return ""
}

func DiscoverInputs(ctx context.Context, opts DiscoveryOptions) (DiscoveredInputs, error) {
	runOpts := normalizeOptions(Options{
		RepoRoot:   opts.RepoRoot,
		SourceDirs: opts.SourceDirs,
		Excludes:   opts.Excludes,
		MaxFiles:   opts.MaxFiles,
	})
	root, err := sourcepath.CleanRoot(runOpts.RepoRoot)
	if err != nil {
		return DiscoveredInputs{}, err
	}
	sourceDirs, err := sourcepath.CleanSourceDirs(root, runOpts.SourceDirs)
	if err != nil {
		return DiscoveredInputs{}, err
	}
	_, extensions := enabledInputs(nil, nil)
	files, err := discoverFiles(root, sourceDirs, extensions, runOpts.MaxFiles, runOpts.Excludes)
	if err != nil {
		return DiscoveredInputs{}, err
	}
	if err := ctx.Err(); err != nil {
		return DiscoveredInputs{}, err
	}
	return inputsFromSourceFiles(files), nil
}

func DiscoverInputsFromCodegraphFiles(files []FileSummary) DiscoveredInputs {
	languages := map[string]bool{}
	extensions := map[string]bool{}
	for _, file := range sourceFilesFromSummaries("", files, nil) {
		languages[file.Language] = true
		ext := strings.ToLower(filepath.Ext(file.Path))
		extensions[ext] = true
	}
	return DiscoveredInputs{Languages: sortedKeys(languages), Extensions: sortedKeys(extensions)}
}

func LanguagesForExtensions(extensions []string) []string {
	byExt := languageByExtension()
	languages := map[string]bool{}
	for _, ext := range normalizeExtensions(extensions) {
		if language := byExt[ext]; language != "" {
			languages[language] = true
		}
	}
	return sortedKeys(languages)
}

func EffectiveExtensions(languages, extensions []string) []string {
	_, enabledExtensions := enabledInputs(languages, extensions)
	return enabledExtensions
}

func EffectiveMaxFiles(maxFiles int) int {
	if maxFiles <= 0 {
		return defaultMaxFiles
	}
	return maxFiles
}

func SourceFileFingerprint(files []FileSummary) string {
	normalized := sourceFilesFromSummaries("", files, nil)
	values := make([]string, 0, len(normalized))
	for _, file := range normalized {
		values = append(values, file.Path+"\x00"+file.Language)
	}
	sort.Strings(values)
	sum := sha1.Sum([]byte(strings.Join(values, "\x00")))
	return fmt.Sprintf("%x", sum[:])
}

func Marshal(ctx context.Context, opts Options) ([]byte, error) {
	resp, err := Run(ctx, opts)
	if err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

func sourceFilesFromSummaries(root string, summaries []FileSummary, excludes []string) []sourceFile {
	enabled := languageByExtension()
	seen := map[string]bool{}
	files := make([]sourceFile, 0, len(summaries))
	for _, summary := range summaries {
		rel := filepath.ToSlash(strings.TrimSpace(summary.Path))
		if rel == "" || filepath.IsAbs(rel) || strings.HasPrefix(rel, "../") || rel == ".." || pathExcluded(rel, excludes) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(rel))
		language, ok := enabled[ext]
		if !ok || sourcepath.IsTestFile(rel, language) {
			continue
		}
		if strings.TrimSpace(summary.Language) != "" {
			language = strings.ToLower(strings.TrimSpace(summary.Language))
		}
		if root != "" {
			abs := filepath.Join(root, filepath.FromSlash(rel))
			if _, err := os.Stat(abs); err != nil {
				continue
			}
		}
		key := rel + "\x00" + language
		if seen[key] {
			continue
		}
		seen[key] = true
		files = append(files, sourceFile{Path: rel, Language: language})
	}
	sortSourceFiles(files)
	return files
}

func sortSourceFiles(files []sourceFile) {
	sort.SliceStable(files, func(i, j int) bool {
		if files[i].Path != files[j].Path {
			return files[i].Path < files[j].Path
		}
		return files[i].Language < files[j].Language
	})
}

func runAsynkronQuickDup(ctx context.Context, root string, files []sourceFile, opts Options) ([]Candidate, Summary, int, error) {
	filesByExtension := make(map[string][]sourceFile)
	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file.Path))
		filesByExtension[ext] = append(filesByExtension[ext], file)
	}
	extensions := make([]string, 0, len(filesByExtension))
	for ext := range filesByExtension {
		extensions = append(extensions, ext)
	}
	sort.Strings(extensions)

	var all []Candidate
	summary := Summary{}
	for _, ext := range extensions {
		if err := ctx.Err(); err != nil {
			return nil, Summary{}, 0, err
		}
		group := filesByExtension[ext]
		absoluteFiles := make([]string, 0, len(group))
		languageByPath := make(map[string]string, len(group))
		for _, file := range group {
			abs := filepath.Join(root, filepath.FromSlash(file.Path))
			absoluteFiles = append(absoluteFiles, abs)
			languageByPath[abs] = file.Language
		}
		result, err := asynkronquickdup.AnalyzeFiles(asynkronquickdup.AnalyzeOptions{
			Files:         absoluteFiles,
			Strategy:      "normalized-indent",
			MinOccur:      2,
			MinRank:       0,
			MinSize:       opts.MinLines,
			MinSimilarity: 0.75,
		})
		if err != nil {
			return nil, Summary{}, 0, err
		}
		summary.FilesScanned += result.FilesScanned
		summary.LinesScanned += result.LinesScanned
		for _, match := range result.Matches {
			candidate, err := candidateFromMatch(root, languageByPath, match)
			if err != nil {
				return nil, Summary{}, 0, err
			}
			all = append(all, candidate)
		}
	}

	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Score != all[j].Score {
			return all[i].Score > all[j].Score
		}
		if all[i].LineCount != all[j].LineCount {
			return all[i].LineCount > all[j].LineCount
		}
		return all[i].Fingerprint < all[j].Fingerprint
	})
	var suppressed int
	all, suppressed = suppressOverlappingDuplicateCandidates(all)
	totalCandidates := len(all)
	summary.DuplicateBlocks = totalCandidates
	summary.DuplicateInstances = duplicateInstanceCount(all)
	summary.OverlappingCandidatesSuppressed = suppressed
	if len(all) > opts.TopN {
		all = all[:opts.TopN]
	}
	for i := range all {
		all[i].Rank = i + 1
	}
	return all, summary, totalCandidates, nil
}

func suppressOverlappingDuplicateCandidates(candidates []Candidate) ([]Candidate, int) {
	if len(candidates) < 2 {
		return candidates, 0
	}
	byOccurrenceSet := make(map[string][]int, len(candidates))
	for i, candidate := range candidates {
		key := candidateOccurrenceSetKey(candidate)
		byOccurrenceSet[key] = append(byOccurrenceSet[key], i)
	}

	suppressed := make([]bool, len(candidates))
	for _, indexes := range byOccurrenceSet {
		if len(indexes) < 2 {
			continue
		}
		for _, candidateIndex := range indexes {
			for _, containerIndex := range indexes {
				if candidateIndex == containerIndex {
					continue
				}
				if !candidateOverlapsDuplicateFamily(candidates[containerIndex], candidates[candidateIndex]) {
					continue
				}
				if candidatePreferredOver(candidates[containerIndex], candidates[candidateIndex], containerIndex, candidateIndex) {
					suppressed[candidateIndex] = true
					break
				}
			}
		}
	}

	filtered := make([]Candidate, 0, len(candidates))
	suppressedCount := 0
	for i, candidate := range candidates {
		if suppressed[i] {
			suppressedCount++
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered, suppressedCount
}

func candidateOccurrenceSetKey(candidate Candidate) string {
	var key strings.Builder
	for _, occurrence := range candidate.Occurrences {
		key.WriteString(occurrence.Path)
		key.WriteByte('\x00')
		key.WriteString(occurrence.Language)
		key.WriteByte('\x00')
	}
	return key.String()
}

func candidateOverlapsDuplicateFamily(left, right Candidate) bool {
	if len(left.Occurrences) != len(right.Occurrences) {
		return false
	}
	for i, rightOccurrence := range right.Occurrences {
		leftOccurrence := left.Occurrences[i]
		if leftOccurrence.Path != rightOccurrence.Path || leftOccurrence.Language != rightOccurrence.Language {
			return false
		}
		overlap := lineRangeOverlap(leftOccurrence, rightOccurrence)
		shorter := min(lineRangeLength(leftOccurrence), lineRangeLength(rightOccurrence))
		if shorter <= 0 || overlap*100 < shorter*duplicateFamilyOverlapPercent {
			return false
		}
	}
	return true
}

func candidatePreferredOver(left, right Candidate, leftIndex, rightIndex int) bool {
	if left.LineCount != right.LineCount {
		return left.LineCount > right.LineCount
	}
	if left.Score != right.Score {
		return left.Score > right.Score
	}
	return leftIndex < rightIndex
}

func lineRangeOverlap(left, right Occurrence) int {
	start := max(left.StartLine, right.StartLine)
	end := min(left.EndLine, right.EndLine)
	if end < start {
		return 0
	}
	return end - start + 1
}

func lineRangeLength(occurrence Occurrence) int {
	if occurrence.EndLine < occurrence.StartLine {
		return 0
	}
	return occurrence.EndLine - occurrence.StartLine + 1
}

func duplicateInstanceCount(candidates []Candidate) int {
	count := 0
	for _, candidate := range candidates {
		count += len(candidate.Occurrences)
	}
	return count
}

func candidateFromMatch(root string, languageByPath map[string]string, match asynkronquickdup.PatternMatch) (Candidate, error) {
	occurrences := make([]Occurrence, 0, len(match.Locations))
	for _, location := range match.Locations {
		rel, err := filepath.Rel(root, location.Filename)
		if err != nil {
			return Candidate{}, err
		}
		rel = filepath.ToSlash(rel)
		occurrences = append(occurrences, Occurrence{
			Path:      rel,
			Language:  languageByPath[location.Filename],
			StartLine: location.LineStart,
			EndLine:   location.LineStart + len(match.Pattern) - 1,
		})
	}
	sort.SliceStable(occurrences, func(i, j int) bool {
		if occurrences[i].Path != occurrences[j].Path {
			return occurrences[i].Path < occurrences[j].Path
		}
		return occurrences[i].StartLine < occurrences[j].StartLine
	})
	sample := make([]string, 0, len(match.Pattern))
	for _, entry := range match.Pattern {
		sample = append(sample, strings.TrimRight(entry.GetRaw(), "\r\n"))
	}
	return Candidate{
		Score:       match.Rank,
		Fingerprint: fmt.Sprintf("%016x", match.Hash),
		LineCount:   len(match.Pattern),
		Occurrences: occurrences,
		Sample:      sample,
		Preview:     strings.Join(sample, "\n"),
	}, nil
}

func normalizeOptions(opts Options) Options {
	if len(opts.SourceDirs) == 0 {
		opts.SourceDirs = []string{"."}
	}
	if opts.TopN <= 0 && opts.Limit > 0 {
		opts.TopN = opts.Limit
	}
	if opts.TopN <= 0 {
		opts.TopN = defaultTopN
	}
	if opts.TopN > maxTopN {
		opts.TopN = maxTopN
	}
	opts.Limit = opts.TopN
	if opts.MinLines <= 0 {
		opts.MinLines = defaultMinLines
	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = defaultMaxFiles
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	opts.Excludes = NormalizeExcludes(opts.Excludes)
	opts.CodegraphLanguages = normalizeMetadataList(opts.CodegraphLanguages)
	opts.CodegraphExtensions = normalizeExtensions(opts.CodegraphExtensions)
	opts.FallbackLanguages = normalizeMetadataList(opts.FallbackLanguages)
	opts.FallbackExtensions = normalizeExtensions(opts.FallbackExtensions)
	return opts
}

// NormalizeExcludes splits comma-separated values, slash-normalizes them, strips
// leading "./" and surrounding "/", drops blanks, dedupes, and sorts the exclude
// globs. The deterministic result is echoed in query/scan_inputs and feeds the
// HTTP cache key, so the scan identity is stable regardless of input order or
// separator style and a filtered scan can never reuse an unfiltered cache entry.
func NormalizeExcludes(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range splitValues(values) {
		pattern := strings.Trim(strings.TrimPrefix(filepath.ToSlash(strings.TrimSpace(raw)), "./"), "/")
		if pattern == "" || seen[pattern] {
			continue
		}
		seen[pattern] = true
		out = append(out, pattern)
	}
	sort.Strings(out)
	return out
}

func PathExcluded(relPath string, excludes []string) bool {
	return pathExcluded(relPath, excludes)
}

// pathExcluded reports whether the repository-relative slash path matches any
// normalized exclude glob. Multi-segment patterns are anchored at the repo root
// and support "**" to cross directory boundaries; single-segment patterns
// (no "/") match a file's basename or any directory segment at any depth, so
// "vendor" drops the whole vendor tree and "*.gen.go" drops generated files
// wherever they live. Matching stays on slash-normalized repository-relative
// paths only; it never reaches outside the scanned root.
func pathExcluded(relPath string, excludes []string) bool {
	if len(excludes) == 0 {
		return false
	}
	relPath = filepath.ToSlash(relPath)
	pathSegs := strings.Split(relPath, "/")
	for _, pattern := range excludes {
		if matchExcludeGlob(pattern, relPath, pathSegs) {
			return true
		}
	}
	return false
}

func matchExcludeGlob(pattern, relPath string, pathSegs []string) bool {
	// Plain (no glob metacharacter) patterns match the path itself or anything
	// nested under it, so a directory name excludes its whole subtree.
	if !strings.ContainsAny(pattern, "*?[") {
		if relPath == pattern || strings.HasPrefix(relPath, pattern+"/") {
			return true
		}
	}
	if !strings.Contains(pattern, "/") {
		// Single-segment globs match a file basename or any directory segment.
		for _, seg := range pathSegs {
			if ok, _ := path.Match(pattern, seg); ok {
				return true
			}
		}
		return false
	}
	if matchDoublestar(pattern, relPath) {
		return true
	}
	// A multi-segment pattern that matches a parent directory prefix excludes the
	// whole subtree below that directory.
	for i := 1; i < len(pathSegs); i++ {
		if matchDoublestar(pattern, strings.Join(pathSegs[:i], "/")) {
			return true
		}
	}
	return false
}

func matchDoublestar(pattern, relPath string) bool {
	ok, err := doublestar.Match(pattern, relPath)
	return err == nil && ok
}

func displaySourceDirs(sourceDirs []string) []string {
	out := make([]string, 0, len(sourceDirs))
	for _, sourceDir := range sourceDirs {
		if sourceDir == "" {
			out = append(out, ".")
			continue
		}
		out = append(out, sourceDir)
	}
	return out
}

func enabledInputs(languages, extensions []string) ([]string, []string) {
	enabledLanguages := map[string]bool{}
	enabledExtensions := map[string]bool{}
	for _, ext := range splitValues(extensions) {
		for _, normalized := range normalizeExtensionValue(ext) {
			enabledExtensions[normalized] = true
		}
	}
	requestedLanguages := map[string]bool{}
	for _, lang := range splitValues(languages) {
		requestedLanguages[strings.ToLower(lang)] = true
	}
	if len(requestedLanguages) == 0 && len(enabledExtensions) == 0 {
		for _, spec := range languageRegistry {
			enabledLanguages[spec.Name] = true
			for _, ext := range spec.Extensions {
				enabledExtensions[ext] = true
			}
		}
		return sortedKeys(enabledLanguages), sortedKeys(enabledExtensions)
	}
	for _, spec := range languageRegistry {
		matched := requestedLanguages[spec.Name]
		for _, alias := range spec.Aliases {
			matched = matched || requestedLanguages[alias]
		}
		if matched {
			enabledLanguages[spec.Name] = true
			for _, ext := range spec.Extensions {
				enabledExtensions[ext] = true
			}
		}
	}
	return sortedKeys(enabledLanguages), sortedKeys(enabledExtensions)
}

func discoverFiles(root string, sourceDirs []string, extensions []string, maxFiles int, excludes []string) ([]sourceFile, error) {
	if len(extensions) == 0 || maxFiles <= 0 {
		return nil, nil
	}
	enabled := languageByExtension()
	filter := map[string]bool{}
	for _, ext := range extensions {
		filter[ext] = true
	}
	// Prune excluded directories during the walk so large vendored or generated
	// trees are not descended into; the file-level filter below is the
	// authoritative guard for the actual exclude decision.
	var shouldSkipDir func(path, name string) bool
	if len(excludes) > 0 {
		shouldSkipDir = func(dirPath, _ string) bool {
			rel, err := filepath.Rel(root, dirPath)
			if err != nil {
				return false
			}
			return pathExcluded(filepath.ToSlash(rel), excludes)
		}
	}
	discovered, err := sourcepath.DiscoverScannerFiles(
		root,
		sourceDirs,
		maxFiles,
		"quickdup",
		[]string{".quickdup"},
		shouldSkipDir,
		func(relPath, ext string) bool {
			language := enabled[ext]
			return filter[ext] && !sourcepath.IsTestFile(relPath, language) && !pathExcluded(relPath, excludes)
		},
	)
	if err != nil {
		return nil, err
	}
	files := make([]sourceFile, 0, len(discovered))
	for _, path := range discovered {
		files = append(files, sourceFile{Path: path, Language: enabled[strings.ToLower(filepath.Ext(path))]})
	}
	return files, nil
}

func inputsFromSourceFiles(files []sourceFile) DiscoveredInputs {
	languages := map[string]bool{}
	extensions := map[string]bool{}
	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file.Path))
		if ext == "" {
			continue
		}
		extensions[ext] = true
		if strings.TrimSpace(file.Language) != "" {
			languages[file.Language] = true
		}
	}
	return DiscoveredInputs{Languages: sortedKeys(languages), Extensions: sortedKeys(extensions)}
}

func unionStrings(left, right []string) []string {
	seen := map[string]bool{}
	for _, value := range append(append([]string(nil), left...), right...) {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = true
		}
	}
	return sortedKeys(seen)
}

func differenceStrings(left, right []string) []string {
	remove := map[string]bool{}
	for _, value := range right {
		remove[value] = true
	}
	var out []string
	for _, value := range left {
		if !remove[value] {
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func languageByExtension() map[string]string {
	enabled := map[string]string{}
	for _, spec := range languageRegistry {
		for _, ext := range spec.Extensions {
			enabled[ext] = spec.Name
		}
	}
	return enabled
}

func normalizeMetadataList(values []string) []string {
	seen := map[string]bool{}
	for _, value := range splitValues(values) {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			seen[value] = true
		}
	}
	return sortedKeys(seen)
}

func normalizeExtensions(values []string) []string {
	seen := map[string]bool{}
	for _, value := range splitValues(values) {
		for _, ext := range normalizeExtensionValue(value) {
			seen[ext] = true
		}
	}
	return sortedKeys(seen)
}

func normalizeExtensionValue(value string) []string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil
	}
	if !strings.HasPrefix(value, ".") {
		value = "." + value
	}
	return []string{value}
}

func splitValues(values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func sortedKeys(in map[string]bool) []string {
	out := make([]string, 0, len(in))
	for key := range in {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
