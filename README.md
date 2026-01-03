# QuickDup - Delta-Indent Clone Detection

QuickDup is a structural code clone detector that identifies **Type-2 and Type-3 clones** using indent-delta fingerprinting. It's designed as a fast candidate generator for AI-assisted code review.

## Philosophy

Traditional clone detection tools optimize for **precision** — minimizing false positives so humans can act directly on results. This made sense when humans were the bottleneck.

In the AI era, the paradigm shifts:

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│  Fast Heuristic │ ──▶ │    AI Agent     │ ──▶ │  Human Decision │
│  (CodeClone)    │     │  (verification) │     │                 │
└─────────────────┘     └─────────────────┘     └─────────────────┘
   Candidates            Understanding           Action
   (noisy OK)            (filters + expands)     (final call)
```

CodeClone is a **candidate generator**. It doesn't need to be precise — it needs to surface interesting patterns quickly. An AI agent can then:

1. Read the actual code and verify semantic similarity
2. Understand *why* code is duplicated
3. Find related clones the fingerprint missed
4. Suggest concrete refactoring strategies
5. Implement the fix

**Speed and recall over precision.** Let the AI do the reasoning.

## Algorithm: Structural Shape Fingerprinting

CodeClone uses a novel lightweight approach we call **Delta-Indent Clone Detection**:

### Phase 1: Parse Files

For each source file, scan line by line and extract:

| Field | Description |
|-------|-------------|
| `LineNumber` | Actual line number in file (for navigation) |
| `IndentDelta` | Change in indentation from previous non-empty line |
| `Word` | First token on the line (keyword, brace, identifier) |

**Key design choices:**

- **Indent delta, not absolute indent** — Captures the *shape* of code (nesting depth changes)
- **Tabs = 4 spaces** — Normalized indentation
- **Whitespace-only lines skipped** — But line numbers preserved
- **First word only** — Lightweight, language-agnostic

Example input:
```go
func foo() {           // line 1
    if x {             // line 2
        return true    // line 3
    }                  // line 4
}                      // line 5
```

Parsed output:
```
Line 1:  delta=0   word="func"
Line 2:  delta=+4  word="if"
Line 3:  delta=+4  word="return"
Line 4:  delta=-4  word="}"
Line 5:  delta=-4  word="}"
```

### Phase 2: Pattern Detection

For each file's parsed entries:

1. Slide a window of size 2-10 lines over the entries
2. Hash each window: `hash(delta₁|word₁, delta₂|word₂, ...)`
3. Store hash → list of (filename, line number) locations

```
For i := 0 to len(entries):
    For windowSize := 2 to 10:
        window := entries[i : i+windowSize]
        hash := FNV64(window)
        patterns[hash].append(Location{file, line})
```

### Phase 3: Report Duplicates

Filter to patterns appearing N+ times (default: 3) and display:

```
Pattern [7 lines] found 5 times:
┌─────────────────────────────────────
│   0  var
│   0  while
│  +4  if
│  +4  return
│  -4  }
│   0  current
│  -4  }
└─────────────────────────────────────
Locations:
  • src/object.cs:249
  • src/proxy.cs:344
  • src/iterator.cs:154
```

## Why This Works

The combination of **indent delta + first keyword** captures:

- **Control flow structure** — `if`, `for`, `while`, `switch` patterns
- **Block nesting shape** — How deeply code nests and unnests
- **Common idioms** — Guard clauses, early returns, cleanup patterns

It's language-agnostic (works on any indented language) and requires no parsing.

## Usage

```bash
# Build
go build -o codeclone .

# Scan Go files in current directory
./codeclone --path . --ext .go --min 3

# Scan TypeScript files with higher threshold
./codeclone --path ./src --ext .ts --min 5

# Scan C# files
./codeclone --path ./src --ext .cs --min 3
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--path` | `.` | Directory to scan recursively |
| `--ext` | `.go` | File extension to match |
| `--min` | `3` | Minimum occurrences to report |

## Limitations

This is a **heuristic candidate generator**, not a precise clone detector:

- **False positives** — Common idioms (guard clauses, error handling) will match
- **False negatives** — Semantically identical code with different structure won't match
- **First word only** — `if x > 0` and `if y < 0` look identical

These are acceptable trade-offs when an AI agent handles verification.

## License

MIT
