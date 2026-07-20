package sourcepath

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCleanRootRejectsBlankRoot(t *testing.T) {
	if _, err := CleanRoot(" "); err == nil {
		t.Fatal("expected blank repository root to be rejected")
	}
}

func TestCleanSourceDirsCleansAndRejectsUnsafeInputs(t *testing.T) {
	root, err := CleanRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"b", "a", "nested/pkg"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	got, err := CleanSourceDirs(root, []string{"b, a", ".", "nested/../nested/pkg", "missing"})
	if err != nil {
		t.Fatalf("CleanSourceDirs returned error: %v", err)
	}
	if want := []string{"", "a", "b", "nested/pkg"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source dirs = %#v, want %#v", got, want)
	}
	for _, sourceDir := range []string{"../outside", filepath.Join(root, "absolute"), "bad\x00path"} {
		if _, err := CleanSourceDirs(root, []string{sourceDir}); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("source dir %q error = %v, want ErrUnsafePath", sourceDir, err)
		}
	}
}

func TestCleanSourceDirsRejectsSymlinkEscapes(t *testing.T) {
	root, err := CleanRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := CleanSourceDirs(root, []string{"link"}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("symlink escape error = %v, want ErrUnsafePath", err)
	}
}

func TestIsTestFileClassifiesSupportedLanguageConventions(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		language string
		want     bool
	}{
		{name: "go suffix", path: "pkg/foo_test.go", language: "go", want: true},
		{name: "typescript dot test", path: "src/foo.test.ts", language: "typescript", want: true},
		{name: "javascript tests dir", path: "src/__tests__/foo.js", language: "javascript", want: true},
		{name: "java test suffix", path: "src/FooTest.java", language: "java", want: true},
		{name: "c prefix", path: "src/test_foo.c", language: "c", want: true},
		{name: "python test dir", path: "tests/test_alpha.py", language: "python", want: true},
		{name: "python suffix", path: "app/alpha_test.py", language: "python", want: true},
		{name: "rust test dir", path: "tests/shared.rs", language: "rust", want: true},
		{name: "rust suffix", path: "src/shared_test.rs", language: "rust", want: true},
		{name: "kotlin suffix", path: "src/SharedSpec.kt", language: "kotlin", want: true},
		{name: "kotlin test suffix", path: "src/SharedTest.kt", language: "kotlin", want: true},
		{name: "swift suffix", path: "Tests/SharedTests.swift", language: "swift", want: true},
		{name: "ruby spec dir", path: "spec/shared_spec.rb", language: "ruby", want: true},
		{name: "shell prefix", path: "scripts/test_install.sh", language: "shell", want: true},
		{name: "sql tests dir", path: "tests/schema.sql", language: "sql", want: true},
		{name: "python production", path: "app/testimony.py", language: "python", want: false},
		{name: "rust production", path: "src/contest.rs", language: "rust", want: false},
		{name: "kotlin contest production", path: "src/Contest.kt", language: "kotlin", want: false},
		{name: "kotlin latest production", path: "src/Latest.kt", language: "kotlin", want: false},
		{name: "swift contest production", path: "src/Contest.swift", language: "swift", want: false},
		{name: "swift latest production", path: "src/Latest.swift", language: "swift", want: false},
		{name: "ruby production", path: "lib/special.rb", language: "ruby", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsTestFile(tt.path, tt.language); got != tt.want {
				t.Fatalf("IsTestFile(%q, %q) = %v, want %v", tt.path, tt.language, got, tt.want)
			}
		})
	}
}
