package quickdup

import (
	"os"
	"path/filepath"
	"strings"
)

// Separators for word extraction
const separators = " \t:.;{}()[]#!<>=,\n\r"

// skipFirstWords defines first-word tokens to skip by file extension
var skipFirstWords = map[string]map[string]bool{
	".cs": {
		"using": true,
		"#":     true, // #region, #endregion, #pragma, etc.
	},
	".go": {
		"import":  true,
		"package": true,
	},
	".java": {
		"import":  true,
		"package": true,
	},
	".ts": {
		"import": true,
		"export": true,
	},
	".tsx": {
		"import": true,
		"export": true,
	},
	".js": {
		"import": true,
		"export": true,
	},
	".jsx": {
		"import": true,
		"export": true,
	},
	".py": {
		"import": true,
		"from":   true,
	},
	".rs": {
		"use": true,
		"mod": true,
	},
	".kt": {
		"import":  true,
		"package": true,
	},
	".scala": {
		"import":  true,
		"package": true,
	},
}

// currentFileExt is set during parsing to track the current file's extension.
// Parsing runs concurrently, so it is pinned once per scan (every file in a
// scan shares one extension) via setCurrentFileExt instead of being written
// per file from inside the worker pool.
var currentFileExt string

func setCurrentFileExt(path string) {
	currentFileExt = strings.ToLower(filepath.Ext(path))
}

// importFirstWords lists the first-word tokens that begin an import
// statement per file extension. Import plumbing is identical across files
// by construction, so it must never become duplication evidence.
var importFirstWords = map[string]map[string]bool{
	".go":    {"import": true},
	".py":    {"import": true, "from": true},
	".ts":    {"import": true},
	".tsx":   {"import": true},
	".js":    {"import": true},
	".jsx":   {"import": true},
	".mjs":   {"import": true},
	".cjs":   {"import": true},
	".java":  {"import": true},
	".kt":    {"import": true},
	".scala": {"import": true},
	".cs":    {"using": true},
	".rs":    {"use": true},
}

// skipImportLine reports whether the line belongs to an import statement,
// including multi-line blocks such as Go's `import (` or TypeScript's
// `import {` ... `} from '...'`. inBlock carries the block state across
// successive lines of one file.
func skipImportLine(line string, keywords map[string]bool, inBlock *bool) bool {
	if len(keywords) == 0 {
		return false
	}
	trimmed := strings.TrimSpace(line)
	if *inBlock {
		if strings.HasPrefix(trimmed, ")") || strings.HasPrefix(trimmed, "}") || strings.HasPrefix(trimmed, "]") {
			*inBlock = false
		}
		return true
	}
	if !keywords[extractFirstWord(line)] {
		return false
	}
	if strings.HasSuffix(trimmed, "(") || strings.HasSuffix(trimmed, "{") || strings.HasSuffix(trimmed, "[") {
		*inBlock = true
	}
	return true
}

func parseFile(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := ActiveStrategy.Preparse(string(data))
	lines := strings.Split(content, "\n")

	keywords := importFirstWords[strings.ToLower(filepath.Ext(path))]
	inImportBlock := false

	var entries []Entry
	var prevEntry Entry

	for lineNumber, line := range lines {
		lineNumber++ // 1-based line numbers

		if skipImportLine(line, keywords, &inImportBlock) {
			continue
		}

		entry, skip := ActiveStrategy.ParseLine(lineNumber, line, prevEntry)
		if skip {
			continue
		}

		prevEntry = entry
		entries = append(entries, entry)
	}

	return entries, nil
}

func isWhitespaceOnly(line string) bool {
	for _, r := range line {
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}

func isCommentOnly(line string) bool {
	if CommentPrefix == "" {
		return false
	}
	trimmed := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(trimmed, CommentPrefix)
}

// shouldSkipByFirstWord checks if the line should be skipped based on its first word
func shouldSkipByFirstWord(line string) bool {
	skipWords := skipFirstWords[currentFileExt]
	if skipWords == nil {
		return false
	}

	word := extractFirstWord(line)
	return skipWords[word]
}

func calculateIndent(line string) int {
	indent := 0
	for _, r := range line {
		switch r {
		case ' ':
			indent++
		case '\t':
			indent += 4
		default:
			return indent
		}
	}
	return indent
}

func extractFirstWord(line string) string {
	// Skip leading whitespace
	start := 0
	for i, r := range line {
		if r != ' ' && r != '\t' {
			start = i
			break
		}
	}

	// Find end of word (first separator)
	trimmed := line[start:]
	end := len(trimmed)
	for i, r := range trimmed {
		if strings.ContainsRune(separators, r) {
			end = i
			break
		}
	}

	// If no word found (line starts with separator), use the first character
	if end == 0 && len(trimmed) > 0 {
		return string(trimmed[0])
	}

	return trimmed[:end]
}
