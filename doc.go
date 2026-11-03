package compromise

import (
	"regexp"
	"sort"
	"strings"
)

// Doc is a parsed and tagged text, the equivalent of nlp(text) in compromise.
type Doc struct {
	m        *model
	document [][]*Term
	input    string

	// full caches All()'s ranges and term slices; edits reset it.
	full *fullCache
}

type fullCache struct {
	ptrs []pointer
	docs [][]*Term
	used []pointer
}

// New tokenises and tags text exactly as compromise's default nlp(text) does
// (minus the chunker and term ids, which no tag depends on).
func New(text string) *Doc {
	m := getModel()
	d := &Doc{m: m, document: fromString(text, m), input: text}
	doc := d.document
	m.contractionsOne(doc)
	for _, terms := range doc {
		for _, t := range terms {
			computeAlias(t, m)
		}
	}
	for _, terms := range doc {
		for _, t := range terms {
			computeMachine(t)
		}
	}
	d.reindex()
	m.freeze(doc)
	m.lexiconTag(doc)
	m.preTagger(doc)
	m.contractionsTwo(doc)
	m.postTagger(doc)
	return d
}

func (d *Doc) reindex() {
	for n, terms := range d.document {
		for i, t := range terms {
			t.Index = [2]int{n, i}
		}
	}
}

// Sentences returns the tokenised sentences.
func (d *Doc) Sentences() [][]*Term { return d.document }

// Text is the document's text: every term's pre + text + post.
func (d *Doc) Text() string {
	var b strings.Builder
	for _, terms := range d.document {
		for _, t := range terms {
			b.WriteString(t.Pre)
			b.WriteString(t.Text)
			b.WriteString(t.Post)
		}
	}
	return b.String()
}

// computeOffsets is compute('offset'): each term's UTF-16 start.
func (d *Doc) computeOffsets() {
	u16 := 0
	for _, terms := range d.document {
		for _, t := range terms {
			t.start = u16 + jsLen(t.Pre)
			u16 += jsLen(t.Pre) + jsLen(t.Text) + jsLen(t.Post)
		}
	}
}

// utf16ToByte converts a UTF-16 offset into s to a byte offset. Offsets are
// measured against the input rather than the terms because the sentence
// splitter turns the first no-break space into a space (same UTF-16 length,
// one byte shorter).
func utf16ToByte(s string, off int) int {
	if isASCII(s) {
		if off > len(s) {
			return len(s)
		}
		return off
	}
	u := 0
	for i, r := range s {
		if u >= off {
			return i
		}
		if r >= 0x10000 {
			u += 2
		} else {
			u++
		}
	}
	return len(s)
}

// All is a view of the whole document.
func (d *Doc) All() *View {
	if d.full == nil {
		fc := &fullCache{}
		for n, terms := range d.document {
			fc.ptrs = append(fc.ptrs, pointer{n, 0, len(terms)})
		}
		v := &View{doc: d, ptrs: fc.ptrs}
		fc.docs, fc.used = v.buildDocs()
		d.full = fc
	}
	return &View{doc: d, ptrs: d.full.ptrs, full: true}
}

// edited drops caches that describe the document's shape.
func (d *Doc) edited() { d.full = nil }

// Match is doc.match(pattern).
func (d *Doc) Match(pattern string) *View { return d.All().Match(pattern) }

// View is a set of term ranges in a Doc, like a compromise View.
type View struct {
	doc  *Doc
	ptrs []pointer
	full bool // ptrs are the cached whole-document ranges
}

func (v *View) update(ptrs []pointer) *View { return &View{doc: v.doc, ptrs: ptrs} }

// docs is getDoc: the term slices, skipping empty ranges.
func (v *View) docs() ([][]*Term, []pointer) {
	if v.full && v.doc.full != nil {
		return v.doc.full.docs, v.doc.full.used
	}
	return v.buildDocs()
}

func (v *View) buildDocs() ([][]*Term, []pointer) {
	var out [][]*Term
	var used []pointer
	for _, p := range v.ptrs {
		if p[0] >= len(v.doc.document) {
			continue
		}
		terms := v.doc.document[p[0]]
		s, e := p[1], p[2]
		if e > len(terms) {
			e = len(terms)
		}
		if s >= e {
			continue
		}
		out = append(out, terms[s:e])
		used = append(used, p)
	}
	return out, used
}

// Found reports whether the view has any terms.
func (v *View) Found() bool {
	docs, _ := v.docs()
	return len(docs) > 0
}

// Len is the number of ranges (view.length).
func (v *View) Len() int { return len(v.ptrs) }

func (v *View) runOn(regs []*reg, group string, hasGroup bool, justOne bool) []pointer {
	docs, full := v.docs()
	res := runMatch(docs, matchTodo{regs: regs, group: group, hasGroup: hasGroup, justOne: justOne}, nil)
	out := make([]pointer, 0, len(res.ptrs))
	for _, p := range res.ptrs {
		f := full[p[0]]
		out = append(out, pointer{f[0], f[1] + p[1], f[1] + p[2]})
	}
	return out
}

func (v *View) parse(pattern string) []*reg { return parseMatch(pattern, v.doc.m.prefixes) }

// Match is view.match(pattern).
func (v *View) Match(pattern string) *View {
	return v.update(v.runOn(v.parse(pattern), "", false, false))
}

// MatchGroup is view.match(pattern, group).
func (v *View) MatchGroup(pattern, group string) *View {
	return v.update(v.runOn(v.parse(pattern), group, true, false))
}

// Has is view.has(pattern).
func (v *View) Has(pattern string) bool {
	return len(v.runOn(v.parse(pattern), "", false, true)) > 0
}

// Terms is view.terms(): one range per term.
func (v *View) Terms() *View {
	var out []pointer
	for _, p := range v.ptrs {
		for i := p[1]; i < p[2]; i++ {
			out = append(out, pointer{p[0], i, i + 1})
		}
	}
	return v.update(out)
}

// Each returns one single-range view per range.
func (v *View) Each() []*View {
	out := make([]*View, len(v.ptrs))
	for i, p := range v.ptrs {
		out[i] = v.update([]pointer{p})
	}
	return out
}

// --- splitting (1-one/pointers/api/lib/split.js) ----------------------------

type splitPart struct {
	passthrough, before, match, after *pointer
}

func ptrRef(p pointer) *pointer { return &p }

func splitAll(full []pointer, m []pointer) []splitPart {
	byN := map[int][]pointer{}
	for _, p := range m {
		byN[p[0]] = append(byN[p[0]], p)
	}
	var res []splitPart
	for _, ptr := range full {
		var matches []pointer
		for _, p := range byN[ptr[0]] {
			if ptr[1] <= p[1] && p[2] <= ptr[2] {
				matches = append(matches, p)
			}
		}
		if len(matches) == 0 {
			res = append(res, splitPart{passthrough: ptrRef(ptr)})
			continue
		}
		sort.SliceStable(matches, func(a, b int) bool { return matches[a][1] < matches[b][1] })
		carry := ptr
		for i, p := range matches {
			var found splitPart
			if carry[1] < p[1] {
				end := carry[2]
				if p[1] < carry[2] {
					end = p[1]
				}
				found.before = ptrRef(pointer{carry[0], carry[1], end})
			}
			found.match = ptrRef(p)
			if carry[2] > p[2] {
				found.after = ptrRef(pointer{carry[0], p[2], carry[2]})
			}
			if i == len(matches)-1 {
				res = append(res, found)
			} else {
				res = append(res, splitPart{before: found.before, match: found.match})
				if found.after != nil {
					carry = *found.after
				}
			}
		}
	}
	return res
}

func appendPtr(out []pointer, p *pointer) []pointer {
	if p != nil {
		out = append(out, *p)
	}
	return out
}

func (v *View) splitter(m *View) []splitPart { return splitAll(v.ptrs, m.ptrs) }

// SplitOn is view.splitOn(pattern).
func (v *View) SplitOn(pattern string) *View { return v.SplitOnView(v.Match(pattern)) }

// SplitOnView is view.splitOn(otherView).
func (v *View) SplitOnView(m *View) *View {
	var res []pointer
	for _, o := range v.splitter(m) {
		res = appendPtr(res, o.passthrough)
		res = appendPtr(res, o.before)
		res = appendPtr(res, o.match)
		res = appendPtr(res, o.after)
	}
	return v.update(res)
}

// SplitBefore is view.splitBefore(pattern[, group]).
func (v *View) SplitBefore(pattern string, group ...string) *View {
	var m *View
	if len(group) > 0 {
		m = v.MatchGroup(pattern, group[0])
	} else {
		m = v.Match(pattern)
	}
	all := v.splitter(m)
	for i := 0; i < len(all); i++ {
		if all[i].after == nil && i+1 < len(all) && all[i+1].before != nil {
			if all[i].match != nil && all[i].match[0] == all[i+1].before[0] {
				all[i].after = all[i+1].before
				all[i+1].before = nil
			}
		}
	}
	var res []pointer
	for _, o := range all {
		res = appendPtr(res, o.passthrough)
		res = appendPtr(res, o.before)
		if o.match != nil && o.after != nil {
			res = append(res, pointer{o.match[0], o.match[1], o.after[2]})
		} else {
			res = appendPtr(res, o.match)
		}
	}
	return v.update(res)
}

// SplitAfter is view.splitAfter(pattern).
func (v *View) SplitAfter(pattern string) *View {
	all := v.splitter(v.Match(pattern))
	var res []pointer
	for _, o := range all {
		res = appendPtr(res, o.passthrough)
		if o.before != nil && o.match != nil {
			res = append(res, pointer{o.before[0], o.before[1], o.match[2]})
		} else {
			res = appendPtr(res, o.before)
			res = appendPtr(res, o.match)
		}
		res = appendPtr(res, o.after)
	}
	return v.update(res)
}

// Not is view.not(otherView).
func (v *View) Not(m *View) *View {
	var res []pointer
	for _, o := range splitAll(v.ptrs, m.ptrs) {
		res = appendPtr(res, o.passthrough)
		res = appendPtr(res, o.before)
		res = appendPtr(res, o.after)
	}
	return v.update(res)
}

// --- output -----------------------------------------------------------------

// Out is view.out('array'): each range's pre+text+post, trimmed, empties dropped.
func (v *View) Out() []string {
	docs, _ := v.docs()
	var out []string
	for _, terms := range docs {
		var b strings.Builder
		for _, t := range terms {
			b.WriteString(t.Pre)
			b.WriteString(t.Text)
			b.WriteString(t.Post)
		}
		if s := jsTrim(b.String()); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Span is one matched range with its offsets into the input text.
type Span struct {
	// Start and End are UTF-16 code-unit offsets, as compromise reports them.
	Start, End int
	// ByteStart and ByteEnd index the Go string.
	ByteStart, ByteEnd int
	Terms              []*Term
}

// Spans returns each range's offsets, measured like compromise's
// json({offset:true}): first term's start to last term's start + length.
func (v *View) Spans() []Span {
	v.doc.computeOffsets()
	docs, _ := v.docs()
	out := make([]Span, 0, len(docs))
	for _, terms := range docs {
		first, last := terms[0], terms[len(terms)-1]
		start, end := first.start, last.start+jsLen(last.Text)
		out = append(out, Span{
			Start:     start,
			End:       end,
			ByteStart: utf16ToByte(v.doc.input, start),
			ByteEnd:   utf16ToByte(v.doc.input, end),
			Terms:     terms,
		})
	}
	return out
}

var (
	reTrimEnd      = regexp.MustCompile(`[,:;)\]*.?~!\x{0022}\x{FF02}\x{201D}\x{2019}\x{00BB}\x{203A}\x{2032}\x{2033}\x{2034}\x{301E}\x{00B4}—-]+$`)
	reTrimStart    = regexp.MustCompile("^[(\\['\"*~\\x{FF02}\\x{201C}\\x{2018}\\x{201F}\\x{201B}\\x{201E}\\x{2E42}\\x{201A}\\x{00AB}\\x{2039}\\x{2035}\\x{2036}\\x{2037}\\x{301D}\\x{0060}\\x{301F}]+")
	rePunctToKill  = regexp.MustCompile(`[,:;)('"\x{201D}\]]`)
	reIsHyphenPost = regexp.MustCompile(`^[-–—]$`)
	reQBang        = regexp.MustCompile(`\?!+`)
	reBangs        = regexp.MustCompile(`!+`)
	reQs           = regexp.MustCompile(`\?+`)
	reDots2        = regexp.MustCompile(`\.{2,}`)
	reWS1          = regexp.MustCompile(`[` + jsSpace + `]`)
	reWSRun        = regexp.MustCompile(`[` + jsSpace + `]+`)
)

func replaceFirst(re *regexp.Regexp, s, repl string) string {
	loc := re.FindStringIndex(s)
	if loc == nil {
		return s
	}
	return s[:loc[0]] + repl + s[loc[1]:]
}

// textForm is textFromDoc for the 'reduced' (root), 'machine' and 'text' formats.
func (v *View) textForm(form string) string {
	docs, used := v.docs()
	if len(docs) == 0 {
		return ""
	}
	some := form == "root" || form == "machine"
	keepSpace := !some // root/machine set keepSpace false
	if form == "text" && !v.isFull() {
		keepSpace = false
	}
	keepEndPunct := true
	if len(used) > 0 && used[0][1] != 0 {
		keepEndPunct = false
	}
	var b strings.Builder
	for _, terms := range docs {
		for _, t := range terms {
			pre, post := t.Pre, t.Post
			if some {
				pre = replaceFirst(reTrimStart, pre, "")
				if reIsHyphenPost.MatchString(post) {
					post = " "
				}
				post = replaceFirst(rePunctToKill, post, "")
				post = replaceFirst(reQBang, post, "?")
				post = replaceFirst(reBangs, post, "!")
				post = replaceFirst(reQs, post, "?")
				post = replaceFirst(reDots2, post, "")
				if t.Tags.Has("Abbreviation") {
					post = strings.Replace(post, ".", "", 1)
				}
				pre = replaceFirst(reWS1, pre, "")
				post = replaceFirst(reWSRun, post, " ")
			}
			word := t.Text
			if form != "text" {
				word = t.Normal
				if form == "machine" && t.Machine != "" {
					word = t.Machine
				}
			}
			if word == "" {
				word = t.Normal
			}
			if form == "root" && t.Implicit != "" {
				word = t.Implicit
				if word == "" {
					word = t.Normal
				}
			}
			if some && t.Implicit != "" {
				if post == "" || !strings.Contains(post, " ") {
					post += " "
				}
			}
			b.WriteString(pre + word + post)
		}
	}
	text := b.String()
	if !keepSpace {
		text = jsTrim(text)
	}
	if !keepEndPunct {
		if !docs[0][0].Tags.Has("Emoticon") {
			text = replaceFirst(reTrimStart, text, "")
		}
		last := docs[len(docs)-1]
		if !last[len(last)-1].Tags.Has("Emoticon") {
			text = replaceFirst(reTrimEnd, text, "")
		}
		if strings.HasSuffix(text, "'") && !strings.HasSuffix(text, "s'") {
			text = strings.Replace(text, "'", "", 1)
		}
	}
	return text
}

func (v *View) isFull() bool {
	if len(v.ptrs) != len(v.doc.document) {
		return false
	}
	for i, p := range v.ptrs {
		if p[0] != i || p[1] != 0 || p[2] != len(v.doc.document[i]) {
			return false
		}
	}
	return true
}
