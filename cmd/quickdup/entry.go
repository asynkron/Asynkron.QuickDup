package main

// Entry represents a parsed line for pattern detection
type Entry interface {
	GetLineNumber() int
	GetRaw() string
	HashBytes() []byte // pre-computed hash contribution for this entry
}
