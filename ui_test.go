package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFitLineFlattensEmbeddedNewlines(t *testing.T) {
	got := fitLine("alpha\nbeta\rgamma", 80)
	if strings.ContainsAny(got, "\r\n") {
		t.Fatalf("fitLine returned embedded newline characters: %q", got)
	}
}

func TestFitFrameReservesWrapColumn(t *testing.T) {
	got := fitFrame(strings.Repeat("x", 30)+"\n"+strings.Repeat("y", 30)+"\nz", 10, 2)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 frame lines, got %d: %q", len(lines), got)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w > 9 {
			t.Fatalf("line %d width = %d, want <= 9: %q", i, w, line)
		}
	}
}
