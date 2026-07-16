package quickdup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseIgnoredHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected uint64
		valid    bool
	}{
		{name: "raw", input: "56c2f5f9b27ed5a0", expected: 0x56c2f5f9b27ed5a0, valid: true},
		{name: "bracketed", input: "[56c2f5f9b27ed5a0]", expected: 0x56c2f5f9b27ed5a0, valid: true},
		{name: "prefixed", input: "0x56c2f5f9b27ed5a0", expected: 0x56c2f5f9b27ed5a0, valid: true},
		{name: "bracketed prefixed", input: "  [0X0f]  ", expected: 0x0f, valid: true},
		{name: "empty", input: "", valid: false},
		{name: "empty brackets", input: "[]", valid: false},
		{name: "unclosed bracket", input: "[0f", valid: false},
		{name: "unexpected sign", input: "+0f", valid: false},
		{name: "not hexadecimal", input: "not-hex", valid: false},
		{name: "overflow", input: "10000000000000000", valid: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			actual, valid := parseIgnoredHash(test.input)
			if actual != test.expected || valid != test.valid {
				t.Fatalf("parseIgnoredHash(%q) = (%x, %v), want (%x, %v)", test.input, actual, valid, test.expected, test.valid)
			}
		})
	}
}

func TestLoadIgnoredHashesCreatesStrategyFileWhenMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ignored := LoadIgnoredHashes(dir, "normalized-indent")
	if ignored == nil || len(ignored) != 0 {
		t.Fatalf("LoadIgnoredHashes() = %v, want an empty map", ignored)
	}

	path := filepath.Join(dir, ".quickdup", "normalized-indent-ignore.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created ignore file: %v", err)
	}
	var created IgnoreFile
	if err := json.Unmarshal(data, &created); err != nil {
		t.Fatalf("parse created ignore file: %v", err)
	}
	if created.Ignored == nil || len(created.Ignored) != 0 {
		t.Fatalf("created ignored hashes = %v, want []", created.Ignored)
	}
}

func TestLoadIgnoredHashesParsesSupportedVariantsAndSkipsInvalidValues(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".quickdup", "word-indent-ignore.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create ignore directory: %v", err)
	}
	data := []byte(`{"ignored":["56c2f5f9b27ed5a0","[c32ca0ee344f8e23]","0x0123456789abcdef","[0X0f]","not-hex",""]}`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write ignore file: %v", err)
	}

	ignored := LoadIgnoredHashes(dir, "word-indent")
	expected := []uint64{0x56c2f5f9b27ed5a0, 0xc32ca0ee344f8e23, 0x0123456789abcdef, 0x0f}
	if len(ignored) != len(expected) {
		t.Fatalf("ignored hashes = %v, want %d supported values", ignored, len(expected))
	}
	for _, hash := range expected {
		if !ignored[hash] {
			t.Errorf("expected hash %016x to be ignored", hash)
		}
	}
}

func TestLoadIgnoredHashesReturnsEmptyMapForInvalidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".quickdup", "inlineable-ignore.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create ignore directory: %v", err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatalf("write invalid ignore file: %v", err)
	}

	ignored := LoadIgnoredHashes(dir, "inlineable")
	if ignored == nil || len(ignored) != 0 {
		t.Fatalf("LoadIgnoredHashes() = %v, want an empty map", ignored)
	}
}
