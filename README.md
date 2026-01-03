<img src="assets/images/logo.png" width="100%" />

# QuickDup - Delta-Indent Clone Detection

## The shape of code

QuickDup is a fast structural code clone detector that:
* Identifies duplicate code patterns using indent-delta fingerprinting.
* Is designed as a candidate generator for AI-assisted code review.

## Performance

- **~100k lines of code in ~500 ms** on 8 cores
- Parallel file parsing and pattern detection
- Lightweight fingerprinting (no AST parsing)

## Philosophy

Traditional clone detection optimizes for **precision** — minimizing false positives. QuickDup optimizes for **speed and recall** — surface candidates fast, let AI verify.

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│    QuickDup     │ ──▶ │    AI Agent     │ ──▶ │  Human Decision │
│  (candidates)   │     │  (verification) │     │                 │
└─────────────────┘     └─────────────────┘     └─────────────────┘
```

## Algorithm

### Phase 1: Parse Files (Parallel)

Extract structural fingerprint per line:

| Field | Description |
|-------|-------------|
| `IndentDelta` | Change in indentation from previous line |
| `Word` | First token on the line |
| `SourceLine` | Original source for output |

Comments and blank lines are skipped. Comment prefixes are auto-detected by file extension.

Example:
```go
func foo() {           // delta=0   word="func"
    if x {             // delta=+4  word="if"
        return true    // delta=+4  word="return"
    }                  // delta=-4  word="}"
}                      // delta=-4  word="}"
```

### Phase 2: Grow-Based Pattern Detection (Parallel)

1. Generate base patterns of minimum size (default: 3 lines)
2. Keep patterns with 3+ occurrences
3. Grow patterns by 1 line, repeat until no patterns survive
4. Track which occurrences grew vs. stopped (only report maximal patterns)

This finds the **longest** duplicate patterns, not just fixed windows.

### Phase 3: Token Similarity Filter

Patterns with similar structure but different actual code are filtered:

1. Tokenize source lines of each occurrence
2. Compute Jaccard similarity (intersection/union of token sets)
3. Filter patterns below threshold (default: 50%)

This eliminates false positives like "all error handlers look similar structurally but have different messages."

### Phase 4: Output

Results written to `.quickdup/` directory:
- `results.json` — Machine-readable patterns with locations
- `patterns.md` — Human-readable code snippets

## Installation

```bash
go install github.com/asynkron/Asynkron.QuickDup/cmd/quickdup@latest
```

## Usage

```bash
# Scan Go files in current directory
quickdup -path . -ext .go

# Scan C# files with stricter similarity threshold
quickdup -path ./src -ext .cs -min-similarity 0.7

# Show top 20 patterns, require 5+ occurrences
quickdup -path . -ext .ts -top 20 -min 5
```

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `-path` | `.` | Directory to scan recursively |
| `-ext` | `.go` | File extension to match |
| `-min` | `3` | Minimum occurrences to report |
| `-min-size` | `3` | Base pattern size (lines) to start growing from |
| `-min-score` | `3` | Minimum unique words in pattern |
| `-min-similarity` | `0.5` | Minimum token similarity between occurrences (0.0-1.0) |
| `-top` | `10` | Show top N patterns by score |
| `-comment` | auto | Override comment prefix (auto-detected by extension) |
| `-no-cache` | `false` | Disable incremental caching, force full re-parse |
| `-github-annotations` | `false` | Output GitHub Actions annotations for inline PR comments |

## GitHub Actions Integration

QuickDup can output annotations that GitHub displays as inline comments on pull requests:

```yaml
- name: Run QuickDup
  run: quickdup -path . -ext .go --github-annotations --no-cache
```

When `--github-annotations` is enabled, QuickDup outputs warnings in GitHub's annotation format and skips writing `.quickdup/results.json` and `patterns.md`.

## Incremental Caching

QuickDup caches parsed file data in `.quickdup/cache.gob`. On subsequent runs, only modified files are re-parsed:

```
Parsed 558 files (542 cached, 16 parsed) (98234 lines of code)
```

This dramatically speeds up repeated runs during development. Use `-no-cache` to force a full re-parse.

## Ignoring Patterns

Create `.quickdup/ignore.json` to suppress known patterns:

```json
{
  "description": "Patterns to ignore",
  "ignored": [
    "56c2f5f9b27ed5a0",
    "c32ca0ee344f8e23"
  ]
}
```

Pattern hashes are shown in the output for easy copy-paste.

## Supported Languages

Comment prefixes are auto-detected for:

- **C-style** (`//`): Go, C, C++, Java, JavaScript, TypeScript, C#, Swift, Kotlin, Rust, PHP, Dart, Zig
- **Hash** (`#`): Python, Ruby, Shell, Perl, R, YAML, TOML, PowerShell, Nim, Julia, Elixir
- **Double-dash** (`--`): SQL, Lua, Haskell, Elm, Ada, VHDL
- **Semicolon** (`;`): Lisp, Clojure, Scheme, Assembly
- **Percent** (`%`): LaTeX, MATLAB, Erlang, Prolog

Use `-comment` to override for unsupported extensions.

## Example Output

```
Scanning 558 files using 8 workers...
Parsed 558 files (98234 lines of code)
Detecting patterns...
Growth stopped at 148 lines
Filtered 23 low-similarity patterns (similarity < 50%)
Found 2410 patterns with 3+ occurrences (showing top 10 by score)

Score 15 [79 lines, 15 unique] found 3 times [a1b2c3d4e5f67890]:
  src/services/auth.go:142
  src/services/oauth.go:89
  src/services/saml.go:201

...

Total: 2410 duplicate patterns in 558 files (98234 lines) in ~500ms
```

## Limitations

This is a **heuristic candidate generator**:

- **False positives** — Structural similarity doesn't guarantee semantic duplication
- **False negatives** — Different structure with same semantics won't match
- **First word only** — `if x > 0` and `if y < 0` look identical

The token similarity filter catches most structural false positives. For the rest, let AI verify.

## License

MIT
