package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadIgnoredHashes_CreatesFileWhenMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	strategyName := "word-indent"

	ignored := LoadIgnoredHashes(dir, strategyName)
	if ignored == nil {
		t.Fatalf("expected non-nil map")
	}

	ignorePath := filepath.Join(dir, ".quickdup", strategyName+"-ignore.json")
	if _, err := os.Stat(ignorePath); err != nil {
		t.Fatalf("expected ignore file to exist at %s: %v", ignorePath, err)
	}
}

func TestLoadIgnoredHashes_ParsesHexVariants(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	strategyName := "word-indent"
	ignorePath := filepath.Join(dir, ".quickdup", strategyName+"-ignore.json")
	if err := os.MkdirAll(filepath.Dir(ignorePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	data := []byte(`{
  "description": "Patterns to ignore",
  "ignored": [
    "56c2f5f9b27ed5a0",
    "[c32ca0ee344f8e23]",
    "0x0123456789abcdef",
    "not-hex",
    "",
    "   [0x000000000000000f]   "
  ]
}`)
	if err := os.WriteFile(ignorePath, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	ignored := LoadIgnoredHashes(dir, strategyName)

	want := []uint64{
		0x56c2f5f9b27ed5a0,
		0xc32ca0ee344f8e23,
		0x0123456789abcdef,
		0x000000000000000f,
	}
	for _, h := range want {
		if !ignored[h] {
			t.Fatalf("expected hash %016x to be ignored", h)
		}
	}
}

func TestParseIgnoredHash(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want uint64
		ok   bool
	}{
		{"56c2f5f9b27ed5a0", 0x56c2f5f9b27ed5a0, true},
		{"[56c2f5f9b27ed5a0]", 0x56c2f5f9b27ed5a0, true},
		{"0x56c2f5f9b27ed5a0", 0x56c2f5f9b27ed5a0, true},
		{"[0x56c2f5f9b27ed5a0]", 0x56c2f5f9b27ed5a0, true},
		{"   [0x56c2f5f9b27ed5a0]   ", 0x56c2f5f9b27ed5a0, true},
		{"", 0, false},
		{"[]", 0, false},
		{"not-hex", 0, false},
	}

	for _, tc := range cases {
		got, ok := parseIgnoredHash(tc.in)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("parseIgnoredHash(%q) = (%x, %v), want (%x, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
