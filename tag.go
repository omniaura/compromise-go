package compromise

import "strings"

// Port of compromise/src/1-one/tag/methods (setTag, unTag, canBe) and the
// preTagger's fastTag/fillTags helpers.

func (m *model) tagTerm(t *Term, tag string, isSafe bool) {
	if t.Tags.Has(tag) || tag == "." {
		return
	}
	if t.Frozen {
		isSafe = true
	}
	if known := m.tagSet[tag]; known != nil {
		for _, not := range known.Not {
			if isSafe && t.Tags.Has(not) {
				return
			}
			t.Tags.Delete(not)
		}
		for _, p := range known.Parents {
			t.Tags.Add(p)
			addChunk(t, p)
		}
	}
	t.Tags.Add(tag)
	addChunk(t, tag)
}

func addChunk(t *Term, tag string) {
	if tag == "Noun" || tag == "Verb" {
		t.Chunk = tag
	}
}

// setTag is methods.one.setTag for a single tag string (which may hold
// several space-separated tags, one per term).
func (m *model) setTag(terms []*Term, tag string, isSafe bool) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return
	}
	if strings.Contains(tag, " ") {
		tags := strings.Split(tag, " ")
		for i, t := range terms {
			if i < len(tags) && tags[i] != "" {
				m.tagTerm(t, strings.TrimPrefix(tags[i], "#"), isSafe)
			}
		}
		return
	}
	tag = strings.TrimPrefix(tag, "#")
	for _, t := range terms {
		m.tagTerm(t, tag, isSafe)
	}
}

// setTags is setTag called with a lexicon value (a string or an array).
func (m *model) setTags(terms []*Term, tags tagList, isSafe bool) {
	if len(tags) == 1 {
		m.setTag(terms, tags[0], isSafe)
		return
	}
	for _, tg := range tags {
		m.setTag(terms, tg, isSafe)
	}
}

func (m *model) unTag(terms []*Term, tag string) {
	tag = strings.TrimPrefix(strings.TrimSpace(tag), "#")
	for _, t := range terms {
		if t.Frozen {
			continue
		}
		if tag == "*" {
			t.Tags.Clear()
			continue
		}
		if known := m.tagSet[tag]; known != nil {
			for _, c := range known.Children {
				t.Tags.Delete(c)
			}
		}
		t.Tags.Delete(tag)
	}
}

// canBe is the sweep tagger's canBe: can every term take this tag?
func (m *model) canBe(terms []*Term, tag string) bool {
	known := m.tagSet[tag]
	if known == nil {
		return true
	}
	for _, t := range terms {
		for _, not := range known.Not {
			if t.Tags.Has(not) {
				return false
			}
		}
	}
	return true
}

func fastTag(t *Term, tags ...string) {
	if len(tags) == 0 || t.Frozen {
		return
	}
	for _, tg := range tags {
		t.Tags.Add(tg)
	}
}

var uncountableTags = []string{"Acronym", "Abbreviation", "ProperNoun", "Uncountable", "Possessive", "Pronoun", "Activity", "Honorific", "Month"}

func (m *model) fillTags(terms []*Term, i int) {
	t := terms[i]
	for _, tg := range t.Tags.List() {
		if known := m.tagSet[tg]; known != nil {
			fastTag(t, known.Parents...)
		}
	}
	// setPluralSingular
	if t.Tags.Has("Noun") && !t.Tags.Has("Plural") && !t.Tags.Has("Singular") {
		skip := false
		for _, u := range uncountableTags {
			if t.Tags.Has(u) {
				skip = true
				break
			}
		}
		if !skip {
			if looksPlural(t.Normal) {
				fastTag(t, "Plural")
			} else {
				fastTag(t, "Singular")
			}
		}
	}
	// setTense
	if t.Tags.Has("Verb") && t.Tags.Size() == 1 {
		if guess := getTense(t.Normal); guess != "" {
			fastTag(t, guess)
		}
	}
}

var looksPluralIs = map[byte][]string{
	'e': {"mice", "louse", "antennae", "formulae", "nebulae", "vertebrae", "vitae"},
	'i': {"tia", "octopi", "viri", "radii", "nuclei", "fungi", "cacti", "stimuli"},
	'n': {"men"},
	't': {"feet"},
}

var looksPluralExceptions = map[string]bool{"israelis": true, "menus": true, "logos": true}

var looksPluralNot = []string{
	"bus", "mas", "was", "ias", "xas", "vas", "cis", "lis", "nis", "ois", "ris", "sis", "tis", "xis",
	"aus", "cus", "eus", "fus", "gus", "ius", "lus", "nus", "das", "ous", "pus", "rus", "sus", "tus",
	"xus", "aos", "igos", "ados", "ogos", "'s", "ss",
}

func looksPlural(str string) bool {
	if str == "" || jsLen(str) <= 3 {
		return false
	}
	if looksPluralExceptions[str] {
		return true
	}
	end, _ := jsLastChar(str)
	if end < 0x80 {
		if list, ok := looksPluralIs[byte(end)]; ok {
			for _, suff := range list {
				if strings.HasSuffix(str, suff) {
					return true
				}
			}
			return false
		}
	}
	if end != 's' {
		return false
	}
	for _, suff := range looksPluralNot {
		if strings.HasSuffix(str, suff) {
			return false
		}
	}
	return true
}

var guessVerb = func() map[string]string {
	src := map[string][]string{
		"Gerund": {"ing"},
		"Actor":  {"erer"},
		"Infinitive": {"ate", "ize", "tion", "rify", "then", "ress", "ify", "age", "nce", "ect", "ise", "ine",
			"ish", "ace", "ash", "ure", "tch", "end", "ack", "and", "ute", "ade", "ock", "ite", "ase", "ose",
			"use", "ive", "int", "nge", "lay", "est", "ain", "ant", "ent", "eed", "er", "le", "unk", "ung",
			"upt", "en"},
		"PastTense": {"ept", "ed", "lt", "nt", "ew", "ld"},
		"PresentTense": {"rks", "cks", "nks", "ngs", "mps", "tes", "zes", "ers", "les", "acks", "ends", "ands",
			"ocks", "lays", "eads", "lls", "els", "ils", "ows", "nds", "ays", "ams", "ars", "ops", "ffs", "als",
			"urs", "lds", "ews", "ips", "es", "ts", "ns"},
		"Participle": {"ken", "wn"},
	}
	// JS builds this with Object.keys order, so a later key wins on a clash.
	order := []string{"Gerund", "Actor", "Infinitive", "PastTense", "PresentTense", "Participle"}
	out := map[string]string{}
	for _, k := range order {
		for _, s := range src[k] {
			out[s] = k
		}
	}
	return out
}()

func getTense(str string) string {
	n := jsLen(str)
	if g, ok := guessVerb[jsSubstring(str, n-3, n)]; ok {
		return g
	}
	if g, ok := guessVerb[jsSubstring(str, n-2, n)]; ok {
		return g
	}
	if jsSubstring(str, n-1, n) == "s" {
		return "PresentTense"
	}
	return ""
}
