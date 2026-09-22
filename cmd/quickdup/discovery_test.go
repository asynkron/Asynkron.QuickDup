package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func writeDiscoveryTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

func discoveredRelPaths(t *testing.T, root string, files []string) []string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	rels := make([]string, 0, len(files))
	for _, file := range files {
		rel, err := filepath.Rel(resolved, file)
		if err != nil {
			t.Fatalf("rel %s: %v", file, err)
		}
		rels = append(rels, filepath.ToSlash(rel))
	}
	sort.Strings(rels)
	return rels
}

func TestDiscoverScanFilesSkipsNodeModulesAndBuildDirs(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryTree(t, root, map[string]string{
		"src/app.ts":                "export const a = 1;\n",
		"node_modules/dep/index.ts": "export const junk = 1;\n",
		"dist/bundle.ts":            "export const built = 1;\n",
		"coverage/report.ts":        "export const report = 1;\n",
	})
	files, err := discoverScanFiles(root, ".ts", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := discoveredRelPaths(t, root, files)
	want := []string{"src/app.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discovered %v, want %v", got, want)
	}
}

func TestDiscoverScanFilesHonorsGitignore(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryTree(t, root, map[string]string{
		"src/app.ts":          "export const a = 1;\n",
		"generated/bundle.ts": "export const gen = 1;\n",
		"secret.ts":           "export const secret = 1;\n",
		".gitignore":          "secret.ts\ngenerated/\n",
	})
	if out, err := exec.Command("git", "init", root).CombinedOutput(); err != nil {
		t.Skipf("git unavailable: %v (%s)", err, out)
	}
	files, err := discoverScanFiles(root, ".ts", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := discoveredRelPaths(t, root, files)
	want := []string{"src/app.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discovered %v, want %v", got, want)
	}
}

func TestDiscoverScanFilesAppliesExcludePatterns(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryTree(t, root, map[string]string{
		"a.ts":            "export const a = 1;\n",
		"a.gen.ts":        "export const gen = 1;\n",
		"vendor-ish/b.ts": "export const b = 1;\n",
	})
	files, err := discoverScanFiles(root, ".ts", []string{"*.gen.ts", "vendor-ish"})
	if err != nil {
		t.Fatal(err)
	}
	got := discoveredRelPaths(t, root, files)
	want := []string{"a.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discovered %v, want %v", got, want)
	}
}

func TestDiscoverScanFilesFiltersByExtension(t *testing.T) {
	root := t.TempDir()
	writeDiscoveryTree(t, root, map[string]string{
		"a.ts": "export const a = 1;\n",
		"b.TS": "export const b = 1;\n",
		"c.js": "export const c = 1;\n",
	})
	files, err := discoverScanFiles(root, ".ts", nil)
	if err != nil {
		t.Fatal(err)
	}
	got := discoveredRelPaths(t, root, files)
	want := []string{"a.ts", "b.TS"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discovered %v, want %v", got, want)
	}
}

func TestExcludedByPatternsMatchesGlobAndSubstring(t *testing.T) {
	if !excludedByPatterns("pkg/a.pb.go", []string{"*.pb.go"}) {
		t.Fatal("basename glob pattern should exclude")
	}
	if !excludedByPatterns("pkg/vendor-ish/b.go", []string{"vendor-ish"}) {
		t.Fatal("substring pattern should exclude")
	}
	if excludedByPatterns("pkg/a.go", []string{"*.pb.go"}) {
		t.Fatal("unrelated file should not be excluded")
	}
	if !strings.Contains("x", "") && excludedByPatterns("pkg/a.go", nil) {
		t.Fatal("no patterns should never exclude")
	}
}
