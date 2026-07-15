package quickdup

import "testing"

func TestFirstWordStrategiesShareParsingWithoutLosingSemantics(t *testing.T) {
	CommentPrefix = "//"
	currentFileExt = ".cs"

	wordOnly := &WordOnlyStrategy{}
	inlineable := &InlineableStrategy{}
	if wordOnly.Name() != "word-only" {
		t.Fatalf("word-only name = %q", wordOnly.Name())
	}
	if inlineable.Name() != "inlineable" {
		t.Fatalf("inlineable name = %q", inlineable.Name())
	}

	wordEntry, skip := wordOnly.ParseLine(12, "\tif (ready) {", nil)
	if skip {
		t.Fatal("word-only ParseLine skipped a source line")
	}
	inlineEntry, skip := inlineable.ParseLine(12, "\tif (ready) {", nil)
	if skip {
		t.Fatal("inlineable ParseLine skipped a source line")
	}
	if wordEntry.GetLineNumber() != 12 || wordEntry.GetRaw() != "\tif (ready) {" {
		t.Fatalf("word-only entry lost source metadata: %+v", wordEntry)
	}
	if string(wordEntry.HashBytes()) != string(inlineEntry.HashBytes()) || wordOnly.Signature([]Entry{wordEntry}) != inlineable.Signature([]Entry{inlineEntry}) {
		t.Fatalf("shared first-word mechanics diverged: word=%q inline=%q", wordOnly.Signature([]Entry{wordEntry}), inlineable.Signature([]Entry{inlineEntry}))
	}

	blocked := wordOnly.BlockedHashes()
	if !blocked[wordOnly.Hash([]Entry{NewWordOnlyEntry("}"), NewWordOnlyEntry("}")})] {
		t.Fatal("word-only blocked hash pattern was not preserved")
	}

	inlineablePattern := []Entry{
		NewFirstWordEntry("public"),
		NewFirstWordEntry("{"),
		NewFirstWordEntry("return"),
		NewFirstWordEntry("}"),
	}
	if score := inlineable.Score(inlineablePattern, 1.0); score != 100 {
		t.Fatalf("inlineable score = %d, want 100", score)
	}
}

func TestIndentStrategiesShareMechanicsWithoutLosingDeltaSemantics(t *testing.T) {
	wordIndent := &WordIndentStrategy{}
	normalized := &NormalizedIndentStrategy{}
	wordEntry1, _ := wordIndent.ParseLine(7, "if ready {", nil)
	wordEntry2, _ := wordIndent.ParseLine(8, "\t\treturn value", wordEntry1)
	normalizedEntry1, _ := normalized.ParseLine(7, "if ready {", nil)
	normalizedEntry2, _ := normalized.ParseLine(8, "\t\treturn value", normalizedEntry1)

	wordPattern := []Entry{wordEntry1, wordEntry2}
	normalizedPattern := []Entry{normalizedEntry1, normalizedEntry2}
	if wordEntry2.(*WordIndentEntry).IndentDelta != 8 || normalizedEntry2.(*NormalizedIndentEntry).IndentDelta != 1 {
		t.Fatalf("delta semantics drifted: %d %d", wordEntry2.(*WordIndentEntry).IndentDelta, normalizedEntry2.(*NormalizedIndentEntry).IndentDelta)
	}
	if string(wordEntry2.HashBytes()) != "8|return\n" || string(normalizedEntry2.HashBytes()) != "1|return\n" ||
		wordIndent.Signature(wordPattern) != "if return" || normalized.Signature(normalizedPattern) != "if return" ||
		wordIndent.Score(wordPattern, 1) != 0 || normalized.Score(normalizedPattern, 1) != 1 ||
		wordIndent.Complexity(wordPattern) != 1 || normalized.Complexity(normalizedPattern) != 1 ||
		!wordIndent.BlockedHashes()[wordIndent.Hash([]Entry{NewWordIndentEntry(-4, "}"), NewWordIndentEntry(-4, "}")})] ||
		!normalized.BlockedHashes()[normalized.Hash([]Entry{NewNormalizedIndentEntry(-1, "}"), NewNormalizedIndentEntry(-1, "}")})] {
		t.Fatal("shared indent strategy mechanics drifted")
	}
}
