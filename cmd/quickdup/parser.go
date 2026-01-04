package main

import (
	"os"
	"strings"
)

// Separators for word extraction
const separators = " \t:.;{}()[]#!<>=,\n\r"

func parseFile(path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	content := defaultStrategy.Preparse(string(data))
	lines := strings.Split(content, "\n")

	var entries []Entry
	var prevEntry Entry

	for lineNumber, line := range lines {
		lineNumber++ // 1-based line numbers

		entry, skip := defaultStrategy.ParseLine(lineNumber, line, prevEntry)
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

