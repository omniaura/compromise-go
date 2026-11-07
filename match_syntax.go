package compromise

// Port of compromise v14.16.0 src/1-one/match/methods/parseMatch/*: the
// match-syntax parser that turns 'foo [#Noun+] (a|b)?' into a list of regs.
//
// Deliberate fidelity notes (each mirrors a JS behaviour line by line):
//   - parseBlocks splits with String.prototype.split(RegExp), so the capture
//     group of bySlashes / byParentheses lands in the output array.
//   - JS key presence is tracked where it is observable: group === null,
//     min/max === NaN ('{}'), and empty-string word-ish keys ('#', '@', '%%', '<>').
//   - '{root}' tokens are parsed into Root/Pos/Sense, but the 04-inflect-root
//     step is not ported: such regs match nothing.

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// reg is one parsed match token. "" means undefined for the string fields.
type reg struct {
	Word   string
	Tag    string
	Method string
	Switch string
	Chunk  string
	Root   string

	Regex *jsRegexp

	Optional   bool
	Greedy     bool
	Negative   bool
	Start      bool
	End        bool
	Anything   bool
	Fuzzy      bool
	GroupStart bool
	GroupEnd   bool

	// Group is JS reg.group once nameGroups has run; HasGroup == Boolean(reg.group).
	Group    string
	HasGroup bool

	// Min/Max mirror reg.min/reg.max; HasMin/HasMax distinguish undefined from 0.
	Min, Max       int
	HasMin, HasMax bool

	Choices  [][]*reg // nil == undefined; an empty non-nil slice is JS []
	Operator string   // "or" | "and"
	FastOr   map[string]bool
	Use      string // "text" when caseSensitive

	// Pos and Sense come from '{root/pos/sense}' tokens.
	Pos   string
	Sense string

	// Exact JS numbers behind Min/Max (fuzzy min is 0.85, '{}' gives NaN).
	minF, maxF     float64
	minFOK, maxFOK bool
	// groupNull is JS `reg.group === null` (key present, value null).
	groupNull bool
	// presence of keys whose JS value is the empty string
	tagSet, methodSet, switchSet, chunkSet bool
	// fastOrList keeps the JS Set insertion order (for JSON output only).
	fastOrList []string
}

func (r *reg) setMin(v float64) {
	r.minF, r.minFOK, r.HasMin, r.Min = v, true, true, floatToInt(v)
}

func (r *reg) setMax(v float64) {
	r.maxF, r.maxFOK, r.HasMax, r.Max = v, true, true, floatToInt(v)
}

// minVal is the JS value of reg.min (only meaningful when HasMin).
func (r *reg) minVal() float64 {
	if r.minFOK {
		return r.minF
	}
	return float64(r.Min)
}

func (r *reg) maxVal() float64 {
	if r.maxFOK {
		return r.maxF
	}
	return float64(r.Max)
}

func floatToInt(v float64) int {
	if math.IsNaN(v) || math.IsInf(v, 0) || v > math.MaxInt32 || v < math.MinInt32 {
		return 0
	}
	return int(v)
}

// jsTruthyNum is the truthiness of a JS number.
func jsTruthyNum(v float64) bool { return v != 0 && !math.IsNaN(v) }

func (r *reg) tagDefined() bool    { return r.Tag != "" || r.tagSet }
func (r *reg) methodDefined() bool { return r.Method != "" || r.methodSet }
func (r *reg) switchDefined() bool { return r.Switch != "" || r.switchSet }
func (r *reg) chunkDefined() bool  { return r.Chunk != "" || r.chunkSet }

func (r *reg) setGroupName(g string) {
	r.Group, r.HasGroup, r.groupNull = g, g != "", false
}

func (r *reg) setFastOr(words []string) {
	r.FastOr = make(map[string]bool, len(words))
	r.fastOrList = r.fastOrList[:0]
	for _, w := range words {
		if !r.FastOr[w] {
			r.FastOr[w] = true
			r.fastOrList = append(r.fastOrList, w)
		}
	}
}

// ---------------------------------------------------------------------------
// index.js

// parseMatch is compromise's methods.one.parseMatch(str, {}, world) minus the
// '{root}' inflection step. prefixes is model.one.prefixes.
func parseMatch(pattern string, prefixes map[string]bool) []*reg {
	if pattern == "" {
		return []*reg{}
	}
	blocks := parseBlocks(pattern)
	regs := make([]*reg, 0, len(blocks))
	for _, b := range blocks {
		regs = append(regs, parseToken(b))
	}
	regs = splitHyphens(regs, prefixes)
	return postProcess(regs)
}

// ---------------------------------------------------------------------------
// 01-parseBlocks.js

// JS '.' (no s flag) excludes all four line terminators; Go's only excludes \n.
const jsDotClass = `[^\n\r\x{2028}\x{2029}]`

var (
	// /(?:^|\s)([![^]*(?:<[^<]*>)?\/.*?[^\\/]\/[?\]+*$~]*)(?:\s|$)/
	// Go's leftmost-first submatch semantics equal JS backtracking here (no
	// lookaround/backrefs). '^' cannot match at a sticky position > 0, so a
	// search from q > 0 uses the variant without it.
	bySlashesAt0 = regexp.MustCompile(`(?:^|[` + jsSpace + `])([!\[^]*(?:<[^<]*>)?/` + jsDotClass + `*?[^\\/]/[?\]+*$~]*)(?:[` + jsSpace + `]|$)`)
	bySlashesAtN = regexp.MustCompile(`[` + jsSpace + `]([!\[^]*(?:<[^<]*>)?/` + jsDotClass + `*?[^\\/]/[?\]+*$~]*)(?:[` + jsSpace + `]|$)`)
	// /([!~[^]*(?:<[^<]*>)?\([^)]+[^\\)]\)[?\]+*$~]*)(?:\s|$)/
	byParentheses = regexp.MustCompile(`([!~\[^]*(?:<[^<]*>)?\([^)]+[^\\)]\)[?\]+*$~]*)(?:[` + jsSpace + `]|$)`)

	isBlockStart = regexp.MustCompile(`^[!\[^]*(<[^<]*>)?\(`)
	isBlockEnd   = regexp.MustCompile(`\)[?\]+*$~]*$`)
	isRegStart   = regexp.MustCompile(`^[!\[^]*(<[^<]*>)?/`)
	isRegEnd     = regexp.MustCompile(`/[?\]+*$~]*$`)
)

func isBlock(s string) bool { return isBlockStart.MatchString(s) && isBlockEnd.MatchString(s) }
func isReg(s string) bool   { return isRegStart.MatchString(s) && isRegEnd.MatchString(s) }

// jsSplitRegexp is String.prototype.split(re) for a regex with one capture
// group that never matches the empty string. at0 is used for a match attempt
// at position 0, atN for later positions (they differ only for a '^' anchor).
func jsSplitRegexp(s string, at0, atN *regexp.Regexp) []string {
	if s == "" {
		// a non-empty-only regex cannot match "", so JS returns [""]
		return []string{s}
	}
	var out []string
	p, q := 0, 0
	for q < len(s) {
		var loc []int
		if q == 0 {
			loc = at0.FindStringSubmatchIndex(s)
		} else if l := atN.FindStringSubmatchIndex(s[q:]); l != nil {
			loc = make([]int, len(l))
			for i, v := range l {
				if v >= 0 {
					v += q
				}
				loc[i] = v
			}
		}
		if loc == nil || loc[0] >= len(s) {
			break
		}
		e := loc[1]
		if e == p {
			// empty match at the last split point: advance (unreachable for our regexes)
			_, size := utf8.DecodeRuneInString(s[loc[0]:])
			q = loc[0] + size
			continue
		}
		out = append(out, s[p:loc[0]])
		for g := 1; 2*g+1 < len(loc); g++ {
			if loc[2*g] < 0 {
				out = append(out, "") // JS pushes undefined; unreachable here
				continue
			}
			out = append(out, s[loc[2*g]:loc[2*g+1]])
		}
		p, q = e, e
	}
	return append(out, s[p:])
}

func cleanUpBlocks(arr []string) []string {
	out := arr[:0:0]
	for _, s := range arr {
		s = jsTrim(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseBlocks(txt string) []string {
	// parse by /regex/ first
	arr := jsSplitRegexp(txt, bySlashesAt0, bySlashesAtN)
	var res []string
	// parse by (blocks), next
	for _, str := range arr {
		if isReg(str) {
			res = append(res, str)
			continue
		}
		res = append(res, jsSplitRegexp(str, byParentheses, byParentheses)...)
	}
	res = cleanUpBlocks(res)
	// split by spaces, now
	var final []string
	for _, str := range res {
		if isBlock(str) {
			final = append(final, str)
		} else if isReg(str) {
			final = append(final, str)
		} else {
			final = append(final, strings.Split(str, " ")...)
		}
	}
	return cleanUpBlocks(final)
}

// ---------------------------------------------------------------------------
// 02-parseToken.js

var (
	hasMinMax   = regexp.MustCompile(`\{([0-9]+)?(, *[0-9]*)?\}`)
	captureName = regexp.MustCompile(`^<[` + jsSpace + `]*([^` + jsSpace + `]+)[` + jsSpace + `]*>`)
	commaSpaces = regexp.MustCompile(`, *`)
)

// JS toUpperCase expansions (SpecialCasing.txt) for single BMP characters
// that Go's unicode.ToUpper maps 1:1 or not at all.
var jsUpperSpecial = map[rune]string{
	'ß': "SS", 'ŉ': "ʼN", 'ǰ': "J̌", 'ΐ': "Ϊ́", 'ΰ': "Ϋ́", 'և': "ԵՒ",
	'ﬀ': "FF", 'ﬁ': "FI", 'ﬂ': "FL", 'ﬃ': "FFI", 'ﬄ': "FFL", 'ﬅ': "ST", 'ﬆ': "ST",
}

// jsCharAtUpper is str.charAt(0).toUpperCase() together with the byte length
// of that first UTF-16 unit's character (0 for "", the full rune otherwise).
func jsCharAtUpper(s string) (string, int) {
	if s == "" {
		return "", 0
	}
	r, size := utf8.DecodeRuneInString(s)
	if r >= 0x10000 || r == utf8.RuneError {
		// charAt(0) is a lone surrogate, which toUpperCase leaves alone; the
		// concatenation with substring(1) rebuilds the original character.
		return s[:size], size
	}
	if up, ok := jsUpperSpecial[r]; ok {
		return up, size
	}
	return string(unicode.ToUpper(r)), size
}

// titleCase is `str.charAt(0).toUpperCase() + str.substring(1)`.
func titleCase(s string) string {
	up, size := jsCharAtUpper(s)
	return up + s[size:]
}

func strEnd(s string) byte {
	if s == "" {
		return 0
	}
	return s[len(s)-1]
}

func strStart(s string) byte {
	if s == "" {
		return 0
	}
	return s[0]
}

// stripStart/stripEnd remove one UTF-16 unit; every call site has already
// checked that unit is an ASCII flag character, so one byte.
func stripStart(s string) string {
	if s == "" {
		return ""
	}
	return s[1:]
}

func stripEnd(s string) string {
	if s == "" {
		return ""
	}
	return s[:len(s)-1]
}

func stripBoth(s string) string { return stripEnd(stripStart(s)) }

// jsNumber is Number(str) for a string of ASCII digits.
func jsNumber(digits string) float64 {
	if digits == "" {
		return 0
	}
	v, _ := strconv.ParseFloat(digits, 64) // overflow gives ±Inf like JS
	return v
}

// jsRegexpSource is RegExp.prototype.source for new RegExp(w): V8 escapes
// '/' outside character classes and line terminators, and reports "(?:)"
// for an empty pattern.
func jsRegexpSource(w string) string {
	if w == "" {
		return "(?:)"
	}
	var b strings.Builder
	inClass := false
	for i := 0; i < len(w); i++ {
		c := w[i]
		switch {
		case c == '\\' && i+1 < len(w):
			b.WriteByte(c)
			b.WriteByte(w[i+1])
			i++
			continue
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '/' && !inClass:
			b.WriteString(`\/`)
			continue
		case c == '\n':
			b.WriteString(`\n`)
			continue
		case c == '\r':
			b.WriteString(`\r`)
			continue
		}
		b.WriteByte(c)
	}
	s := b.String()
	s = strings.ReplaceAll(s, " ", ` `)
	return strings.ReplaceAll(s, " ", ` `)
}

// newUserRegexp is `new RegExp(w)` for a /regex/ token. A pattern RE2 cannot
// express (lookaround, backreference) becomes a regex that never matches.
func newUserRegexp(w string) *jsRegexp {
	re, err := compileJSRegexp(w, "")
	if err != nil {
		return &jsRegexp{source: jsRegexpSource(w), fn: func(string) bool { return false }}
	}
	re.source = jsRegexpSource(w)
	return re
}

func parseToken(w string) *reg {
	obj := &reg{}
	//collect any flags (do it twice)
	for i := 0; i < 2; i++ {
		//end-flag
		if strEnd(w) == '$' {
			obj.End = true
			w = stripEnd(w)
		}
		//front-flag
		if strStart(w) == '^' {
			obj.Start = true
			w = stripStart(w)
		}
		if strEnd(w) == '?' {
			obj.Optional = true
			w = stripEnd(w)
		}
		//capture group (this one can span multiple-terms)
		if strStart(w) == '[' || strEnd(w) == ']' {
			obj.Group, obj.HasGroup, obj.groupNull = "", false, true
			if strStart(w) == '[' {
				obj.GroupStart = true
			}
			if strEnd(w) == ']' {
				obj.GroupEnd = true
			}
			w = strings.TrimPrefix(w, "[")
			w = strings.TrimSuffix(w, "]")
			// Use capture group name
			if strStart(w) == '<' {
				res := captureName.FindStringSubmatch(w)
				if res == nil {
					// JS: `res.length` on a null exec result throws.
					panic(fmt.Sprintf("compromise: parseMatch: TypeError: Cannot read properties of null (reading 'length') for token %q", w))
				}
				obj.setGroupName(res[1])
				w = strings.Replace(w, res[0], "", 1)
			}
		}
		//back-flags
		if strEnd(w) == '+' {
			obj.Greedy = true
			w = stripEnd(w)
		}
		if w != "*" && strEnd(w) == '*' && w != `\*` {
			obj.Greedy = true
			w = stripEnd(w)
		}
		if strStart(w) == '!' {
			obj.Negative = true
			w = stripStart(w)
		}
		//soft-match
		if strStart(w) == '~' && strEnd(w) == '~' && jsLen(w) > 2 {
			w = stripBoth(w)
			obj.Fuzzy = true
			obj.setMin(0.85) // opts.fuzzy || 0.85
			if !strings.Contains(w, "(") {
				obj.Word = w
				return obj
			}
		}
		//regex
		if strStart(w) == '/' && strEnd(w) == '/' {
			w = stripBoth(w)
			obj.Regex = newUserRegexp(w)
			return obj
		}
		// support foo{1,9}
		if loc := hasMinMax.FindStringSubmatchIndex(w); loc != nil {
			bDef, cDef := loc[2] >= 0, loc[4] >= 0
			var b, c string
			if bDef {
				b = w[loc[2]:loc[3]]
			}
			if cDef {
				c = w[loc[4]:loc[5]]
			}
			if !cDef {
				// '{3}' Exactly three times
				if bDef {
					obj.setMin(jsNumber(b))
					obj.setMax(jsNumber(b))
				} else {
					// '{}': Number(undefined) is NaN
					obj.setMin(math.NaN())
					obj.setMax(math.NaN())
				}
			} else {
				// c.replace(/, */, '') — first match only
				if l := commaSpaces.FindStringIndex(c); l != nil {
					c = c[:l[0]] + c[l[1]:]
				}
				if !bDef {
					// '{,9}' implied zero min
					obj.setMin(0)
					obj.setMax(jsNumber(c))
				} else {
					// '{2,4}' Two to four times
					obj.setMin(jsNumber(b))
					// '{3,}' Three or more times
					if c == "" {
						obj.setMax(999)
					} else {
						obj.setMax(jsNumber(c))
					}
				}
			}
			// use same method as '+'
			obj.Greedy = true
			// 0 as min means the same as '?'
			if !jsTruthyNum(obj.minVal()) {
				obj.Optional = true
			}
			w = w[:loc[0]] + w[loc[1]:]
		}
		//wrapped-flags
		if strStart(w) == '(' && strEnd(w) == ')' {
			var parts []string
			// support (one && two)
			if strings.Contains(w, "&&") {
				parts = strings.Split(w, "&&")
				obj.Operator = "and"
			} else {
				parts = strings.Split(w, "|")
				obj.Operator = "or"
			}
			//remove '(' and ')'
			parts[0] = stripStart(parts[0])
			last := len(parts) - 1
			parts[last] = stripEnd(parts[last])
			// clean up the results
			obj.Choices = [][]*reg{}
			for _, s := range parts {
				s = jsTrim(s)
				if s == "" {
					continue
				}
				//recursion alert!
				words := strings.Split(s, " ")
				block := make([]*reg, 0, len(words))
				for _, sw := range words {
					block = append(block, parseToken(sw))
				}
				obj.Choices = append(obj.Choices, block)
			}
			w = ""
		}
		//root/sense overloaded
		if strStart(w) == '{' && strEnd(w) == '}' {
			w = stripBoth(w)
			obj.Root = w
			if strings.Contains(w, "/") {
				split := strings.Split(obj.Root, "/")
				obj.Root = split[0]
				obj.Pos = split[1]
				if obj.Pos == "adj" {
					obj.Pos = "Adjective"
				}
				// titlecase
				up, size := jsCharAtUpper(obj.Pos)
				obj.Pos = up + jsToLower(obj.Pos[size:])
				// add sense-number too
				if len(split) > 2 {
					obj.Sense = split[2]
				}
			}
			return obj
		}
		//chunks
		if strStart(w) == '<' && strEnd(w) == '>' {
			w = stripBoth(w)
			obj.Chunk = titleCase(w)
			obj.chunkSet = true
			obj.Greedy = true
			return obj
		}
		if strStart(w) == '%' && strEnd(w) == '%' {
			w = stripBoth(w)
			obj.Switch = w
			obj.switchSet = true
			return obj
		}
	}
	//do the actual token content
	if strStart(w) == '#' {
		obj.Tag = titleCase(stripStart(w))
		obj.tagSet = true
		return obj
	}
	//dynamic function on a term object
	if strStart(w) == '@' {
		obj.Method = stripStart(w)
		obj.methodSet = true
		return obj
	}
	if w == "." {
		obj.Anything = true
		return obj
	}
	//support alone-astrix
	if w == "*" {
		obj.Anything = true
		obj.Greedy = true
		obj.Optional = true
		return obj
	}
	if w != "" {
		//somehow handle encoded-chars? (String.replace with a string: first occurrence only)
		w = strings.Replace(w, `\*`, "*", 1)
		w = strings.Replace(w, `\.`, ".", 1)
		w = jsToLower(w)
		obj.Word = w
	}
	return obj
}

// ---------------------------------------------------------------------------
// 03-splitHyphens.js

// /[a-z0-9][-–—][a-z]/i — non-unicode JS case folding only adds A-Z (Go's
// (?i) would also fold ſ and the Kelvin sign), so spell the classes out.
var hasDashRe = regexp.MustCompile(`[a-zA-Z0-9][-–—][a-zA-Z]`)

// jsSplitDashes is word.split(/[-–—]/g).
func jsSplitDashes(w string) []string {
	var out []string
	last := 0
	for i, r := range w {
		if r == '-' || r == '–' || r == '—' {
			out = append(out, w[last:i])
			last = i + utf8.RuneLen(r)
		}
	}
	return append(out, w[last:])
}

// match 're-do' -> ['re','do']
func splitHyphens(regs []*reg, prefixes map[string]bool) []*reg {
	for i := len(regs) - 1; i >= 0; i-- {
		r := regs[i]
		if r.Word != "" && hasDashRe.MatchString(r.Word) {
			words := jsSplitDashes(r.Word)
			// don't split 're-cycle', etc
			if _, ok := prefixes[words[0]]; ok {
				continue
			}
			var keep []string
			for _, w := range words {
				if w != "" {
					keep = append(keep, w)
				}
			}
			// JS reverses then splices each copy in at i, so the net order is
			// the original order.
			repl := make([]*reg, 0, len(keep))
			for _, w := range keep {
				cp := *r // Object.assign({}, reg)
				cp.Word = w
				repl = append(repl, &cp)
			}
			out := make([]*reg, 0, len(regs)-1+len(repl))
			out = append(out, regs[:i]...)
			out = append(out, repl...)
			out = append(out, regs[i+1:]...)
			regs = out
		}
	}
	return regs
}

// ---------------------------------------------------------------------------
// 05-postProcess.js

// name any [unnamed] capture-groups with a number
func nameGroups(regs []*reg) []*reg {
	index := 0
	var inGroup *string // nil == null
	for _, token := range regs {
		if token.GroupStart {
			if token.Group != "" {
				g := token.Group
				inGroup = &g
			} else {
				g := strconv.Itoa(index)
				inGroup = &g
				index++
			}
		}
		if inGroup != nil {
			token.setGroupName(*inGroup)
		}
		if token.GroupEnd {
			inGroup = nil
		}
	}
	return regs
}

// optimize an 'or' lookup, when the (a|b|c) list is simple or multi-word
func doFastOrMode(tokens []*reg) []*reg {
	for _, token := range tokens {
		if token.Choices == nil || token.Operator != "or" || token.Fuzzy {
			continue
		}
		shouldPack := true
		for _, block := range token.Choices {
			if len(block) != 1 {
				shouldPack = false
				break
			}
			r := block[0]
			// reg.method !== true is always true (method is a string)
			if r.Fuzzy || r.Start || r.End || r.Word == "" || r.Negative || r.Optional {
				shouldPack = false
				break
			}
		}
		if shouldPack {
			words := make([]string, 0, len(token.Choices))
			for _, block := range token.Choices {
				words = append(words, block[0].Word)
			}
			token.setFastOr(words)
			token.Choices = nil
		}
	}
	return tokens
}

// support ~(a|b|c)~
func fuzzyOr(regs []*reg) []*reg {
	for _, r := range regs {
		if r.Fuzzy && r.Choices != nil {
			for _, block := range r.Choices {
				if len(block) == 1 && block[0].Word != "" {
					block[0].Fuzzy = true
					// r0.min = reg.min
					if r.HasMin {
						block[0].minF, block[0].minFOK, block[0].HasMin, block[0].Min = r.minVal(), true, true, r.Min
					} else {
						block[0].minF, block[0].minFOK, block[0].HasMin, block[0].Min = 0, false, false, 0
					}
				}
			}
		}
	}
	return regs
}

func postProcess(regs []*reg) []*reg {
	regs = nameGroups(regs)
	regs = doFastOrMode(regs)
	return fuzzyOr(regs)
}

// ---------------------------------------------------------------------------
// JSON: the shape JSON.stringify gives compromise's parsed regs, with Set ->
// array and RegExp -> {source, flags}.

type regexJSON struct {
	Source string `json:"source"`
	Flags  string `json:"flags"`
}

// UnmarshalJSON decodes a JSON-serialised compromise reg.
func (r *reg) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*r = reg{}
	str := func(k string, dst *string, set *bool) error {
		raw, ok := m[k]
		if !ok || string(raw) == "null" {
			return nil
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return fmt.Errorf("reg.%s: %w", k, err)
		}
		if set != nil {
			*set = true
		}
		return nil
	}
	boolean := func(k string, dst *bool) error {
		raw, ok := m[k]
		if !ok || string(raw) == "null" {
			return nil
		}
		if err := json.Unmarshal(raw, dst); err != nil {
			return fmt.Errorf("reg.%s: %w", k, err)
		}
		return nil
	}
	var dummy bool
	for _, f := range []struct {
		k   string
		dst *string
		set *bool
	}{
		{"word", &r.Word, &dummy}, {"tag", &r.Tag, &r.tagSet}, {"method", &r.Method, &r.methodSet},
		{"switch", &r.Switch, &r.switchSet}, {"chunk", &r.Chunk, &r.chunkSet}, {"root", &r.Root, nil},
		{"operator", &r.Operator, nil}, {"use", &r.Use, nil}, {"pos", &r.Pos, nil}, {"sense", &r.Sense, nil},
	} {
		if err := str(f.k, f.dst, f.set); err != nil {
			return err
		}
	}
	for _, f := range []struct {
		k   string
		dst *bool
	}{
		{"optional", &r.Optional}, {"greedy", &r.Greedy}, {"negative", &r.Negative}, {"start", &r.Start},
		{"end", &r.End}, {"anything", &r.Anything}, {"fuzzy", &r.Fuzzy}, {"groupStart", &r.GroupStart},
		{"groupEnd", &r.GroupEnd},
	} {
		if err := boolean(f.k, f.dst); err != nil {
			return err
		}
	}
	if raw, ok := m["group"]; ok {
		if string(raw) == "null" {
			r.groupNull = true
		} else {
			var g string
			if err := json.Unmarshal(raw, &g); err != nil {
				return fmt.Errorf("reg.group: %w", err)
			}
			r.setGroupName(g)
		}
	}
	num := func(k string, set func(float64)) error {
		raw, ok := m[k]
		if !ok {
			return nil
		}
		if string(raw) == "null" { // JSON.stringify(NaN)
			set(math.NaN())
			return nil
		}
		var v float64
		if err := json.Unmarshal(raw, &v); err != nil {
			return fmt.Errorf("reg.%s: %w", k, err)
		}
		set(v)
		return nil
	}
	if err := num("min", r.setMin); err != nil {
		return err
	}
	if err := num("max", r.setMax); err != nil {
		return err
	}
	if raw, ok := m["regex"]; ok && string(raw) != "null" {
		var rx regexJSON
		if err := json.Unmarshal(raw, &rx); err != nil {
			return fmt.Errorf("reg.regex: %w", err)
		}
		re, err := compileJSRegexp(rx.Source, rx.Flags)
		if err != nil {
			return fmt.Errorf("reg.regex: %w", err)
		}
		r.Regex = re
	}
	if raw, ok := m["fastOr"]; ok && string(raw) != "null" {
		var words []string
		if err := json.Unmarshal(raw, &words); err != nil {
			return fmt.Errorf("reg.fastOr: %w", err)
		}
		r.setFastOr(words)
	}
	if raw, ok := m["choices"]; ok && string(raw) != "null" {
		var choices [][]*reg
		if err := json.Unmarshal(raw, &choices); err != nil {
			return fmt.Errorf("reg.choices: %w", err)
		}
		if choices == nil {
			choices = [][]*reg{}
		}
		r.Choices = choices
	}
	return nil
}

// MarshalJSON produces the same shape JSON.stringify gives the JS reg (keys
// present exactly when JS sets them).
func (r *reg) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	putS := func(k, v string, set bool) {
		if v != "" || set {
			m[k] = v
		}
	}
	putS("word", r.Word, false)
	putS("tag", r.Tag, r.tagSet)
	putS("method", r.Method, r.methodSet)
	putS("switch", r.Switch, r.switchSet)
	putS("chunk", r.Chunk, r.chunkSet)
	putS("root", r.Root, false)
	putS("operator", r.Operator, false)
	putS("use", r.Use, false)
	putS("pos", r.Pos, false)
	putS("sense", r.Sense, false)
	for k, v := range map[string]bool{
		"optional": r.Optional, "greedy": r.Greedy, "negative": r.Negative, "start": r.Start, "end": r.End,
		"anything": r.Anything, "fuzzy": r.Fuzzy, "groupStart": r.GroupStart, "groupEnd": r.GroupEnd,
	} {
		if v {
			m[k] = true
		}
	}
	if r.Group != "" {
		m["group"] = r.Group
	} else if r.groupNull {
		m["group"] = nil
	}
	numOut := func(v float64) any {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		return v
	}
	if r.HasMin {
		m["min"] = numOut(r.minVal())
	}
	if r.HasMax {
		m["max"] = numOut(r.maxVal())
	}
	if r.Regex != nil {
		m["regex"] = regexJSON{Source: r.Regex.source}
	}
	if r.FastOr != nil {
		list := r.fastOrList
		if len(list) != len(r.FastOr) {
			list = list[:0:0]
			for w := range r.FastOr {
				list = append(list, w)
			}
		}
		if list == nil {
			list = []string{}
		}
		m["fastOr"] = list
	}
	if r.Choices != nil {
		m["choices"] = r.Choices
	}
	return json.Marshal(m)
}
