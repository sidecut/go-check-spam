package reporter

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func captureOutput(fn func()) string {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()
	_ = w.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestPrintSpamSummary(t *testing.T) {
	counts := map[string]int{
		"2024-01-01": 5,
		"2024-01-02": 3,
		"2024-01-03": 7,
	}

	out := captureOutput(func() {
		PrintSpamSummary(counts, "2024-01-02")
	})

	expected := fmt.Sprintf(
		"Mon 2024-01-01 5\n" +
			"\n" +
			"Tue 2024-01-02 3\n" +
			"Wed 2024-01-03 7\n" +
			"Total: 15\n")

	if out != expected {
		t.Fatalf("unexpected output:\n%s\nexpected:\n%s", out, expected)
	}
}

func TestPrintSpamSummary_NoBeforeCutoff(t *testing.T) {
	counts := map[string]int{
		"2024-01-02": 3,
		"2024-01-03": 7,
	}

	out := captureOutput(func() {
		PrintSpamSummary(counts, "2024-01-01")
	})

	if strings.Contains(out, "\n\n") {
		t.Fatalf("expected no blank line when all dates are after cutoff, got:\n%s", out)
	}

	expectedTotal := "Total: 10\n"
	if !strings.HasSuffix(out, expectedTotal) {
		t.Fatalf("expected output to end with %q, got:\n%s", expectedTotal, out)
	}
}

func TestPrintSpamSummary_Empty(t *testing.T) {
	out := captureOutput(func() {
		PrintSpamSummary(map[string]int{}, "2024-01-01")
	})

	if out != "Total: 0\n" {
		t.Fatalf("expected 'Total: 0\\n', got %q", out)
	}
}
