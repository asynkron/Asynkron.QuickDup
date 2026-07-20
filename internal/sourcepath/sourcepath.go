package sourcepath

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var ErrUnsafePath = errors.New("unsafe source path")

func CleanRoot(repoRoot string) (string, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return "", errors.New("repository root unavailable")
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(root)
}

func CleanSourceDirs(root string, sourceDirs []string) ([]string, error) {
	out := make([]string, 0, len(sourceDirs))
	seen := map[string]bool{}
	for _, raw := range sourceDirs {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if strings.Contains(part, "\x00") || filepath.IsAbs(part) {
				return nil, fmt.Errorf("%w: %s", ErrUnsafePath, part)
			}
			clean := filepath.Clean(filepath.FromSlash(part))
			if clean == "." {
				clean = ""
			}
			if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
				return nil, fmt.Errorf("%w: %s", ErrUnsafePath, part)
			}
			full := filepath.Join(root, clean)
			resolved, err := filepath.EvalSymlinks(full)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			if resolved != root && !strings.HasPrefix(resolved, root+string(os.PathSeparator)) {
				return nil, fmt.Errorf("%w: %s", ErrUnsafePath, part)
			}
			rel, err := filepath.Rel(root, resolved)
			if err != nil {
				return nil, err
			}
			rel = filepath.ToSlash(rel)
			if rel == "." {
				rel = ""
			}
			if !seen[rel] {
				out = append(out, rel)
				seen[rel] = true
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

func IsTestFile(path, language string) bool {
	segments := strings.Split(filepath.ToSlash(path), "/")
	base := segments[len(segments)-1]
	lowerBase := strings.ToLower(base)
	lowerNoExt := strings.TrimSuffix(lowerBase, strings.ToLower(filepath.Ext(lowerBase)))
	noExt := strings.TrimSuffix(base, filepath.Ext(base))
	hasSegment := func(names ...string) bool {
		allowed := map[string]bool{}
		for _, name := range names {
			allowed[name] = true
		}
		for _, segment := range segments[:len(segments)-1] {
			if allowed[strings.ToLower(segment)] {
				return true
			}
		}
		return false
	}
	hasTestSuffix := func() bool {
		return strings.HasSuffix(lowerNoExt, "test") || strings.HasSuffix(lowerNoExt, "tests")
	}
	hasSpecSuffix := func() bool {
		return strings.HasSuffix(lowerNoExt, "spec") || strings.HasSuffix(lowerNoExt, "specs")
	}
	hasCaseSuffix := func(suffixes ...string) bool {
		for _, suffix := range suffixes {
			if strings.HasSuffix(noExt, suffix) && noExt != suffix {
				return true
			}
		}
		return false
	}
	hasUnderscoreTestName := func() bool {
		return strings.HasPrefix(lowerNoExt, "test_") ||
			strings.HasSuffix(lowerNoExt, "_test") ||
			strings.HasSuffix(lowerNoExt, "_tests")
	}

	switch language {
	case "go":
		return strings.HasSuffix(lowerBase, "_test.go")
	case "typescript", "tsx", "javascript":
		return hasSegment("__tests__", "__test__", "test", "tests") ||
			strings.Contains(lowerBase, ".test.") ||
			strings.Contains(lowerBase, ".spec.") ||
			strings.HasSuffix(lowerNoExt, "-test") ||
			strings.HasSuffix(lowerNoExt, "-spec")
	case "java", "csharp":
		return hasSegment("test", "tests") || hasTestSuffix()
	case "c", "cpp":
		return hasSegment("test", "tests") ||
			strings.HasPrefix(lowerNoExt, "test_") ||
			strings.HasSuffix(lowerNoExt, "_test") ||
			strings.HasSuffix(lowerNoExt, "_tests") ||
			hasTestSuffix()
	case "python":
		return hasSegment("test", "tests") || hasUnderscoreTestName()
	case "rust":
		return hasSegment("test", "tests") || hasUnderscoreTestName()
	case "kotlin":
		return hasSegment("test", "tests") ||
			hasCaseSuffix("Test", "Tests", "Spec", "Specs") ||
			strings.HasSuffix(lowerNoExt, "_test") ||
			strings.HasSuffix(lowerNoExt, "_tests") ||
			strings.HasSuffix(lowerNoExt, "_spec") ||
			strings.HasSuffix(lowerNoExt, "_specs")
	case "swift":
		return hasSegment("test", "tests") ||
			hasCaseSuffix("Test", "Tests") ||
			strings.HasSuffix(lowerNoExt, "_test") ||
			strings.HasSuffix(lowerNoExt, "_tests")
	case "ruby":
		return hasSegment("test", "tests", "spec", "specs") ||
			hasUnderscoreTestName() ||
			strings.HasSuffix(lowerNoExt, "_spec") ||
			hasSpecSuffix()
	case "shell", "sql":
		return hasSegment("test", "tests") || hasUnderscoreTestName()
	default:
		return false
	}
}

func DiscoverScannerFiles(root string, sourceDirs []string, maxFiles int, limitName string, extraSkipDirs []string, shouldSkipDir func(path, name string) bool, includeFile func(relPath, ext string) bool) ([]string, error) {
	var files []string
	if limitName == "" {
		limitName = "scanner"
	}
	// Build the gitignore matcher once per scan so every shared-discovery
	// caller (Codegraph, Quickdup, and any future tool) inherits the same
	// ignore contract. A nil matcher (non-Git root or nothing ignored) applies
	// no filtering, keeping non-Git temp roots deterministic.
	ignore := newGitIgnoreMatcher(root)
	for _, sourceDir := range sourceDirs {
		start := filepath.Join(root, filepath.FromSlash(sourceDir))
		if sourceDir == "" {
			start = root
		}
		err := filepath.WalkDir(start, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				if os.IsPermission(walkErr) {
					return nil
				}
				return walkErr
			}
			name := entry.Name()
			if entry.IsDir() {
				if path != start {
					rel, relErr := filepath.Rel(root, path)
					if relErr == nil && ignore.ignoredDir(filepath.ToSlash(rel)) {
						return filepath.SkipDir
					}
					if (shouldSkipDir != nil && shouldSkipDir(path, name)) ||
						ShouldSkipScannerDir(root, path, name, extraSkipDirs...) {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relPath := filepath.ToSlash(rel)
			if ignore.ignoredFile(relPath) {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(name))
			if includeFile != nil && !includeFile(relPath, ext) {
				return nil
			}
			files = append(files, relPath)
			if maxFiles > 0 && len(files) > maxFiles {
				return fmt.Errorf("%s file limit exceeded: %d", limitName, maxFiles)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}

func ShouldSkipScannerDir(root, path, name string, extraNames ...string) bool {
	for _, extraName := range extraNames {
		if name == extraName {
			return true
		}
	}
	rel, err := filepath.Rel(root, path)
	if err == nil {
		switch filepath.ToSlash(rel) {
		case "verticals/web/app", "packages/frontend/storybook-static":
			return true
		}
	}
	switch name {
	case ".git", ".faktorial", ".faktorial-go", "bin", "build", "coverage", "dist", "node_modules", "obj", "vendor":
		return true
	default:
		return strings.HasPrefix(name, ".")
	}
}
