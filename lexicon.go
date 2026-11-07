package compromise

import (
	"regexp"
	"strings"
)

// Port of compromise/src/1-one/freeze and src/1-one/lexicon/compute.

func joinMachine(words []*Term) string {
	parts := make([]string, len(words))
	for i, t := range words {
		parts[i] = t.machineOrNormal()
	}
	return strings.Join(parts, " ")
}

func (m *model) freeze(docs [][]*Term) {
	for _, terms := range docs {
		for i := 0; i < len(terms); i++ {
			t := terms[i]
			word := t.machineOrNormal()
			if count, ok := m.multiCache[word]; ok && i+1 < len(terms) {
				end := i + count - 1
				for k := end; k > i; k-- {
					hi := k + 1 // terms.slice(i, k + 1) clamps to the sentence
					if hi > len(terms) {
						hi = len(terms)
					}
					words := terms[i:hi]
					if tags, ok := m.frozenLex[joinMachine(words)]; ok {
						m.setTags(words, tags, false)
						for _, w := range words {
							w.Frozen = true
						}
					}
				}
			}
			if tags, ok := m.frozenLex[word]; ok {
				m.setTags([]*Term{t}, tags, false)
				t.Frozen = true
			}
		}
	}
}

func unfreeze(docs [][]*Term) {
	for _, terms := range docs {
		for _, t := range terms {
			t.Frozen = false
		}
	}
}

var (
	reLexPrefix      = regexp.MustCompile(`^(under|over|mis|re|un|dis|semi|pre|post)-?`)
	lexAllowedPrefix = map[string]bool{"Verb": true, "Infinitive": true, "PastTense": true, "Gerund": true, "PresentTense": true, "Adjective": true, "Participle": true}
)

func (m *model) lexiconTag(docs [][]*Term) {
	for _, terms := range docs {
		for i := 0; i < len(terms); i++ {
			if terms[i].Tags.Size() != 0 {
				continue
			}
			// JS: found = found || multiWord(); found = found || singleWord()
			if m.multiWord(terms, i) != 1 {
				m.singleWord(terms, i)
			}
		}
	}
}

// multiWord returns 1 when tagged, -1 when it looked and failed, 0 when the
// word starts no multi-word entry (JS true/false/null).
func (m *model) multiWord(terms []*Term, start int) int {
	t := terms[start]
	word := t.machineOrNormal()
	count, ok := m.multiCache[word]
	if !ok || start+1 >= len(terms) {
		return 0
	}
	end := start + count - 1
	for i := end; i > start; i-- {
		hi := i + 1
		if hi > len(terms) {
			hi = len(terms)
		}
		words := terms[start:hi]
		if len(words) <= 1 {
			return -1
		}
		str := joinMachine(words)
		if tags, ok := m.lexicon[str]; ok {
			m.setTags(words, tags, false)
			if len(tags) == 2 && (tags[0] == "PhrasalVerb" || tags[1] == "PhrasalVerb") {
				m.setTag(words[1:2], "Particle", false)
			}
			return 1
		}
	}
	return -1
}

func (m *model) singleWord(terms []*Term, i int) {
	t := terms[i]
	word := t.machineOrNormal()
	if tags, ok := m.lexicon[word]; ok {
		m.setTags([]*Term{t}, tags, false)
		return
	}
	for _, a := range t.Alias {
		if tags, ok := m.lexicon[a]; ok {
			m.setTags([]*Term{t}, tags, false)
			return
		}
	}
	if reLexPrefix.MatchString(word) {
		stem := reLexPrefix.ReplaceAllString(word, "")
		if tags, ok := m.lexicon[stem]; ok && jsLen(stem) > 3 {
			if len(tags) == 1 && lexAllowedPrefix[tags[0]] {
				m.setTags([]*Term{t}, tags, false)
			}
		}
	}
}
