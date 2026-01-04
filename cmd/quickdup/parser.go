package main

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
		"using":  true,
		"#":      true, // #region, #endregion, #pragma, etc.
	},
	".go": {
		"import": true,
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

// currentFileExt is set during parsing to track the current file's extension
var currentFileExt string

func parseFile(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Set current file extension for skip word checking
	currentFileExt = strings.ToLower(filepath.Ext(path))

	content := activeStrategy.Preparse(string(data))
	lines := strings.Split(content, "\n")

	var entries []Entry
	var prevEntry Entry

	for lineNumber, line := range lines {
		lineNumber++ // 1-based line numbers

		entry, skip := activeStrategy.ParseLine(lineNumber, line, prevEntry)
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
	if commentPrefix == "" {
		return false
	}
	trimmed := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(trimmed, commentPrefix)
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

