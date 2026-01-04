package main

// Strategy defines how patterns are detected and scored
type Strategy interface {
	Name() string
	Preparse(content string) string
	ParseLine(lineNum int, line string, prevEntry Entry) (Entry, bool) // returns entry and whether to skip
	Hash(entries []Entry) uint64
	Signature(entries []Entry) string
	Score(entries []Entry, similarity float64) int
}

// Preparser transforms file content before parsing
type Preparser interface {
	Preparse(content string) string
}
