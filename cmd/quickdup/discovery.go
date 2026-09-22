package main

import (
	"path/filepath"
	"strings"

	"github.com/asynkron/Asynkron.QuickDup/internal/sourcepath"
)

// discoverScanFiles lists the files under folder whose extension matches,
// honoring the shared discovery contract used by every Quickdup scanner:
// Git-ignored files and directories are pruned (delegated to Git itself, so
// nested .gitignore files, negations, and the global excludesfile all apply),
// and dependency or build directories such as node_modules, dist, coverage,
// vendor, and .git are skipped even when they are not Git-ignored. In
// non-Git roots only the name-based skips apply, which keeps temp roots
// deterministic.
func discoverScanFiles(folder string, extension string, excludePatterns []string) ([]string, error) {
	root := folder
	if resolved, err := filepath.Abs(root); err == nil {
		root = resolved
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	relFiles, err := sourcepath.DiscoverScannerFiles(
		root,
		[]string{""},
		0,
		"quickdup",
		[]string{".quickdup"},
		nil,
		func(relPath, ext string) bool {
			if ext != strings.ToLower(extension) {
				return false
			}
			return !excludedByPatterns(relPath, excludePatterns)
		},
	)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(relFiles))
	for _, rel := range relFiles {
		files = append(files, filepath.Join(root, filepath.FromSlash(rel)))
	}
	return files, nil
}

// excludedByPatterns keeps the historical --exclude semantics: a pattern
// matches either as a glob against the base name or as a substring of the
// scanned-relative path (for directory patterns).
func excludedByPatterns(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, filepath.Base(relPath)); matched {
			return true
		}
		if strings.Contains(relPath, pattern) {
			return true
		}
	}
	return false
}
