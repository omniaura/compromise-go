package compromise

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// jsNumberString is String(n) for a JS number.
func jsNumberString(n float64) string {
	switch {
	case math.IsNaN(n):
		return "NaN"
	case math.IsInf(n, 1):
		return "Infinity"
	case math.IsInf(n, -1):
		return "-Infinity"
	case n == 0:
		return "0"
	}
	a := math.Abs(n)
	if a >= 1e21 || a < 1e-6 {
		s := strconv.FormatFloat(n, 'e', -1, 64)
		// Go writes 1e+21 / 1e-07; JS writes 1e+21 / 1e-7.
		mant, exp, _ := strings.Cut(s, "e")
		sign := exp[0]
		exp = strings.TrimLeft(exp[1:], "0")
		return mant + "e" + string(sign) + exp
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// jsToLocaleString is Number.prototype.toLocaleString() in en-US.
func jsToLocaleString(n float64) string {
	if math.IsNaN(n) {
		return "NaN"
	}
	if math.IsInf(n, 0) {
		if n > 0 {
			return "∞"
		}
		return "-∞"
	}
	s := strconv.FormatFloat(n, 'f', 3, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	intPart, frac, hasFrac := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := b.String()
	if hasFrac {
		out += "." + frac
	}
	if neg && out != "0" {
		out = "-" + out
	}
	return out
}

// numToString is numbers/_toString.js.
func numToString(n float64) string {
	if n < 1000000 {
		return jsNumberString(n)
	}
	return strconv.FormatFloat(math.Round(n), 'f', 0, 64)
}

// numOrdinal is format/toOrdinal/numOrdinal.js; ok is false for JS null.
func numOrdinal(num float64) (string, bool) {
	if num == 0 || math.IsNaN(num) {
		if num == 0 && !math.IsNaN(num) {
			// `!num && num !== 0` is false for 0: fall through
		} else {
			return "", false
		}
	}
	tens := math.Mod(num, 100)
	if tens > 10 && tens < 20 {
		return jsNumberString(num) + "th", true
	}
	str := numToString(num)
	switch str[len(str)-1:] {
	case "1":
		return str + "st", true
	case "2":
		return str + "nd", true
	case "3":
		return str + "rd", true
	default:
		return str + "th", true
	}
}

func formatNumber(p ParsedNumber, ordinal bool) string {
	if ordinal {
		s, ok := numOrdinal(p.Num)
		if !ok {
			s = "null"
		}
		return p.Prefix + s + p.Suffix
	}
	num := jsNumberString(p.Num)
	if p.HasComma {
		num = jsToLocaleString(p.Num)
	}
	return p.Prefix + num + p.Suffix
}

// ToNumber is numbers.toNumber(): every value is rewritten in numeric form,
// in place, when the view holds any #TextValue. Like compromise, this
// mutates the document, so later matches see "3" where "three" was.
func (v *View) ToNumber() *View {
	d := v.doc
	type anchor struct{ first, last *Term }
	anchors := make([]anchor, 0, len(v.ptrs))
	for _, p := range v.ptrs {
		terms := d.document[p[0]]
		if p[1] >= p[2] || p[2] > len(terms) {
			anchors = append(anchors, anchor{})
			continue
		}
		anchors = append(anchors, anchor{terms[p[1]], terms[p[2]-1]})
	}
	current := func() []pointer {
		out := make([]pointer, 0, len(anchors))
		for _, a := range anchors {
			if p, ok := d.locate(a.first, a.last); ok {
				out = append(out, p)
			}
		}
		return out
	}
	for i := range anchors {
		if !d.hasTag(current(), "TextValue") {
			continue
		}
		p, ok := d.locate(anchors[i].first, anchors[i].last)
		if !ok {
			continue
		}
		val := v.update([]pointer{p})
		obj := val.parseValue()
		str := formatNumber(obj, val.Has("#Ordinal"))
		newTerms := d.replaceWith(p, str)
		if len(newTerms) > 0 {
			anchors[i] = anchor{newTerms[0], newTerms[len(newTerms)-1]}
		}
	}
	return v.update(current())
}

// locate finds the current range of a run of terms by identity. Terms carry
// their [sentence, position] index, kept current by reindex after every edit.
func (d *Doc) locate(first, last *Term) (pointer, bool) {
	if first == nil {
		return pointer{}, false
	}
	n, i := first.Index[0], first.Index[1]
	if n >= len(d.document) || i >= len(d.document[n]) || d.document[n][i] != first {
		return pointer{}, false
	}
	terms := d.document[n]
	for j := i; j < len(terms); j++ {
		if terms[j] == last {
			return pointer{n, i, j + 1}, true
		}
	}
	return pointer{}, false
}

// hasTag is view.has('#Tag') without going through the matcher.
func (d *Doc) hasTag(ptrs []pointer, tag string) bool {
	for _, p := range ptrs {
		if p[0] >= len(d.document) {
			continue
		}
		terms := d.document[p[0]]
		for i := p[1]; i < p[2] && i < len(terms); i++ {
			if terms[i].Tags.Has(tag) {
				return true
			}
		}
	}
	return false
}

var reJuicy = regexp.MustCompile(`[-.?!,;:)–—'"]`)

// endSpace gives the last term a trailing space unless it has one or a dash.
func endSpace(terms []*Term) {
	if len(terms) == 0 {
		return
	}
	last := terms[len(terms)-1]
	if !strings.HasSuffix(last.Post, " ") && !strings.ContainsAny(last.Post, "-–—") {
		last.Post += " "
	}
}

// movePunct moves the replaced word's trailing punctuation onto the new words.
func movePunct(wasLast *Term, needle []*Term) {
	if wasLast == nil {
		return
	}
	if puncts := reJuicy.FindAllString(wasLast.Post, -1); len(puncts) > 0 {
		last := needle[len(needle)-1]
		last.Post = strings.Join(puncts, "") + last.Post
		wasLast.Post = reJuicy.ReplaceAllString(wasLast.Post, "")
	}
}

// replaceWith is view.replaceWith(str, {tags: true}) as toNumber() uses it:
// insertAfter (cleanAppend) the tagged words, delete the old ones, restore
// the outer punctuation, re-tag the old pointer's range and copy the old
// tags back safely, then tag #NumericValue.
func (d *Doc) replaceWith(p pointer, str string) []*Term {
	m := d.m
	parsed := m.fromText(str)
	if len(parsed) == 0 || len(parsed[0]) == 0 {
		return nil
	}
	needle := parsed[0]
	n := p[0]
	home := d.document[n]
	old := append([]*Term(nil), home[p[1]:p[2]]...)
	oldTags := make([][]string, len(old))
	for i, t := range old {
		oldTags[i] = t.Tags.List()
	}
	originalPre := old[0].Pre
	originalPost := old[len(old)-1].Post

	// cleanAppend
	total := len(home)
	if p[2] < total {
		movePunct(home[p[2]-1], needle)
		endSpace(needle)
	} else {
		endSpace(home)
		movePunct(home[p[2]-1], needle)
		if n+1 < len(d.document) {
			needle[len(needle)-1].Post += " "
		}
	}
	// splice the needle in after the old words, then pluck the old words out
	out := make([]*Term, 0, total-len(old)+len(needle))
	out = append(out, home[:p[1]]...)
	out = append(out, needle...)
	out = append(out, home[p[2]:]...)
	d.document[n] = out
	d.edited()
	for k, t := range out {
		t.Index = [2]int{n, k}
	}

	if originalPre != "" {
		needle[0].Pre = originalPre
	}
	if last := needle[len(needle)-1]; originalPost != "" && jsTrim(last.Post) == "" {
		last.Post = originalPost
	}
	// toView(ptrs).compute(['index','freeze','lexicon','preTagger','unfreeze'])
	// on the original pointer, which may now reach past the new words.
	end := p[2]
	if end > len(out) {
		end = len(out)
	}
	window := [][]*Term{out[p[1]:end]}
	m.freeze(window)
	m.lexiconTag(window)
	m.preTagger(window)
	unfreeze(window)
	for i, t := range window[0] {
		if i < len(oldTags) && i < len(needle) {
			for _, tg := range oldTags[i] {
				m.setTag([]*Term{t}, tg, true)
			}
		}
	}
	m.setTag(needle, "NumericValue", false)
	return needle
}
