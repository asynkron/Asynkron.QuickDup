package main

import (
	"flag"
	"testing"
)

func TestMinScoreHelpText(t *testing.T) {
	fs := flag.NewFlagSet("quickdup", flag.ContinueOnError)
	registerFlags(fs)

	minScoreFlag := fs.Lookup("min-score")
	if minScoreFlag == nil {
		t.Fatal("min-score flag not registered")
	}

	want := "Minimum score to report (strategy-specific score)"
	if minScoreFlag.Usage != want {
		t.Fatalf("min-score help text = %q, want %q", minScoreFlag.Usage, want)
	}
}
