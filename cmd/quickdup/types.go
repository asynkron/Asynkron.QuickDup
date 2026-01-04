package main

// PatternLocation represents a location where a pattern was found
type PatternLocation struct {
	Filename   string
	LineStart  int
	EntryIndex int     // start position in entries array
	Pattern    []Entry // the actual pattern at this location
}

// PatternMatch represents a matched pattern with all its occurrences
type PatternMatch struct {
	Hash        uint64
	Locations   []PatternLocation
	Pattern     []Entry // representative pattern (first occurrence)
	UniqueWords int     // number of unique words in pattern
	Similarity  float64 // average token similarity across occurrences (0.0-1.0)
	Score       int     // combined score: uniqueWords + similarity bonus
}

// JSON output structures

type JSONLocation struct {
	Filename  string `json:"filename"`
	LineStart int    `json:"line_start"`
}

type JSONPattern struct {
	Hash        string         `json:"hash"`
	Score       int            `json:"score"`
	Lines       int            `json:"lines"`
	UniqueWords int            `json:"unique_words"`
	Similarity  float64        `json:"similarity"`
	Occurrences int            `json:"occurrences"`
	Pattern     []string       `json:"pattern"`
	Locations   []JSONLocation `json:"locations"`
}

type JSONOutput struct {
	TotalPatterns int           `json:"total_patterns"`
	Patterns      []JSONPattern `json:"patterns"`
}

// IgnoreFile represents the structure of ignore.json
type IgnoreFile struct {
	Description string   `json:"description"`
	Ignored     []string `json:"ignored"`
}

// OccurrenceKey uniquely identifies an occurrence by file and position
type OccurrenceKey struct {
	Filename   string
	EntryIndex int
}
