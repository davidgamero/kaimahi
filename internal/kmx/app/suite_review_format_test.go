package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSuiteReviewNativeSelectionAndReadableBlocks(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	a, p, _ := quickstartSuiteApplyFixture(t)
	r, err := a.reviewSuiteWorkspace(t.Context(), p.Workspace)
	if err != nil {
		t.Fatal(err)
	}
	m := &suiteConsole{review: r, width: 100, height: 32}
	if m.View().MouseMode != tea.MouseModeNone {
		t.Fatal("source review captures drag instead of terminal selection")
	}
	m.Update(tea.MouseClickMsg{X: 10, Y: 10, Button: tea.MouseLeft})
	if m.page != 0 || m.scroll != 0 {
		t.Fatal("review selection click changed page")
	}
	for _, page := range []int{0, 1} {
		m.page = page
		_, rows := m.reviewText()
		for _, row := range rows {
			if ansi.StringWidth(row) > 84 {
				t.Fatalf("review overflow %q", row)
			}
			if row != ansi.Strip(row) {
				t.Fatal("NO_COLOR review has escape codes")
			}
		}
	}
	block := strings.Join(reviewBlock("# Heading\n  nested text\nnext", 40, 2), "\n")
	if block != "  # Heading\n    nested text\n  next" {
		t.Fatalf("multiline indentation lost: %q", block)
	}
	if strings.Contains(strings.Join(reviewBlock("safe\x1b[2Jtext", 40, 2), "\n"), "\x1b") {
		t.Fatal("authored escape sequence leaked")
	}
	m.review = nil
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("mouse reporting not restored after review")
	}
}
