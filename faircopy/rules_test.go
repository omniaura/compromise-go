package faircopy

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	compromise "github.com/omniaura/compromise-go"
	"github.com/omniaura/compromise-go/internal/golden"
)

func spans(ds []Diagnostic) [][2]int {
	out := [][2]int{}
	for _, d := range ds {
		out = append(out, [2]int{d.Start, d.End})
	}
	return out
}

func norm(s [][2]int) [][2]int {
	if s == nil {
		return [][2]int{}
	}
	return s
}

// TestRuleParity checks that both rules report the same spans as the JS
// rules (@faircopy/rules-nlp 1.26.0) on every golden document.
func TestRuleParity(t *testing.T) {
	files := []string{"../testdata/golden/drafts.jsonl.gz", "../testdata/golden/compromise-tests.jsonl.gz", "../testdata/golden/adversarial.jsonl.gz"}
	if v := os.Getenv("PARITY_EXTRA"); v != "" {
		for _, f := range strings.Split(v, ",") {
			files = append(files, "../"+f)
		}
	}
	for _, file := range files {
		rows, err := golden.Load(file)
		if err != nil {
			t.Fatal(err)
		}
		var bad []string
		hits := 0
		for _, row := range rows {
			llm := spans(NoLLMSpeak(compromise.New(row.Text), LLMSpeakOptions{}))
			nt := spans(NoNThings(compromise.New(row.Text), row.Text, NThingsOptions{}))
			hits += len(llm) + len(nt)
			if !reflect.DeepEqual(llm, norm(row.LLM)) {
				bad = append(bad, fmt.Sprintf("no-llm-speak %q: %v, want %v", row.Text, llm, row.LLM))
			}
			if !reflect.DeepEqual(nt, norm(row.NThings)) {
				bad = append(bad, fmt.Sprintf("no-n-things %q: %v, want %v", row.Text, nt, row.NThings))
			}
		}
		t.Logf("%s: %d docs, %d diagnostics, %d mismatches", file, len(rows), hits, len(bad))
		if len(bad) > 0 {
			if len(bad) > 30 {
				bad = bad[:30]
			}
			t.Errorf("rule parity:\n%s", strings.Join(bad, "\n"))
		}
	}
}

// TestMessagesAreStatic: no message may contain draft text.
func TestMessagesAreStatic(t *testing.T) {
	text := "It's important to note the quokka. We found seven things."
	for _, d := range Review(text) {
		if strings.Contains(strings.ToLower(d.Message), "quokka") || strings.Contains(d.Message, "seven") || strings.Contains(d.Message, "important to note") {
			t.Fatalf("message echoes draft text: %q", d.Message)
		}
	}
	if len(Review(text)) != 2 {
		t.Fatalf("want 2 diagnostics, got %+v", Review(text))
	}
}
