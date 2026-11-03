// Package faircopy ports Fair Copy's two compromise-backed writing rules,
// no-llm-speak and no-n-things (omniaura/faircopy packages/rules-nlp), onto
// the Go compromise port.
//
// The spans match the JavaScript rules exactly (that is what the parity
// tests check). Messages differ on purpose: the JS messages quote the draft,
// and a server-side review must never echo draft text back, so every message
// here is static.
package faircopy

import (
	"math"
	"sort"
	"strings"
	"unicode"

	compromise "github.com/omniaura/compromise-go"
)

// Diagnostic is one finding. Start/End are UTF-16 offsets (what the JS rule
// reports); ByteStart/ByteEnd index the Go string.
type Diagnostic struct {
	RuleID             string
	Message            string
	Start, End         int
	ByteStart, ByteEnd int
}

// LLMSpeakPhrase is a no-llm-speak entry.
type LLMSpeakPhrase struct {
	Phrase       string
	Alternatives []string
}

// DefaultLLMSpeakPhrases is the JS rule's DEFAULT_PHRASES, in order.
var DefaultLLMSpeakPhrases = []LLMSpeakPhrase{
	{"delve into", []string{"explore", "examine"}},
	{"it's important to note", []string{"note", "remember"}},
	{"it's worth noting", []string{"note"}},
	{"in the ever-evolving landscape", []string{"today", "now"}},
	{"in today's digital age", []string{"today"}},
	{"a myriad of", []string{"many"}},
	{"a wealth of", []string{"rich", "deep"}},
	{"in the realm of", []string{"in"}},
	{"at the forefront of", []string{"leading"}},
	{"navigate the complexities of", []string{"handle"}},
	{"sheds light on", []string{"explains", "clarifies"}},
	{"underscores", []string{"shows", "emphasizes"}},
	{"furthermore", []string{"also"}},
	{"moreover", []string{"also"}},
	{"in conclusion", []string{"so", "finally"}},
	{"it is interesting to note", []string{"note"}},
	{"it should be noted", []string{"note"}},
	{"it is worth noting", []string{"note"}},
	{"as mentioned earlier", nil},
	{"as noted above", nil},
	{"going forward", []string{"next", "later"}},
	{"moving forward", []string{"next"}},
	{"tapestry", []string{"pattern", "mix"}},
	{"multifaceted", []string{"complex", "many-sided"}},
	{"intricate", []string{"complex", "detailed"}},
	{"meticulous", []string{"careful", "precise"}},
	{"pivotal", []string{"key", "critical"}},
	{"crucial", []string{"critical", "key"}},
	{"robust", []string{"strong", "resilient"}},
}

// LLMSpeakOptions mirrors NoLlmSpeakOptions.
type LLMSpeakOptions struct {
	Phrases        []LLMSpeakPhrase
	AllowedPhrases []string
}

const llmSpeakMessage = "replace this LLM-speak phrase with a direct verb or concrete noun, or delete it"

// NoLLMSpeak runs no-llm-speak on a parsed document.
func NoLLMSpeak(doc *compromise.Doc, opts LLMSpeakOptions) []Diagnostic {
	phrases := opts.Phrases
	if len(phrases) == 0 {
		phrases = DefaultLLMSpeakPhrases
	}
	allowed := map[string]bool{}
	for _, p := range opts.AllowedPhrases {
		allowed[strings.ToLower(p)] = true
	}
	var out []Diagnostic
	for _, p := range phrases {
		if allowed[strings.ToLower(p.Phrase)] {
			continue
		}
		for _, s := range doc.Match(p.Phrase).Spans() {
			if s.End <= s.Start {
				continue
			}
			out = append(out, Diagnostic{
				RuleID: "no-llm-speak", Message: llmSpeakMessage,
				Start: s.Start, End: s.End, ByteStart: s.ByteStart, ByteEnd: s.ByteEnd,
			})
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Start < out[b].Start })
	return out
}

// NThingsOptions mirrors NoNThingsOptions.
type NThingsOptions struct {
	MinCount       int // default 2
	Nouns          []string
	AllowedPhrases []string
}

const nThingsMessage = "name the items instead of counting a placeholder noun; a counted placeholder is an AI-writing tell"

func normalizePhrase(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// escapeMatchWord is word.replace(/[^\w-]/g, ”).
func escapeMatchWord(w string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || (r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r))) {
			return r
		}
		return -1
	}, w)
}

// NoNThings runs no-n-things on a parsed document. Like the JS rule it calls
// values().toNumber(), which rewrites numbers in doc in place: parse a fresh
// Doc per rule.
func NoNThings(doc *compromise.Doc, text string, opts NThingsOptions) []Diagnostic {
	minCount := opts.MinCount
	if minCount == 0 {
		minCount = 2
	}
	allowed := map[string]bool{}
	for _, p := range opts.AllowedPhrases {
		allowed[normalizePhrase(p)] = true
	}
	nouns := opts.Nouns
	if len(nouns) == 0 {
		nouns = []string{"thing", "things"}
	}
	var clean []string
	for _, n := range nouns {
		if e := escapeMatchWord(n); e != "" {
			clean = append(clean, e)
		}
	}
	if len(clean) == 0 || text == "" {
		return nil
	}
	spans := doc.Match("#Value+ (" + strings.Join(clean, "|") + ")").Spans()
	var out []Diagnostic
	for _, s := range spans {
		if len(s.Terms) < 2 || s.End <= s.Start {
			continue
		}
		matched := text[s.ByteStart:s.ByteEnd]
		if allowed[normalizePhrase(matched)] {
			continue
		}
		// Only a cardinal count frames the trope; ordinals and vague values
		// come back from toNumber() as non-numbers.
		counts := doc.Match(matched).Values().ToNumber().Out()
		count := math.NaN()
		if len(counts) > 0 {
			count = compromise.JSNumber(counts[0])
		}
		if math.IsNaN(count) || math.IsInf(count, 0) || count < float64(minCount) {
			continue
		}
		out = append(out, Diagnostic{
			RuleID: "no-n-things", Message: nThingsMessage,
			Start: s.Start, End: s.End, ByteStart: s.ByteStart, ByteEnd: s.ByteEnd,
		})
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Start < out[b].Start })
	return out
}

// Review runs both rules the way the JS rules do: each on its own parse.
func Review(text string) []Diagnostic {
	out := NoLLMSpeak(compromise.New(text), LLMSpeakOptions{})
	out = append(out, NoNThings(compromise.New(text), text, NThingsOptions{})...)
	sort.SliceStable(out, func(a, b int) bool { return out[a].Start < out[b].Start })
	return out
}
