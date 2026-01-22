package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIgnoredHashesUsesStrategySpecificFile(t *testing.T) {
	dir := t.TempDir()
	quickdupDir := filepath.Join(dir, ".quickdup")
	if err := os.MkdirAll(quickdupDir, 0755); err != nil {
		t.Fatalf("create .quickdup dir: %v", err)
	}

	ignore := IgnoreFile{Ignored: []string{"56c2f5f9b27ed5a0"}}
	ignoreData, err := json.Marshal(ignore)
	if err != nil {
		t.Fatalf("marshal ignore file: %v", err)
	}

	ignorePath := filepath.Join(quickdupDir, "word-indent-ignore.json")
	if err := os.WriteFile(ignorePath, ignoreData, 0644); err != nil {
		t.Fatalf("write ignore file: %v", err)
	}

	ignored := LoadIgnoredHashes(dir, "word-indent")
	var expected uint64
	if _, err := fmt.Sscanf(ignore.Ignored[0], "%x", &expected); err != nil {
		t.Fatalf("parse ignore hash: %v", err)
	}
	if ignored == nil || !ignored[expected] {
		t.Fatalf("expected hash %x to be ignored", expected)
	}

	otherPath := filepath.Join(quickdupDir, "normalized-indent-ignore.json")
	LoadIgnoredHashes(dir, "normalized-indent")
	if _, err := os.Stat(otherPath); err != nil {
		t.Fatalf("expected %s to be created: %v", otherPath, err)
	}
}
