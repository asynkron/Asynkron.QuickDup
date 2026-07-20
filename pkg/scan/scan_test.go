package scan

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunReturnsSchemaRankedDuplicatesFromCompiledQuickDup(t *testing.T) {
	root := t.TempDir()
	block := `package sample

func Alpha() {
	x := 1
	y := 2
	z := x + y
	_ = z
}
`
	writeQuickDupFile(t, root, "a.go", block)
	writeQuickDupFile(t, root, "nested/b.go", block)
	writeQuickDupFile(t, root, "nested/b_test.go", block)

	resp, err := Run(context.Background(), Options{
		RepoRoot:   root,
		SourceDirs: []string{"."},
		Languages:  []string{"go"},
		TopN:       1,
		MinLines:   3,
		Now:        func() time.Time { return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if resp.SchemaVersion != SchemaVersion {
		t.Fatalf("schema_version = %q", resp.SchemaVersion)
	}
	if resp.GeneratedAt.Format(time.RFC3339) != "2026-06-19T12:00:00Z" {
		t.Fatalf("generated_at = %s", resp.GeneratedAt)
	}
	if resp.Query.TopN != 1 || resp.Query.MinLines != 3 {
		t.Fatalf("query echo = %+v", resp.Query)
	}
	if resp.ScanInputs.Limit != 1 || len(resp.ScanInputs.SourceDirs) != 1 || resp.ScanInputs.SourceDirs[0] != "." {
		t.Fatalf("scan inputs = %+v", resp.ScanInputs)
	}
	if resp.Summary.FilesScanned != 2 || resp.Summary.CandidatesReturned != 1 {
		t.Fatalf("summary = %+v", resp.Summary)
	}
	if len(resp.UnavailableReasons) != 0 {
		t.Fatalf("unexpected unavailable metadata from compiled QuickDup: %+v", resp.UnavailableReasons)
	}
	if resp.Tooling.Engine != "Asynkron.QuickDup" || resp.Tooling.Mode != "go-module-dependency" || resp.Tooling.Source == "" {
		t.Fatalf("tooling = %+v, want Asynkron.QuickDup module dependency identity", resp.Tooling)
	}
	if len(resp.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1: %+v", len(resp.Candidates), resp.Candidates)
	}
	candidate := resp.Candidates[0]
	if candidate.Rank != 1 || candidate.Score <= 0 || candidate.LineCount < 3 {
		t.Fatalf("bad top candidate ranking: %+v", candidate)
	}
	if len(candidate.Sample) != candidate.LineCount || candidate.Preview == "" {
		t.Fatalf("candidate source preview missing: %+v", candidate)
	}
	if !containsQuickDupLine(candidate.Sample, "\tx := 1") {
		t.Fatalf("candidate sample should preserve leading indentation: %#v", candidate.Sample)
	}
	if len(candidate.Occurrences) != 2 {
		t.Fatalf("test files should be excluded; occurrences = %+v", candidate.Occurrences)
	}
	if _, err := os.Stat(filepath.Join(root, ".quickdup")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("QuickDup left a .quickdup artifact: %v", err)
	}
}

func TestAnalyzeKeepsLimitCompatibilityAlias(t *testing.T) {
	root := t.TempDir()
	writeQuickDupFile(t, root, "a.go", "package a\nfunc A() {\nprintln(1)\nprintln(2)\nprintln(3)\n}\n")
	writeQuickDupFile(t, root, "b.go", "package b\nfunc B() {\nprintln(1)\nprintln(2)\nprintln(3)\n}\n")
	resp, err := Analyze(context.Background(), Options{
		RepoRoot:  root,
		Languages: []string{"go"},
		Limit:     1,
		MinLines:  3,
		Now:       func() time.Time { return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}
	if resp.Query.TopN != 1 || resp.ScanInputs.Limit != 1 || len(resp.Candidates) != 1 {
		t.Fatalf("limit alias not honored: query=%+v inputs=%+v candidates=%d", resp.Query, resp.ScanInputs, len(resp.Candidates))
	}
}

func TestRunDefaultScanDoesNotCompareAcrossExactExtensions(t *testing.T) {
	root := t.TempDir()
	block := "shared line one\nshared line two\nshared line three\nshared line four\n"
	writeQuickDupFile(t, root, "a.go", block)
	writeQuickDupFile(t, root, "b.ts", block)

	resp, err := Run(context.Background(), Options{
		RepoRoot: root,
		TopN:     10,
		MinLines: 3,
		Now: func() time.Time {
			return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if resp.Summary.FilesScanned != 2 {
		t.Fatalf("files_scanned = %d, want 2", resp.Summary.FilesScanned)
	}
	if resp.TotalCandidates != 0 || len(resp.Candidates) != 0 {
		t.Fatalf("cross-extension duplicate leaked into candidates: total=%d candidates=%+v", resp.TotalCandidates, resp.Candidates)
	}
}

func TestRunUsesExactSourceFilesBeforeFallbackDiscovery(t *testing.T) {
	root := t.TempDir()
	goBlock := "package sample\nfunc Shared() {\nprintln(1)\nprintln(2)\nprintln(3)\n}\n"
	ignoredBlock := "package ignored\nfunc Ignored() {\nprintln(1)\nprintln(2)\nprintln(3)\n}\n"
	writeQuickDupFile(t, root, "graph/a.go", goBlock)
	writeQuickDupFile(t, root, "graph/b.go", goBlock)
	writeQuickDupFile(t, root, "outside/a.go", ignoredBlock)
	writeQuickDupFile(t, root, "outside/b.go", ignoredBlock)

	resp, err := Run(context.Background(), Options{
		RepoRoot: root,
		SourceFiles: []FileSummary{
			{Path: "graph/a.go", Language: "go"},
			{Path: "graph/b.go", Language: "go"},
		},
		Extensions:          []string{".go"},
		InputSource:         "codegraph",
		CodegraphLanguages:  []string{"go"},
		CodegraphExtensions: []string{".go"},
		TopN:                10,
		MinLines:            3,
		Now: func() time.Time {
			return time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if resp.ScanInputs.InputSource != "codegraph" || resp.ScanInputs.SourceFiles != 2 {
		t.Fatalf("scan provenance = %+v, want codegraph with two exact source files", resp.ScanInputs)
	}
	if resp.Summary.FilesScanned != 2 {
		t.Fatalf("files_scanned = %d, want only exact codegraph files", resp.Summary.FilesScanned)
	}
	for _, candidate := range resp.Candidates {
		for _, occurrence := range candidate.Occurrences {
			if occurrence.Path != "graph/a.go" && occurrence.Path != "graph/b.go" {
				t.Fatalf("fallback discovery path leaked into exact source-file scan: %q", occurrence.Path)
			}
		}
	}
}

func TestSuppressOverlappingDuplicateCandidatesDropsShiftedWindows(t *testing.T) {
	candidates := []Candidate{
		{
			Score:       95,
			Fingerprint: "shifted",
			LineCount:   9,
			Occurrences: []Occurrence{
				{Path: "a.go", Language: "go", StartLine: 12, EndLine: 20},
				{Path: "b.go", Language: "go", StartLine: 32, EndLine: 40},
			},
		},
		{
			Score:       90,
			Fingerprint: "whole",
			LineCount:   10,
			Occurrences: []Occurrence{
				{Path: "a.go", Language: "go", StartLine: 10, EndLine: 19},
				{Path: "b.go", Language: "go", StartLine: 30, EndLine: 39},
			},
		},
		{
			Score:       80,
			Fingerprint: "distinct",
			LineCount:   6,
			Occurrences: []Occurrence{
				{Path: "a.go", Language: "go", StartLine: 50, EndLine: 55},
				{Path: "b.go", Language: "go", StartLine: 70, EndLine: 75},
			},
		},
		{
			Score:       75,
			Fingerprint: "low-overlap",
			LineCount:   9,
			Occurrences: []Occurrence{
				{Path: "a.go", Language: "go", StartLine: 18, EndLine: 26},
				{Path: "b.go", Language: "go", StartLine: 38, EndLine: 46},
			},
		},
		{
			Score:       70,
			Fingerprint: "other-path",
			LineCount:   9,
			Occurrences: []Occurrence{
				{Path: "a.go", Language: "go", StartLine: 11, EndLine: 19},
				{Path: "c.go", Language: "go", StartLine: 31, EndLine: 39},
			},
		},
	}

	filtered, suppressed := suppressOverlappingDuplicateCandidates(candidates)
	if suppressed != 1 {
		t.Fatalf("suppressed = %d, want 1", suppressed)
	}
	if len(filtered) != 4 {
		t.Fatalf("filtered candidates = %d, want 4: %+v", len(filtered), filtered)
	}
	fingerprints := map[string]bool{}
	for _, candidate := range filtered {
		fingerprints[candidate.Fingerprint] = true
	}
	if fingerprints["shifted"] {
		t.Fatalf("shifted overlapping candidate should be suppressed: %+v", filtered)
	}
	for _, want := range []string{"whole", "distinct", "low-overlap", "other-path"} {
		if !fingerprints[want] {
			t.Fatalf("filtered candidates missing %q: %+v", want, filtered)
		}
	}
}

func TestRunRejectsUnsafeSourcePaths(t *testing.T) {
	root := t.TempDir()
	for _, source := range []string{"../outside", filepath.Join(root, "absolute")} {
		_, err := Run(context.Background(), Options{RepoRoot: root, SourceDirs: []string{source}})
		if err == nil {
			t.Fatalf("expected unsafe source path %q to be rejected", source)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := Run(context.Background(), Options{RepoRoot: root, SourceDirs: []string{"link"}})
	if err == nil {
		t.Fatal("expected outside symlink source to be rejected")
	}
}

func TestRunHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Run(ctx, Options{RepoRoot: t.TempDir()})
	if err == nil {
		t.Fatal("expected cancelled context to stop scan")
	}
}

func TestRunFiltersExtensions(t *testing.T) {
	root := t.TempDir()
	writeQuickDupFile(t, root, "a.go", "package a\nfunc A() {}\nfunc B() {}\nfunc C() {}\n")
	writeQuickDupFile(t, root, "a.ts", "export function A() {}\nexport function B() {}\nexport function C() {}\n")
	resp, err := Run(context.Background(), Options{RepoRoot: root, Extensions: []string{"ts"}, MinLines: 2})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if len(resp.Query.Extensions) != 1 || resp.Query.Extensions[0] != ".ts" {
		t.Fatalf("extensions = %+v", resp.Query.Extensions)
	}
	if len(resp.Query.Languages) != 0 {
		t.Fatalf("language should not be inferred for extension-only filters: %+v", resp.Query.Languages)
	}
}

func TestRunExcludesNonGoTestFileDuplicates(t *testing.T) {
	root := t.TempDir()
	block := "def repeated_logic(value):\n    total = value + 1\n    total = total * 2\n    return total\n"
	writeQuickDupFile(t, root, "app/alpha.py", block)
	writeQuickDupFile(t, root, "app/beta.py", block)
	writeQuickDupFile(t, root, "tests/test_alpha.py", block)
	writeQuickDupFile(t, root, "tests/test_beta.py", block)

	resp, err := Run(context.Background(), Options{
		RepoRoot:   root,
		SourceDirs: []string{"."},
		Languages:  []string{"python"},
		TopN:       20,
		MinLines:   3,
		Now:        func() time.Time { return time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if resp.Summary.FilesScanned != 2 {
		t.Fatalf("files_scanned = %d, want 2 production Python files", resp.Summary.FilesScanned)
	}
	if len(resp.Candidates) == 0 {
		t.Fatal("expected production Python duplicate candidate to remain")
	}
	for _, candidate := range resp.Candidates {
		for _, occurrence := range candidate.Occurrences {
			if strings.HasPrefix(filepath.ToSlash(occurrence.Path), "tests/") {
				t.Fatalf("test file leaked into duplicate occurrence: %+v", occurrence)
			}
		}
	}
}

func TestRunAppliesExcludeGlobs(t *testing.T) {
	root := t.TempDir()
	block := "package sample\nfunc Alpha() {\nx := 1\ny := 2\nz := x + y\n_ = z\n}\n"
	// A duplicate pair lives under vendor/ and a separate duplicate pair lives in
	// source we want to keep. Excluding the vendored tree and a generated-file
	// glob must drop those occurrences without touching the rest of the scan.
	writeQuickDupFile(t, root, "thirdparty/dep/a.go", block)
	writeQuickDupFile(t, root, "thirdparty/dep/b.go", block)
	writeQuickDupFile(t, root, "pkg/gen.pb.go", block)
	writeQuickDupFile(t, root, "pkg/real_a.go", block)
	writeQuickDupFile(t, root, "pkg/real_b.go", block)

	resp, err := Run(context.Background(), Options{
		RepoRoot:   root,
		SourceDirs: []string{"."},
		Languages:  []string{"go"},
		Excludes:   []string{"thirdparty", " **/*.pb.go "},
		TopN:       20,
		MinLines:   3,
		Now:        func() time.Time { return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	// Normalized excludes are echoed deterministically (sorted, slash-trimmed) in
	// both query and scan_inputs so copied evidence records the scan identity.
	wantExcludes := []string{"**/*.pb.go", "thirdparty"}
	if !equalStrings(resp.Query.Excludes, wantExcludes) {
		t.Fatalf("query excludes = %#v, want %#v", resp.Query.Excludes, wantExcludes)
	}
	if !equalStrings(resp.ScanInputs.Excludes, wantExcludes) {
		t.Fatalf("scan_inputs excludes = %#v, want %#v", resp.ScanInputs.Excludes, wantExcludes)
	}
	// Only the two non-excluded source files remain in the scan, leaving exactly
	// one duplicate candidate over real_a.go/real_b.go.
	if resp.Summary.FilesScanned != 2 {
		t.Fatalf("files_scanned = %d, want 2 (excluded files must not be scanned)", resp.Summary.FilesScanned)
	}
	for _, candidate := range resp.Candidates {
		for _, occurrence := range candidate.Occurrences {
			if occurrence.Path != "pkg/real_a.go" && occurrence.Path != "pkg/real_b.go" {
				t.Fatalf("excluded path leaked into candidate occurrences: %q", occurrence.Path)
			}
		}
	}

	// Without excludes the same tree scans every non-test file, proving the
	// default behavior is unchanged.
	full, err := Run(context.Background(), Options{
		RepoRoot:   root,
		SourceDirs: []string{"."},
		Languages:  []string{"go"},
		TopN:       20,
		MinLines:   3,
		Now:        func() time.Time { return time.Date(2026, 6, 19, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Run (no excludes) returned error: %v", err)
	}
	if full.Summary.FilesScanned != 5 {
		t.Fatalf("default files_scanned = %d, want 5 (excludes must not change default behavior)", full.Summary.FilesScanned)
	}
	if len(full.Query.Excludes) != 0 {
		t.Fatalf("default query excludes = %#v, want empty", full.Query.Excludes)
	}
}

func TestNormalizeExcludesDeterministic(t *testing.T) {
	got := NormalizeExcludes([]string{"./vendor/", "a,b", " a ", "", "vendor"})
	want := []string{"a", "b", "vendor"}
	if !equalStrings(got, want) {
		t.Fatalf("NormalizeExcludes = %#v, want %#v", got, want)
	}
}

func TestPathExcludedCompatibility(t *testing.T) {
	tests := []struct {
		name     string
		relPath  string
		excludes []string
		want     bool
	}{
		{
			name:     "plain exact path",
			relPath:  "generated",
			excludes: []string{"generated"},
			want:     true,
		},
		{
			name:     "plain directory subtree",
			relPath:  "generated/client/a.go",
			excludes: []string{"generated"},
			want:     true,
		},
		{
			name:     "plain partial segment does not match",
			relPath:  "generated-client/a.go",
			excludes: []string{"generated"},
			want:     false,
		},
		{
			name:     "single segment directory any depth",
			relPath:  "internal/vendor/pkg/a.go",
			excludes: []string{"vendor"},
			want:     true,
		},
		{
			name:     "single segment basename glob any depth",
			relPath:  "pkg/generated/client.gen.go",
			excludes: []string{"*.gen.go"},
			want:     true,
		},
		{
			name:     "single segment glob does not cross segment",
			relPath:  "pkg/generated/client.go",
			excludes: []string{"*.gen.go"},
			want:     false,
		},
		{
			name:     "multi segment pattern is root anchored",
			relPath:  "internal/generated/client.go",
			excludes: []string{"generated/*.go"},
			want:     false,
		},
		{
			name:     "multi segment pattern matches from root",
			relPath:  "generated/client.go",
			excludes: []string{"generated/*.go"},
			want:     true,
		},
		{
			name:     "doublestar crosses directories",
			relPath:  "internal/api/testdata/fixture.go",
			excludes: []string{"internal/**/testdata"},
			want:     true,
		},
		{
			name:     "doublestar basename recursive",
			relPath:  "pkg/generated/client.pb.go",
			excludes: []string{"**/*.pb.go"},
			want:     true,
		},
		{
			name:     "multi segment parent prefix excludes subtree",
			relPath:  "internal/api/testdata/fixtures/a.go",
			excludes: []string{"internal/**/testdata"},
			want:     true,
		},
		{
			name:     "slash normalized relative path",
			relPath:  filepath.Join("internal", "vendor", "pkg", "a.go"),
			excludes: []string{"vendor"},
			want:     true,
		},
		{
			name:     "malformed single segment pattern is tolerated",
			relPath:  "pkg/a.go",
			excludes: []string{"["},
			want:     false,
		},
		{
			name:     "malformed multi segment pattern is tolerated",
			relPath:  "pkg/a.go",
			excludes: []string{"pkg/["},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PathExcluded(tt.relPath, tt.excludes); got != tt.want {
				t.Fatalf("PathExcluded(%q, %#v) = %v, want %v", tt.relPath, tt.excludes, got, tt.want)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func writeQuickDupFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func initQuickDupGitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// TestRunIgnoresGitignoredDuplicates proves AC-3: a duplicate block that exists
// only in gitignored files or folders is neither scanned nor reported, while a
// genuine duplicate between tracked source files is still detected.
func TestRunIgnoresGitignoredDuplicates(t *testing.T) {
	root := t.TempDir()
	initQuickDupGitRepo(t, root)
	block := `package sample

func Alpha() {
	x := 1
	y := 2
	z := x + y
	_ = z
}
`
	writeQuickDupFile(t, root, ".gitignore", "generated/\n*.gen.go\n")
	writeQuickDupFile(t, root, "src/one.go", block)        // tracked duplicate
	writeQuickDupFile(t, root, "src/two.go", block)        // tracked duplicate
	writeQuickDupFile(t, root, "generated/leak.go", block) // ignored directory
	writeQuickDupFile(t, root, "src/leak.gen.go", block)   // ignored file

	resp, err := Run(context.Background(), Options{
		RepoRoot:   root,
		SourceDirs: []string{"."},
		Languages:  []string{"go"},
		TopN:       5,
		MinLines:   3,
		Now:        func() time.Time { return time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if resp.Summary.FilesScanned != 2 {
		t.Fatalf("files_scanned = %d, want 2 (gitignored files must not be scanned)", resp.Summary.FilesScanned)
	}
	for _, candidate := range resp.Candidates {
		for _, occ := range candidate.Occurrences {
			path := filepath.ToSlash(occ.Path)
			if path != "src/one.go" && path != "src/two.go" {
				t.Fatalf("gitignored source leaked into duplicate occurrences: %q", occ.Path)
			}
		}
	}
}

func containsQuickDupLine(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}
