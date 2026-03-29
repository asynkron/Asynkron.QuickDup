package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIgnoredHashesCreatesStrategyIgnoreFile(t *testing.T) {
	dir := t.TempDir()

	_ = LoadIgnoredHashes(dir, "normalized-indent")

	ignorePath := filepath.Join(dir, ".quickdup", "normalized-indent-ignore.json")
	if _, err := os.Stat(ignorePath); err != nil {
		t.Fatalf("expected ignore file at %s: %v", ignorePath, err)
	}
}
