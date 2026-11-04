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

// TestRuleParity checks no-llm-speak against the JS rule
// (@faircopy/rules-nlp 1.26.0) and no-n-things against the corrected JS rule
// in goldens.mjs, on every golden document. It also counts the documents
// where the correction changes the JS rule's answer.
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
		hits, changed := 0, 0
		for _, row := range rows {
			llm := spans(NoLLMSpeak(compromise.New(row.Text), LLMSpeakOptions{}))
			nt := spans(NoNThings(compromise.New(row.Text), row.Text, NThingsOptions{}))
			hits += len(llm) + len(nt)
			if !reflect.DeepEqual(llm, norm(row.LLM)) {
				bad = append(bad, fmt.Sprintf("no-llm-speak %q: %v, want %v", row.Text, llm, row.LLM))
			}
			if !reflect.DeepEqual(nt, norm(row.NThingsFixed)) {
				bad = append(bad, fmt.Sprintf("no-n-things %q: %v, want %v", row.Text, nt, row.NThingsFixed))
			}
			if !reflect.DeepEqual(norm(row.NThings), norm(row.NThingsFixed)) {
				changed++
			}
		}
		t.Logf("%s: %d docs, %d diagnostics, %d mismatches, %d docs where the no-n-things fix changes the JS result", file, len(rows), hits, len(bad), changed)
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

// TestNThingsCountsEveryOccurrence pins the fix: the JS rule reports the
// first "three things" only, and reads "one, two, three things" as 1.
func TestNThingsCountsEveryOccurrence(t *testing.T) {
	text := "We found three things. Then three things again: one, two, three things. Also (5) things and first things first."
	doc := compromise.New(text)
	got := NoNThings(doc, text, NThingsOptions{})
	var spans []string
	for _, d := range got {
		spans = append(spans, text[d.ByteStart:d.ByteEnd])
	}
	want := []string{"three things", "three things", "one, two, three things", "(5) things"}
	if !reflect.DeepEqual(spans, want) {
		t.Fatalf("spans %q, want %q", spans, want)
	}
	if doc.Text() != text {
		t.Fatalf("NoNThings edited the document: %q", doc.Text())
	}
}
