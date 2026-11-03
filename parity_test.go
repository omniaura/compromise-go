package compromise

import (
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/omniaura/compromise-go/internal/golden"
)

var goldenFiles = append([]string{"testdata/golden/drafts.jsonl.gz", "testdata/golden/compromise-tests.jsonl.gz", "testdata/golden/adversarial.jsonl.gz"}, extraGoldens()...)

// extraGoldens lets a local run add held-out corpora that are not committed
// (PARITY_EXTRA=path1.jsonl.gz,path2.jsonl.gz, paths relative to the module root).
func extraGoldens() []string {
	if v := os.Getenv("PARITY_EXTRA"); v != "" {
		return strings.Split(v, ",")
	}
	return nil
}

func sortedCopy(s []string) []string {
	out := append([]string(nil), s...)
	sort.Strings(out)
	return out
}

type parityStats struct {
	docs, docDiffs, terms, termDiffs, tagDiffs, tagOrderDiffs, valueDiffs, toNumberDiffs int
}

// TestParity compares every term of every golden document with JS compromise:
// tokenisation (text, pre, post), normalisation (normal, implicit, machine),
// UTF-16 offsets and the tag set, plus values() and values().toNumber().
func TestParity(t *testing.T) {
	report := os.Getenv("PARITY_REPORT") != ""
	for _, file := range goldenFiles {
		rows, err := golden.Load(file)
		if err != nil {
			t.Fatal(err)
		}
		var st parityStats
		var examples []string
		addEx := func(s string) {
			if len(examples) < 40 {
				examples = append(examples, s)
			}
		}
		for _, row := range rows {
			st.docs++
			doc := New(row.Text)
			doc.computeOffsets()
			diff := false
			got := doc.Sentences()
			if len(got) != len(row.Sentences) {
				diff = true
				addEx(fmt.Sprintf("%q: %d sentences, want %d", row.Text, len(got), len(row.Sentences)))
			}
			for n := 0; n < len(got) && n < len(row.Sentences); n++ {
				gs, ws := got[n], row.Sentences[n]
				if len(gs) != len(ws) {
					diff = true
					var a, b []string
					for _, x := range gs {
						a = append(a, x.Text+"|"+x.Implicit)
					}
					for _, x := range ws {
						b = append(b, x.Text+"|"+x.Implicit)
					}
					addEx(fmt.Sprintf("%q: sentence %d terms %v, want %v", row.Text, n, a, b))
					continue
				}
				for i := range gs {
					st.terms++
					g, w := gs[i], ws[i]
					gt := []string{g.Text, g.Pre, g.Post, g.Normal, g.Implicit, g.Machine, fmt.Sprint(g.start)}
					wt := []string{w.Text, w.Pre, w.Post, w.Normal, w.Implicit, w.Machine, fmt.Sprint(w.Start)}
					if !reflect.DeepEqual(gt, wt) {
						diff = true
						st.termDiffs++
						addEx(fmt.Sprintf("%q: term %q fields %q, want %q", row.Text, w.Text, gt, wt))
					}
					gTags := g.Tags.List()
					if !reflect.DeepEqual(sortedCopy(gTags), sortedCopy(w.Tags)) {
						diff = true
						st.tagDiffs++
						addEx(fmt.Sprintf("%q: term %q tags %v, want %v", row.Text, w.Text, gTags, w.Tags))
					} else if !reflect.DeepEqual(gTags, w.Tags) {
						st.tagOrderDiffs++
					}
				}
			}
			vals := New(row.Text).All().Values().Out()
			if !equalStrings(vals, row.Values) {
				diff = true
				st.valueDiffs++
				addEx(fmt.Sprintf("%q: values %q, want %q", row.Text, vals, row.Values))
			}
			nums := New(row.Text).All().Values().ToNumber().Out()
			if !equalStrings(nums, row.ToNumber) {
				diff = true
				st.toNumberDiffs++
				addEx(fmt.Sprintf("%q: toNumber %q, want %q", row.Text, nums, row.ToNumber))
			}
			if diff {
				st.docDiffs++
			}
		}
		summary := fmt.Sprintf("%s: docs=%d docs_with_diff=%d terms=%d term_field_diffs=%d tag_set_diffs=%d tag_order_only=%d values_diffs=%d tonumber_diffs=%d",
			file, st.docs, st.docDiffs, st.terms, st.termDiffs, st.tagDiffs, st.tagOrderDiffs, st.valueDiffs, st.toNumberDiffs)
		if report {
			t.Log(summary)
			for _, e := range examples {
				t.Log("  " + e)
			}
		}
		if st.docDiffs > 0 {
			t.Errorf("%s\n%s", summary, strings.Join(examples, "\n"))
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}
