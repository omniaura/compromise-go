package faircopy

import (
	"os"
	"strings"
	"testing"

	compromise "github.com/omniaura/compromise-go"
)

// draft builds a deterministic n-byte draft from the corpus paragraphs, cut
// at a word boundary. It is the same text for every run.
func draft(tb testing.TB, n int) string {
	raw, err := os.ReadFile("../testdata/corpus/drafts.txt")
	if err != nil {
		tb.Fatal(err)
	}
	paras := strings.Split(string(raw), "\n---\n")
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.TrimSpace(paras[i%len(paras)]))
	}
	s := strings.ToValidUTF8(b.String()[:n], "")
	if i := strings.LastIndexByte(s, ' '); i > n-40 {
		s = s[:i]
	}
	return s
}

var sizes = []struct {
	name string
	n    int
}{{"1KB", 1 << 10}, {"8KB", 8 << 10}, {"16KB", 16 << 10}}

// BenchmarkReview is what a server-side review costs: both rules, each on
// its own parse (as the JS rules do).
func BenchmarkReview(b *testing.B) {
	for _, s := range sizes {
		text := draft(b, s.n)
		b.Run(s.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(text)))
			for i := 0; i < b.N; i++ {
				Review(text)
			}
		})
	}
}

// BenchmarkParse is compromise.New alone: tokenise, contractions and tagging.
func BenchmarkParse(b *testing.B) {
	for _, s := range sizes {
		text := draft(b, s.n)
		b.Run(s.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(text)))
			for i := 0; i < b.N; i++ {
				compromise.New(text)
			}
		})
	}
}

// BenchmarkReviewSharedParse runs both rules on one parse (no-llm-speak
// first, since no-n-things rewrites numbers in place).
func BenchmarkReviewSharedParse(b *testing.B) {
	for _, s := range sizes {
		text := draft(b, s.n)
		b.Run(s.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(text)))
			for i := 0; i < b.N; i++ {
				doc := compromise.New(text)
				NoLLMSpeak(doc, LLMSpeakOptions{})
				NoNThings(doc, text, NThingsOptions{})
			}
		})
	}
}
