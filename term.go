package compromise

// Term is one word, as compromise models it: the original text with its
// surrounding whitespace and punctuation split off into Pre and Post, a
// normalised form, and the tags the tagger settled on.
//
// A contraction such as "it's" becomes two terms: "it" keeps the text "It's"
// and gets Implicit "it"; the second term has empty Text and Implicit "is".
type Term struct {
	Text   string
	Pre    string
	Post   string
	Normal string
	// Machine is compromise's machine form ("" when it equals Normal).
	Machine string
	// Implicit is the expanded word of a contraction ("" when not one).
	Implicit string
	Alias    []string
	Tags     Tags
	// Switch names an ambiguous word class from the switches model, such as "Noun|Verb".
	Switch string
	Chunk  string
	Frozen bool
	// Index is [sentence, position] in the document.
	Index [2]int

	// start is the term's UTF-16 offset into the input (set by computeOffsets).
	start int
}

// machineOrNormal is `term.machine || term.normal`.
func (t *Term) machineOrNormal() string {
	if t.Machine != "" {
		return t.Machine
	}
	return t.Normal
}

// normalOrImplicit is `term.normal || term.implicit`.
func (t *Term) normalOrImplicit() string {
	if t.Normal != "" {
		return t.Normal
	}
	return t.Implicit
}

// Tags is an insertion-ordered set, like the JS Set compromise uses. Order
// matters: several tagger steps iterate a term's tags.
type Tags struct {
	list []string
}

func (s *Tags) Has(tag string) bool {
	for _, t := range s.list {
		if t == tag {
			return true
		}
	}
	return false
}

func (s *Tags) Add(tag string) {
	if !s.Has(tag) {
		s.list = append(s.list, tag)
	}
}

func (s *Tags) Delete(tag string) {
	for i, t := range s.list {
		if t == tag {
			s.list = append(s.list[:i], s.list[i+1:]...)
			return
		}
	}
}

func (s *Tags) Clear()    { s.list = s.list[:0] }
func (s *Tags) Size() int { return len(s.list) }
func (s *Tags) List() []string {
	out := make([]string, len(s.list))
	copy(out, s.list)
	return out
}
