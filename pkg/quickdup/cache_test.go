package quickdup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWordIndentCacheUsesStrategySpecificFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "sample.go")
	if err := os.WriteFile(sourcePath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	entry := NewWordIndentEntry(0, "package")
	entry.LineNumber = 1
	entry.SourceLine = "package main"

	SaveCache(dir, "word-indent", []string{sourcePath}, map[string][]Entry{
		sourcePath: {entry},
	})

	cachePath := filepath.Join(dir, ".quickdup", "word-indent-cache.gob")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected cache at %s: %v", cachePath, err)
	}
	cache := LoadCache(dir, "word-indent")
	if cache == nil {
		t.Fatal("LoadCache() = nil, want the word-indent cache")
	}
	cached, ok := cache.Files[sourcePath]
	if !ok || len(cached.Entries) != 1 {
		t.Fatalf("cached entry = %+v, %v; want one package entry", cached, ok)
	}
	actual := cached.Entries[0]
	if actual.LineNumber != entry.LineNumber ||
		actual.IndentDelta != entry.IndentDelta ||
		actual.Word != entry.Word ||
		actual.SourceLine != entry.SourceLine {
		t.Fatalf("cached entry = %+v; want %+v", actual, *entry)
	}
	if string(actual.HashBytes()) != string(entry.HashBytes()) {
		t.Fatalf("cached hash bytes = %q; want %q", actual.HashBytes(), entry.HashBytes())
	}
}

func TestNonWordIndentStrategiesSkipCachePersistence(t *testing.T) {
	strategies := []string{"normalized-indent", "word-only", "inlineable"}
	for _, strategyName := range strategies {
		t.Run(strategyName, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cachePath := filepath.Join(dir, ".quickdup", strategyName+"-cache.gob")
			SaveCache(dir, strategyName, nil, nil)
			if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
				t.Fatalf("SaveCache() created %s for a non-cacheable strategy: %v", cachePath, err)
			}

			if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
				t.Fatalf("create cache directory: %v", err)
			}
			if err := os.WriteFile(cachePath, []byte("bogus"), 0o644); err != nil {
				t.Fatalf("write bogus cache: %v", err)
			}
			if cache := LoadCache(dir, strategyName); cache != nil {
				t.Fatalf("LoadCache() = %+v, want nil for non-cacheable strategy", cache)
			}
		})
	}
}
