package sourcepath

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitIgnoreMatcher answers whether a repository-relative path is ignored by
// Git. Scanner discovery uses it so build artifacts and local generated state
// that live under gitignored files or directories never become false
// source-code evidence for Quickdup, Codegraph, or any other shared-discovery
// caller.
//
// The ignore decision is delegated to Git itself (rather than a hand-rolled
// .gitignore parser) so that nested .gitignore files, negations, the user's
// global excludesfile, and .git/info/exclude are all honored exactly the way
// Git would honor them. A single `git ls-files` invocation per scan collects
// the ignored set up front, so the walk never pays a per-file Git cost.
type gitIgnoreMatcher struct {
	files map[string]bool
	dirs  []string
}

// FilterGitIgnored returns repository-relative paths that are not ignored by
// Git. Non-Git roots and Git command failures return the input paths unchanged,
// matching DiscoverScannerFiles' best-effort ignore contract.
func FilterGitIgnored(root string, paths []string) []string {
	matcher := newGitIgnoreMatcher(root)
	if matcher == nil {
		return append([]string(nil), paths...)
	}
	filtered := make([]string, 0, len(paths))
	for _, path := range paths {
		path = filepath.ToSlash(path)
		if !matcher.ignoredFile(path) {
			filtered = append(filtered, path)
		}
	}
	return filtered
}

// newGitIgnoreMatcher builds a matcher for root. It returns nil when root is
// not a Git work tree, the git binary is unavailable, or nothing is ignored —
// in every one of those cases the scanner simply applies no gitignore filter,
// which keeps non-Git temporary test roots deterministic. The nil matcher is
// safe to call methods on.
func newGitIgnoreMatcher(root string) *gitIgnoreMatcher {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// --others --ignored --exclude-standard lists untracked, ignored paths
	// (honoring .gitignore, .git/info/exclude, and the global excludesfile);
	// --directory collapses a fully ignored directory to its name so we can
	// prune it before descending; -z keeps paths with odd characters intact.
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--directory")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	m := &gitIgnoreMatcher{files: map[string]bool{}}
	for _, entry := range strings.Split(string(out), "\x00") {
		if entry == "" {
			continue
		}
		entry = filepath.ToSlash(entry)
		if strings.HasSuffix(entry, "/") {
			m.dirs = append(m.dirs, strings.TrimSuffix(entry, "/"))
		} else {
			m.files[entry] = true
		}
	}
	if len(m.files) == 0 && len(m.dirs) == 0 {
		return nil
	}
	return m
}

// ignoredDir reports whether a repository-relative directory (slash-separated)
// is itself ignored or nested inside an ignored directory.
func (m *gitIgnoreMatcher) ignoredDir(rel string) bool {
	if m == nil {
		return false
	}
	for _, dir := range m.dirs {
		if rel == dir || strings.HasPrefix(rel, dir+"/") {
			return true
		}
	}
	return false
}

// ignoredFile reports whether a repository-relative file (slash-separated) is
// ignored, either by an exact match or because it lives under an ignored
// directory.
func (m *gitIgnoreMatcher) ignoredFile(rel string) bool {
	if m == nil {
		return false
	}
	if m.files[rel] {
		return true
	}
	return m.ignoredDir(rel)
}
