package quickdup

import (
	"fmt"
	"io"
)

// ActiveStrategy is the detection strategy used by parsing and pattern
// growth. Callers must set it before calling ParseFile-driven APIs
// (ParseFilesWithCache, DetectPatterns, FilterPatterns's blocked-hash lookup).
var ActiveStrategy Strategy = &NormalizedIndentStrategy{}

// CommentPrefix is the line-comment prefix used to skip comment-only lines
// while parsing. Callers should set it per file extension before parsing.
var CommentPrefix string

// Debug gates "[debug]" progress lines emitted during pattern detection.
var Debug bool

// ProgressOutput receives progress lines emitted during scanning (both
// unconditional phase-transition messages and, when Debug is set,
// "[debug]" lines). The zero value discards all output.
var ProgressOutput io.Writer = io.Discard

func progressf(format string, args ...any) {
	if ProgressOutput == nil {
		return
	}
	_, _ = fmt.Fprintf(ProgressOutput, format, args...)
}
