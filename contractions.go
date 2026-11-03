package compromise

import (
	"regexp"
	"strings"
)

// Port of compromise/src/1-one/contraction-one and src/2-two/contraction-two.

// splitApostrophe is normal.split(/'/) reduced to [0] and [1]; hasAfter is
// false when there is no apostrophe (JS leaves `after` null).
func splitApostrophe(normal string) (before, after string, has bool) {
	i := strings.IndexByte(normal, '\'')
	if i < 0 {
		return "", "", false
	}
	before = normal[:i]
	rest := normal[i+1:]
	if j := strings.IndexByte(rest, '\''); j >= 0 {
		rest = rest[:j]
	}
	return before, rest, true
}

// toDocs is view.fromText(words.join(' ')).docs[0]: fromText tokenises and
// then runs freeze, lexicon and preTagger on the fragment by itself.
func (m *model) toDocs(words []string, alias bool) []*Term {
	doc := m.fromText(strings.Join(words, " "))
	if len(doc) == 0 {
		return nil
	}
	terms := doc[0]
	if alias {
		for _, t := range terms {
			computeAlias(t, m)
		}
	}
	return terms
}

// fromText is View.fromText: a tagged fragment, without machine forms,
// aliases or sentence indexes (checkCase then sees every term as first).
func (m *model) fromText(str string) [][]*Term {
	doc := fromString(str, m)
	m.freeze(doc)
	m.lexiconTag(doc)
	m.preTagger(doc)
	unfreeze(doc)
	return doc
}

// splice replaces terms[w] in sentence n with the expansion words.
func splice(document [][]*Term, n, w int, words []*Term) {
	if len(words) == 0 {
		return
	}
	orig := document[n][w]
	for i, word := range words {
		word.Implicit = word.Text
		word.Machine = word.Text
		word.Pre = ""
		word.Post = ""
		word.Text = ""
		word.Normal = ""
		word.Index = [2]int{n, w + i}
	}
	words[0].Pre = orig.Pre
	words[len(words)-1].Post = orig.Post
	words[0].Text = orig.Text
	words[0].Normal = orig.Normal
	terms := document[n]
	out := make([]*Term, 0, len(terms)+len(words)-1)
	out = append(out, terms[:w]...)
	out = append(out, words...)
	out = append(out, terms[w+1:]...)
	document[n] = out
}

// --- contraction-one --------------------------------------------------------

var (
	alwaysDid = map[string]bool{"what": true, "how": true, "when": true, "where": true, "why": true}
	useWould  = map[string]bool{"be": true, "go": true, "start": true, "think": true, "need": true}
	useHad    = map[string]bool{"been": true, "gone": true}

	reNumDash   = regexp.MustCompile(`^[0-9][^-–—]*[-–—].*?[0-9]`)
	reIsRange   = regexp.MustCompile(`(?i)^([0-9.]{1,4}[a-z]{0,2}) ?[-–—] ?([0-9]{1,4}[a-z]{0,2})$`)
	reTimeRange = regexp.MustCompile(`(?i)^([0-9]{1,2}(:[0-9][0-9])?(am|pm)?) ?[-–—] ?([0-9]{1,2}(:[0-9][0-9])?(am|pm)?)$`)
	rePhoneNum  = regexp.MustCompile(`^[0-9]{3}-[0-9]{4}$`)
	reNumUnit   = regexp.MustCompile(`^([+-]?[0-9][.,0-9]*)([a-z°²³µ/]+)$`)

	reFeminine  = regexp.MustCompile(`(e|é|aison|sion|tion)$`)
	reMasculine = regexp.MustCompile(`(age|isme|acle|ege|oire)$`)
)

func (m *model) knownContraction(t *Term, before, after string, has bool) []string {
	for _, o := range m.contractions {
		if o.Word != nil && *o.Word == t.Normal {
			return o.Out
		} else if has && o.After != nil && after == *o.After {
			return append([]string{before}, o.Out...)
		} else if has && o.Before != nil && before == *o.Before && after != "" && jsLen(after) > 2 {
			return append(append([]string{}, o.Out...), after)
		}
	}
	return nil
}

func apostropheD1(terms []*Term, i int) []string {
	before, _, _ := splitApostrophe(terms[i].Normal)
	if alwaysDid[before] {
		return []string{before, "did"}
	}
	if i+1 < len(terms) {
		if useHad[terms[i+1].Normal] {
			return []string{before, "had"}
		}
		if useWould[terms[i+1].Normal] {
			return []string{before, "would"}
		}
	}
	return nil
}

func apostropheT1(terms []*Term, i int) []string {
	n := terms[i].Normal
	if n == "ain't" || n == "aint" {
		return nil
	}
	return []string{strings.Replace(n, "n't", "", 1), "not"}
}

func frenchPre(prefix string, terms []*Term, i int) []string {
	_, after, _ := splitApostrophe(terms[i].Normal)
	switch prefix {
	case "j":
		return []string{"je", after}
	case "l":
		if after != "" && strings.HasSuffix(after, "e") {
			return []string{"la", after}
		}
		return []string{"le", after}
	default: // d
		if after != "" && reFeminine.MatchString(after) && !reMasculine.MatchString(after) {
			return []string{"du", after}
		} else if after != "" && strings.HasSuffix(after, "s") {
			return []string{"des", after}
		}
		return []string{"de", after}
	}
}

func thereHas(terms []*Term, i int) []string {
	for k := i + 1; k < 5; k++ {
		if k >= len(terms) {
			break
		}
		if terms[k].Normal == "been" {
			return []string{"there", "has"}
		}
	}
	return []string{"there", "is"}
}

func numberRange(t *Term) []string {
	if p := reIsRange.FindStringSubmatch(t.Text); p != nil {
		if t.Tags.Has("PhoneNumber") || rePhoneNum.MatchString(t.Text) {
			return nil
		}
		return []string{p[1], "to", p[2]}
	}
	if p := reTimeRange.FindStringSubmatch(t.Text); p != nil {
		return []string{p[1], "to", p[4]}
	}
	return nil
}

func (m *model) numberUnit(t *Term) []string {
	p := reNumUnit.FindStringSubmatch(t.Text)
	if p == nil {
		return nil
	}
	unit := jsTrim(jsToLower(p[2]))
	if m.numberSuffixes[unit] {
		return nil
	}
	return []string{p[1], unit}
}

func (m *model) contractionsOne(document [][]*Term) {
	for n := range document {
		for i := len(document[n]) - 1; i >= 0; i-- {
			terms := document[n]
			t := terms[i]
			before, after, has := splitApostrophe(t.Normal)
			words := m.knownContraction(t, before, after, has)
			if words == nil && has {
				switch after {
				case "t":
					words = apostropheT1(terms, i)
				case "d":
					words = apostropheD1(terms, i)
				}
			}
			if words == nil && has {
				switch before {
				case "j", "l", "d":
					words = frenchPre(before, terms, i)
				}
			}
			if has && before == "there" && after == "s" {
				words = thereHas(terms, i)
			}
			if words != nil {
				splice(document, n, i, m.toDocs(words, true))
				continue
			}
			if reNumDash.MatchString(t.Normal) {
				if words = numberRange(t); words != nil {
					ws := m.toDocs(words, true)
					splice(document, n, i, ws)
					m.setTag(ws, "NumberRange", false)
					if len(ws) > 2 && ws[2].Tags.Has("Time") {
						m.setTag(ws[:1], "Time", false)
					}
				}
				continue
			}
			if words = m.numberUnit(t); words != nil {
				ws := m.toDocs(words, true)
				splice(document, n, i, ws)
				if len(ws) > 1 {
					m.setTag(ws[1:2], "Unit", false)
				}
			}
		}
	}
}

// --- contraction-two --------------------------------------------------------

var (
	hadWords   = map[string]bool{"better": true, "done": true, "before": true, "it": true, "had": true}
	wouldWords = map[string]bool{"have": true, "be": true}
	hasWords   = map[string]bool{"been": true, "become": true}
	isWords    = map[string]bool{"what": true, "how": true, "when": true, "if": true, "too": true}
	adjLikeS   = map[string]bool{"too": true, "also": true, "enough": true}
)

func hadOrWould(terms []*Term, i int) string {
	for o := i + 1; o < len(terms); o++ {
		t := terms[o]
		switch {
		case hadWords[t.Normal]:
			return "had"
		case wouldWords[t.Normal]:
			return "would"
		case t.Tags.Has("PastTense") || t.Switch == "Adj|Past":
			return "had"
		case t.Tags.Has("PresentTense") || t.Tags.Has("Infinitive"):
			return "would"
		case t.Tags.Has("#Determiner"):
			return "had"
		case t.Tags.Has("Adjective"):
			return "would"
		}
	}
	return ""
}

func apostropheD2(terms []*Term, i int) []string {
	before, _, _ := splitApostrophe(terms[i].Normal)
	if before == "how" || before == "what" {
		return []string{before, "did"}
	}
	if hadOrWould(terms, i) == "had" {
		return []string{before, "had"}
	}
	return []string{before, "would"}
}

func isOrHas(terms []*Term, i int) string {
	for o := i + 1; o < len(terms); o++ {
		t := terms[o]
		if hasWords[t.Normal] {
			return "has"
		}
		if isWords[t.Normal] {
			return "is"
		}
		if t.Tags.Has("Gerund") || t.Tags.Has("Determiner") || t.Tags.Has("Adjective") {
			return "is"
		}
		if t.Switch == "Adj|Past" && o+1 < len(terms) {
			if adjLikeS[terms[o+1].Normal] || terms[o+1].Tags.Has("Preposition") {
				return "is"
			}
		}
		if t.Tags.Has("PastTense") {
			if o+1 < len(terms) && terms[o+1].Normal == "for" {
				return "is"
			}
			return "has"
		}
	}
	return "is"
}

func apostropheS2(terms []*Term, i int) []string {
	before, _, _ := splitApostrophe(terms[i].Normal)
	if before == "let" {
		return []string{before, "us"}
	}
	if before == "there" && i+1 < len(terms) && terms[i+1].Tags.Has("Plural") {
		return []string{before, "are"}
	}
	if isOrHas(terms, i) == "has" {
		return []string{before, "has"}
	}
	return []string{before, "is"}
}

func apostropheT2(terms []*Term, i int) []string {
	n := terms[i].Normal
	if n == "ain't" || n == "aint" {
		if i+1 < len(terms) && terms[i+1].Normal == "never" {
			return []string{"have"}
		}
		var noun *Term
		for k := i - 1; k >= 0; k-- {
			tg := &terms[k].Tags
			if tg.Has("Noun") || tg.Has("Pronoun") || tg.Has("Plural") || tg.Has("Singular") {
				noun = terms[k]
				break
			}
		}
		if noun != nil {
			if noun.Normal == "we" || noun.Normal == "they" {
				return []string{"are", "not"}
			}
			if noun.Normal == "i" {
				return []string{"am", "not"}
			}
			if noun.Tags.Has("Plural") {
				return []string{"are", "not"}
			}
		}
		return []string{"is", "not"}
	}
	return []string{strings.Replace(n, "n't", "", 1), "not"}
}

var (
	possBan          = map[string]bool{"that": true, "there": true, "let": true, "here": true, "everywhere": true}
	beforePossessive = map[string]bool{"in": true, "by": true, "for": true}
	adjLikePoss      = map[string]bool{"too": true, "also": true, "enough": true, "about": true}
	nounLikePoss     = map[string]bool{"is": true, "are": true, "did": true, "were": true, "could": true, "should": true, "must": true, "had": true, "have": true}
)

func isPossessive(terms []*Term, i int) bool {
	t := terms[i]
	if possBan[t.machineOrNormal()] {
		return false
	}
	if t.Tags.Has("Possessive") {
		return true
	}
	if t.Tags.Has("QuestionWord") {
		return false
	}
	if t.Normal == "he's" || t.Normal == "she's" {
		return false
	}
	if i+1 >= len(terms) {
		return true
	}
	next := terms[i+1]
	if t.Normal == "it's" {
		return next.Tags.Has("#Noun")
	}
	if next.Switch == "Noun|Gerund" {
		if i+2 >= len(terms) {
			return t.Tags.Has("Actor") || t.Tags.Has("ProperNoun")
		}
		return terms[i+2].Tags.Has("Copula")
	}
	if next.Tags.Has("Verb") {
		if next.Tags.Has("Infinitive") {
			return true
		}
		if next.Tags.Has("Gerund") {
			return false
		}
		return next.Tags.Has("PresentTense")
	}
	if next.Switch == "Adj|Noun" {
		if i+2 >= len(terms) {
			return false
		}
		two := terms[i+2]
		if nounLikePoss[two.Normal] {
			return true
		}
		if adjLikePoss[two.Normal] {
			return false
		}
	}
	if next.Tags.Has("Noun") {
		ns := next.machineOrNormal()
		if ns == "here" || ns == "there" || ns == "everywhere" {
			return false
		}
		if next.Tags.Has("Possessive") {
			return false
		}
		if next.Tags.Has("ProperNoun") && !t.Tags.Has("ProperNoun") {
			return false
		}
		return true
	}
	if i > 0 && beforePossessive[terms[i-1].Normal] {
		return true
	}
	if next.Tags.Has("Adjective") {
		if i+2 >= len(terms) {
			return false
		}
		two := terms[i+2]
		if two.Tags.Has("Noun") && !two.Tags.Has("Pronoun") {
			s := next.Normal
			return !(s == "above" || s == "below" || s == "behind")
		}
		return two.Switch == "Noun|Verb"
	}
	return next.Tags.Has("Value")
}

func (m *model) contractionsTwo(document [][]*Term) {
	for n := range document {
		for i := len(document[n]) - 1; i >= 0; i-- {
			terms := document[n]
			if terms[i].Implicit != "" {
				continue
			}
			_, after, has := splitApostrophe(terms[i].Normal)
			if !has {
				continue
			}
			var words []string
			switch after {
			case "d":
				words = apostropheD2(terms, i)
			case "t":
				words = apostropheT2(terms, i)
			case "s":
				if isPossessive(terms, i) {
					m.setTag(terms[i:i+1], "Possessive", false)
				} else {
					words = apostropheS2(terms, i)
				}
			}
			if words == nil {
				continue
			}
			ws := m.toDocs(words, false)
			if len(ws) == 0 {
				continue
			}
			splice(document, n, i, ws)
			m.reTag(document[n], i, len(ws))
			for k, t := range document[n] {
				t.Index[1] = k
			}
		}
	}
}

// reTag re-runs freeze, lexicon and preTagger on a window around a fresh
// contraction, then unfreezes it.
func (m *model) reTag(terms []*Term, start, length int) {
	end := start + length
	if start > 0 {
		start--
	}
	if end < len(terms) {
		end++
	}
	window := [][]*Term{terms[start:end]}
	m.freeze(window)
	m.lexiconTag(window)
	m.preTagger(window)
	unfreeze(window)
}
