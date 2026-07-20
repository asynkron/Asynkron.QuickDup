package sourcepath

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// initGitRepo creates a minimal Git work tree at dir so the gitignore-aware
// discovery contract can be exercised against real Git semantics. It skips the
// test when the git binary is unavailable.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func writeGitFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func discoverGo(t *testing.T, root string) []string {
	t.Helper()
	files, err := DiscoverScannerFiles(root, []string{""}, 0, "test", nil, nil, func(_, ext string) bool {
		return ext == ".go"
	})
	if err != nil {
		t.Fatalf("DiscoverScannerFiles: %v", err)
	}
	return files
}

// TestDiscoverScannerFilesSkipsGitignored proves AC-1 (an ignored directory is
// not descended into) and AC-2 (an ignored file outside an ignored directory is
// not included) using a real Git work tree with a repository-specific ignore
// pattern that is absent from the hard-coded skip list.
func TestDiscoverScannerFilesSkipsGitignored(t *testing.T) {
	root, err := CleanRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, root)
	writeGitFile(t, root, ".gitignore", "generated/\n*.gen.go\n")
	writeGitFile(t, root, "src/keep.go", "package src\n")
	writeGitFile(t, root, "src/keep.gen.go", "package src\n")           // ignored file (AC-2)
	writeGitFile(t, root, "generated/artifact.go", "package gen\n")     // ignored dir (AC-1)
	writeGitFile(t, root, "generated/nested/deep.go", "package deep\n") // ignored dir descendant

	got := discoverGo(t, root)
	want := []string{"src/keep.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discovered = %#v, want %#v", got, want)
	}
}

// TestDiscoverScannerFilesNonGitRootUnfiltered proves the fallback contract:
// a non-Git temporary root applies no gitignore filtering, so existing tests
// that use t.TempDir without git init keep seeing every eligible file.
func TestDiscoverScannerFilesNonGitRootUnfiltered(t *testing.T) {
	root, err := CleanRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeGitFile(t, root, ".gitignore", "generated/\n")
	writeGitFile(t, root, "src/keep.go", "package src\n")
	writeGitFile(t, root, "generated/artifact.go", "package gen\n")

	got := discoverGo(t, root)
	want := []string{"generated/artifact.go", "src/keep.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discovered = %#v, want %#v", got, want)
	}
}

func TestFilterGitIgnoredPreservesOrderAndRemovesIgnoredPaths(t *testing.T) {
	root, err := CleanRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, root)
	writeGitFile(t, root, ".gitignore", "generated/\n*.gen.go\n")
	writeGitFile(t, root, "src/keep.go", "package src\n")
	writeGitFile(t, root, "src/skip.gen.go", "package src\n")
	writeGitFile(t, root, "generated/skip.go", "package generated\n")

	got := FilterGitIgnored(root, []string{"src/keep.go", "generated/skip.go", "src/skip.gen.go"})
	want := []string{"src/keep.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered = %#v, want %#v", got, want)
	}
}
