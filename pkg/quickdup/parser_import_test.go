package quickdup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupImportTestParser(t *testing.T, ext string) {
	t.Helper()
	prevStrategy := ActiveStrategy
	ActiveStrategy = &NormalizedIndentStrategy{}
	CommentPrefix = "//"
	path := filepath.Join(t.TempDir(), "probe"+ext)
	setCurrentFileExt(path)
	t.Cleanup(func() { ActiveStrategy = prevStrategy })
}

func TestParseFileSkipsMultilineImportBlocks(t *testing.T) {
	setupImportTestParser(t, ".mjs")
	src := `import {
  Foo,
  Bar,
} from 'deps'
import single from 'other'

export function helper() {
  return 1
}
`
	path := filepath.Join(t.TempDir(), "a.mjs")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := parseFile(path)
	if err != nil {
		t.Fatal(err)
	}

	banned := []string{"import {", "Foo,", "Bar,", "} from 'deps'", "import single from 'other'"}
	sawHelper := false
	for _, e := range entries {
		raw := strings.TrimSpace(e.GetRaw())
		for _, b := range banned {
			if raw == b {
				t.Fatalf("import line became an entry: %q", raw)
			}
		}
		if strings.Contains(raw, "return 1") {
			sawHelper = true
		}
	}
	if !sawHelper {
		t.Fatal("real code after the import block was not parsed")
	}
}

func TestParseFileSkipsGoImportBlock(t *testing.T) {
	setupImportTestParser(t, ".go")
	src := `package x

import (
	"fmt"
	"os"
)

func run() {
	fmt.Println(os.Args[0])
}
`
	path := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := parseFile(path)
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range entries {
		raw := strings.TrimSpace(e.GetRaw())
		if raw == `"fmt"` || raw == `"os"` || raw == `import (` || raw == `)` || strings.HasPrefix(raw, "package") {
			t.Fatalf("import/module line became an entry: %q", raw)
		}
	}
}

func TestParseFileKeepsCodeAfterClosedImportBlock(t *testing.T) {
	setupImportTestParser(t, ".ts")
	src := `import {
  A,
  B,
} from 'deps'

const value = compute(A)
`
	path := filepath.Join(t.TempDir(), "a.ts")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := parseFile(path)
	if err != nil {
		t.Fatal(err)
	}

	sawValue := false
	for _, e := range entries {
		raw := strings.TrimSpace(e.GetRaw())
		if strings.HasPrefix(raw, "const value") {
			sawValue = true
		}
		if raw == "A" || raw == "B," || strings.HasPrefix(raw, "} from") {
			t.Fatalf("line inside import block became an entry: %q", raw)
		}
	}
	if !sawValue {
		t.Fatal("code after the closed import block was dropped")
	}
}

func TestSkipImportLineBlockStateTransitions(t *testing.T) {
	kw := map[string]bool{"import": true}
	var inBlock bool

	if !skipImportLine("import (", kw, &inBlock) || !inBlock {
		t.Fatal("opener should be skipped and open a block")
	}
	if !skipImportLine(`  "fmt"`, kw, &inBlock) || !inBlock {
		t.Fatal("block body should be skipped")
	}
	if !skipImportLine(")", kw, &inBlock) || inBlock {
		t.Fatal("closer should be skipped and close the block")
	}
	if skipImportLine("func run() {", kw, &inBlock) {
		t.Fatal("code after the block must not be skipped")
	}
	if skipImportLine("x := 1", kw, &inBlock) {
		t.Fatal("plain code must not be skipped")
	}
	if skipImportLine("import x from 'y'", map[string]bool{}, &inBlock) {
		t.Fatal("extension without import keywords must never skip")
	}
}
