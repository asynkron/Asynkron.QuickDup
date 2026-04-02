package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveCacheSkipsNonWordIndent(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.go")
	if err := os.WriteFile(filePath, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write sample file: %v", err)
	}

	entries := []Entry{
		&WordIndentEntry{LineNumber: 1, IndentDelta: 0, Word: "package", SourceLine: "package main"},
	}
	fileData := map[string][]Entry{filePath: entries}

	saveCache(dir, "normalized-indent", []string{filePath}, fileData)

	cachePath := filepath.Join(dir, ".quickdup", "normalized-indent-cache.gob")
	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Fatalf("expected no cache file for non-word-indent strategy, got %v", err)
	}
}

func TestLoadCacheSkipsNonWordIndent(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, ".quickdup")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache dir: %v", err)
	}
	cachePath := filepath.Join(cacheDir, "normalized-indent-cache.gob")
	if err := os.WriteFile(cachePath, []byte("bogus"), 0644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}

	if cache := loadCache(dir, "normalized-indent"); cache != nil {
		t.Fatalf("expected nil cache for non-word-indent strategy")
	}
}

func TestSaveCacheWritesWordIndent(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "sample.go")
	if err := os.WriteFile(filePath, []byte("func main() {}\n"), 0644); err != nil {
		t.Fatalf("write sample file: %v", err)
	}

	entries := []Entry{
		&WordIndentEntry{LineNumber: 1, IndentDelta: 0, Word: "func", SourceLine: "func main() {}"},
	}
	fileData := map[string][]Entry{filePath: entries}

	saveCache(dir, "word-indent", []string{filePath}, fileData)

	cachePath := filepath.Join(dir, ".quickdup", "word-indent-cache.gob")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected cache file for word-indent strategy: %v", err)
	}

	cache := loadCache(dir, "word-indent")
	if cache == nil {
		t.Fatalf("expected cache to load for word-indent strategy")
	}
	cached, ok := cache.Files[filePath]
	if !ok {
		t.Fatalf("expected cached file data for %s", filePath)
	}
	if len(cached.Entries) != 1 {
		t.Fatalf("expected 1 cached entry, got %d", len(cached.Entries))
	}
	if cached.Entries[0].Word != "func" {
		t.Fatalf("expected cached entry word to be func, got %q", cached.Entries[0].Word)
	}
}
