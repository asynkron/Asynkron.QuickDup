package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveCacheWritesStrategySpecificFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	entry := NewWordIndentEntry(0, "package")
	entry.LineNumber = 1
	entry.SourceLine = "package main"

	files := []string{filePath}
	fileData := map[string][]Entry{
		filePath: {entry},
	}

	saveCache(dir, "word-indent", files, fileData)
	cachePath := filepath.Join(dir, ".quickdup", "word-indent-cache.gob")
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected cache file at %s: %v", cachePath, err)
	}

	cache := loadCache(dir, "word-indent")
	if cache == nil {
		t.Fatalf("expected cache to load")
	}
	if _, ok := cache.Files[filePath]; !ok {
		t.Fatalf("expected cache to include %s", filePath)
	}

	saveCache(dir, "normalized-indent", files, fileData)
	otherPath := filepath.Join(dir, ".quickdup", "normalized-indent-cache.gob")
	if _, err := os.Stat(otherPath); !os.IsNotExist(err) {
		t.Fatalf("expected no cache at %s", otherPath)
	}
}
