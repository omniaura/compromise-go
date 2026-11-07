package compromise

import (
	"regexp"
	"strings"
	"unicode"
)

// Port of compromise/src/1-one/tokenize: sentence splitting, word splitting,
// punctuation trimming and normalisation. Every step keeps the input's
// characters in Pre/Text/Post, so offsets can be recomputed by concatenation.

// --- sentences --------------------------------------------------------------

func isSentenceTerminal(r rune) bool {
	switch r {
	case '.', '!', '?', 0x203d, 0x2e18, 0x203c, 0x3002:
		return true
	}
	return r >= 0x2047 && r <= 0x2049
}

// splitNewlines is text.split(/((?:\r?\n|\r)+)/): pieces alternate with the
// captured newline runs.
func splitNewlines(text string) []string {
	var out []string
	last := 0
	i := 0
	for i < len(text) {
		if text[i] == '\n' || text[i] == '\r' {
			j := i
			for j < len(text) && (text[j] == '\n' || text[j] == '\r') {
				j++
			}
			out = append(out, text[last:i], text[i:j])
			last = j
			i = j
			continue
		}
		i++
	}
	return append(out, text[last:])
}

// splitInitSplit is line.split(/([.!?…]+\s)/) with the capture kept.
func splitInitSplit(line string) []string {
	var out []string
	last := 0
	rs := []rune(line)
	// work on byte offsets via a rune walk
	pos := make([]int, len(rs)+1)
	b := 0
	for i, r := range rs {
		pos[i] = b
		b += len(string(r))
	}
	pos[len(rs)] = b
	for i := 0; i < len(rs); {
		if isSentenceTerminal(rs[i]) {
			j := i
			for j < len(rs) && isSentenceTerminal(rs[j]) {
				j++
			}
			if j < len(rs) && isJSSpace(rs[j]) {
				out = append(out, line[pos[last]:pos[i]], line[pos[i]:pos[j+1]])
				last = j + 1
				i = j + 1
				continue
			}
			i = j
			continue
		}
		i++
	}
	return append(out, line[pos[last]:])
}

func isSplitsOnly(s string) bool {
	rs := []rune(s)
	if len(rs) < 2 || !isJSSpace(rs[len(rs)-1]) {
		return false
	}
	for _, r := range rs[:len(rs)-1] {
		if !isSentenceTerminal(r) {
			return false
		}
	}
	return true
}

func basicSplit(text string) []string {
	var all []string
	for _, line := range splitNewlines(text) {
		arr := splitInitSplit(line)
		for o := 0; o < len(arr); o++ {
			if o+1 < len(arr) && arr[o+1] != "" && isSplitsOnly(arr[o+1]) {
				arr[o] += arr[o+1]
				arr[o+1] = ""
			}
			if arr[o] != "" {
				all = append(all, arr[o])
			}
		}
	}
	return all
}

// hasLetterLoose is /[a-z0-9À-ÿ©® -㌀퀀-\udfff]/i
// evaluated over UTF-16 units (astral characters are surrogate pairs, which
// fall in 퀀-\udfff).
func hasLetterLoose(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return true
		case r >= 0xc0 && r <= 0xff, r == 0xa9, r == 0xae:
			return true
		case r >= 0x2000 && r <= 0x3300, r >= 0xd000 && r <= 0xdfff, r >= 0x10000:
			return true
		}
	}
	return false
}

func hasNonSpace(s string) bool {
	for _, r := range s {
		if !isJSSpace(r) {
			return true
		}
	}
	return false
}

func simpleMerge(splits []string) []string {
	var chunks []string
	for i := 0; i < len(splits); i++ {
		s := splits[i]
		if s == "" {
			continue
		}
		if !hasNonSpace(s) || !hasLetterLoose(s) {
			if len(chunks) > 0 {
				chunks[len(chunks)-1] += s
				continue
			} else if i+1 < len(splits) && splits[i+1] != "" {
				splits[i+1] = s + splits[i+1]
				continue
			}
		}
		chunks = append(chunks, s)
	}
	return chunks
}

var (
	reIsAcronymEnd = regexp.MustCompile(`(?i)[ .][A-Z]\.? *$`)
	reHasEllipse   = regexp.MustCompile(`(?:\x{2026}|\.{2,}) *$`)
	reHasPeriod    = regexp.MustCompile(`\. *$`)
	reLeadInit     = regexp.MustCompile(`^[A-Z]\. $`)
	reTrailPunct   = regexp.MustCompile(`[.!?\x{203D}\x{2E18}\x{203C}\x{2047}-\x{2049}] *$`)
)

func hasUnicodeLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func isSentence(str string, abbrevs map[string]bool) bool {
	if !hasUnicodeLetter(str) {
		return false
	}
	if reIsAcronymEnd.MatchString(str) {
		return false
	}
	if jsLen(str) == 3 && reLeadInit.MatchString(str) {
		return false
	}
	if reHasEllipse.MatchString(str) {
		return false
	}
	txt := reTrailPunct.ReplaceAllString(str, "")
	words := strings.Split(txt, " ")
	lastWord := jsToLower(words[len(words)-1])
	if abbrevs[lastWord] && reHasPeriod.MatchString(str) {
		return false
	}
	return true
}

func smartMerge(chunks []string, abbrevs map[string]bool) []string {
	var sentences []string
	for i := 0; i < len(chunks); i++ {
		c := chunks[i]
		if i+1 < len(chunks) && chunks[i+1] != "" && !isSentence(c, abbrevs) && !strings.HasSuffix(c, "\n") {
			chunks[i+1] = c + chunks[i+1]
		} else if c != "" {
			sentences = append(sentences, c)
			chunks[i] = ""
		}
	}
	return sentences
}

var quotePairs = map[rune]rune{
	0x22: 0x22, 0xff02: 0xff02, 0x201c: 0x201d, 0x201f: 0x201d, 0x201e: 0x201d,
	0x2e42: 0x201d, 0x201a: 0x2019, 0xab: 0xbb, 0x2039: 0x203a, 0x2035: 0x2032,
	0x2036: 0x2033, 0x2037: 0x2034, 0x301d: 0x301e, 0x301f: 0x301e,
}

var closeQuotes = func() map[rune]bool {
	m := map[rune]bool{}
	for _, v := range quotePairs {
		m[v] = true
	}
	return m
}()

func countRunes(s string, set func(rune) bool) int {
	n := 0
	for _, r := range s {
		if set(r) {
			n++
		}
	}
	return n
}

func isOpenQuote(r rune) bool  { _, ok := quotePairs[r]; return ok }
func isCloseQuote(r rune) bool { return closeQuotes[r] }

func closesQuote(s string) bool {
	return s != "" && countRunes(s, isCloseQuote) == 1
}

const maxQuote = 280

func quoteMerge(splits []string) []string {
	var arr []string
	for i := 0; i < len(splits); i++ {
		split := splits[i]
		if countRunes(split, isOpenQuote) == 1 {
			if i+1 < len(splits) && closesQuote(splits[i+1]) && jsLen(splits[i+1]) < maxQuote {
				splits[i] += splits[i+1]
				arr = append(arr, splits[i])
				splits[i+1] = ""
				i++
				continue
			}
			if i+2 < len(splits) && closesQuote(splits[i+2]) {
				toAdd := splits[i+1] + splits[i+2]
				if jsLen(toAdd) < maxQuote {
					splits[i] += toAdd
					arr = append(arr, splits[i])
					splits[i+1] = ""
					splits[i+2] = ""
					i += 2
					continue
				}
			}
		}
		arr = append(arr, splits[i])
	}
	return arr
}

const maxParens = 250

func parensMerge(splits []string) []string {
	var arr []string
	for i := 0; i < len(splits); i++ {
		split := splits[i]
		if strings.Count(split, "(") == 1 {
			if i+1 < len(splits) && splits[i+1] != "" && jsLen(splits[i+1]) < maxParens {
				if strings.Contains(splits[i+1], ")") && !strings.Contains(splits[i+1], "(") {
					splits[i] += splits[i+1]
					arr = append(arr, splits[i])
					splits[i+1] = ""
					i++
					continue
				}
			}
		}
		arr = append(arr, splits[i])
	}
	return arr
}

func leadingSpace(s string) int {
	for i, r := range s {
		if !isJSSpace(r) {
			return i
		}
	}
	return len(s)
}

func splitSentences(text string, m *model) []string {
	if !hasNonSpace(text) {
		return nil
	}
	// JS String.replace with a string pattern replaces the first match only.
	text = strings.Replace(text, " ", " ", 1)
	splits := basicSplit(text)
	sentences := simpleMerge(splits)
	sentences = smartMerge(sentences, m.abbreviations)
	sentences = quoteMerge(sentences)
	sentences = parensMerge(sentences)
	if len(sentences) == 0 {
		return []string{text}
	}
	for i := 1; i < len(sentences); i++ {
		if n := leadingSpace(sentences[i]); n > 0 {
			sentences[i-1] += sentences[i][:n]
			sentences[i] = sentences[i][n:]
		}
	}
	return sentences
}

// --- words ------------------------------------------------------------------

func isDash(r rune) bool { return r == '-' || r == 0x2013 || r == 0x2014 }

// splitOnDashes is str.split(/[-–—]/), keeping empty pieces.
func splitOnDashes(s string) []string {
	var out []string
	last := 0
	for i, r := range s {
		if isDash(r) {
			out = append(out, s[last:i])
			last = i + len(string(r))
		}
	}
	return append(out, s[last:])
}

var (
	reHyphenWord = regexp.MustCompile(`(?i)^([a-z\x{C0}-\x{FF}` + "`" + `"'/]+)[-–—]([a-z0-9\x{C0}-\x{FF}].*)`)
	reHyphenNum  = regexp.MustCompile(`(?i)^[('"]?([0-9]{1,4})[-–—]([a-z\x{C0}-\x{FF}` + "`" + `"'/-]+[)'"]?$)`)
	reOneLetter  = regexp.MustCompile(`(?i)[a-z]`)
	reTrailStop  = regexp.MustCompile(`[.?!]$`)
)

func hasHyphen(str string, m *model) bool {
	parts := splitOnDashes(str)
	if len(parts) <= 1 {
		return false
	}
	if jsLen(parts[0]) == 1 && reOneLetter.MatchString(parts[0]) {
		return false
	}
	if m.prefixes[parts[0]] {
		return false
	}
	p1 := reTrailStop.ReplaceAllString(jsTrim(parts[1]), "")
	if m.suffixes[p1] {
		return false
	}
	if reHyphenWord.MatchString(str) {
		return true
	}
	if reHyphenNum.MatchString(str) {
		return true
	}
	return false
}

func splitHyphenatedWord(word string) []string {
	hyphens := splitOnDashes(word)
	whichDash := "-"
	for _, r := range word {
		if isDash(r) {
			whichDash = string(r)
			break
		}
	}
	out := make([]string, 0, len(hyphens))
	for o, h := range hyphens {
		if o == len(hyphens)-1 {
			out = append(out, h)
		} else {
			out = append(out, h+whichDash)
		}
	}
	return out
}

// naiveSplit is str.split(/(\S+)/): alternating whitespace and words, starting
// and ending with (possibly empty) whitespace.
func naiveSplit(str string) []string {
	var out []string
	last := 0
	inWord := false
	start := 0
	for i, r := range str {
		sp := isJSSpace(r)
		if !inWord && !sp {
			out = append(out, str[last:i])
			start = i
			inWord = true
		} else if inWord && sp {
			out = append(out, str[start:i])
			last = i
			inWord = false
		}
	}
	if inWord {
		out = append(out, str[start:])
		out = append(out, "")
	} else {
		out = append(out, str[last:])
	}
	return out
}

var notWord = map[string]bool{
	".": true, "?": true, "!": true, ":": true, ";": true, "-": true, "–": true, "—": true,
	"--": true, "...": true, "(": true, ")": true, "[": true, "]": true, `"`: true, "'": true,
	"`": true, "«": true, "»": true, "*": true, "•": true,
}

func isBoundary(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != '!' && s[i] != '?' && s[i] != '.' {
			return false
		}
	}
	return true
}

var (
	reIsSlash    = regexp.MustCompile(`\p{L} ?/ ?\p{L}+$`)
	reStartRange = regexp.MustCompile(`^[0-9]{1,4}(:[0-9][0-9])?([a-z]{1,2})? ?[-–—] ?$`)
	reEndRange   = regexp.MustCompile(`^[0-9]{1,4}([a-z]{1,2})? ?$`)
)

func splitWords(str string, m *model) []string {
	var arr []string
	for _, w := range naiveSplit(str) {
		if hasHyphen(w, m) {
			arr = append(arr, splitHyphenatedWord(w)...)
			continue
		}
		arr = append(arr, w)
	}
	var result []string
	carry := ""
	for _, word := range arr {
		if hasNonSpace(word) && !notWord[word] && !isBoundary(word) {
			if len(result) > 0 {
				result[len(result)-1] += carry
				result = append(result, word)
			} else {
				result = append(result, carry+word)
			}
			carry = ""
		} else {
			carry += word
		}
	}
	if carry != "" {
		if len(result) == 0 {
			result = append(result, "")
		}
		result[len(result)-1] += carry
	}
	// combineSlashes: null entries are represented by a parallel flag
	alive := make([]bool, len(result))
	for i := range alive {
		alive[i] = true
	}
	for i := 1; i < len(result)-1; i++ {
		if alive[i] && reIsSlash.MatchString(result[i]) {
			result[i-1] += result[i] + result[i+1]
			alive[i] = false
			alive[i+1] = false
			result[i] = ""
			result[i+1] = ""
		}
	}
	// combineRanges
	for i := 0; i < len(result)-1; i++ {
		if alive[i+1] && result[i+1] != "" && reStartRange.MatchString(result[i]) && reEndRange.MatchString(result[i+1]) {
			result[i] = result[i] + result[i+1]
			alive[i+1] = false
			result[i+1] = ""
		}
	}
	out := result[:0]
	for i, s := range result {
		if alive[i] && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// --- punctuation ------------------------------------------------------------

var (
	reHasAcronymTok = regexp.MustCompile(`(?i)^[a-z]\.([a-z]\.)+`)
	reChillin       = regexp.MustCompile(`[sn]['’]$`)
	reIsFullNumber  = regexp.MustCompile(`^[(+\-]?\d+(th|st|nd|rd)?[)+\-]?$`)
)

func isLetterOrNumber(r rune) bool {
	return unicode.IsLetter(r) || unicode.In(r, unicode.N, unicode.Sc)
}

func normalizePunctuation(str string, m *model) (text, pre, post string) {
	original := str
	chars := []rune(str)
	if m.emoticons[jsTrim(str)] {
		return jsTrim(str), "", " "
	}
	start := 0
	for start < len(chars) {
		c := chars[start]
		if m.prePunctuation[string(c)] {
			// JS `continue`s without consuming; the loop then re-reads chars[0]
			// until its counter runs out, so a kept char ends the scan.
			break
		}
		if (c == '+' || c == '-' || c == '(') && reIsFullNumber.MatchString(jsTrim(str)) {
			break
		}
		if isLetterOrNumber(c) {
			break
		}
		start++
	}
	end := len(chars)
	for end > start {
		c := chars[end-1]
		if m.postPunctuation[string(c)] {
			break
		}
		if isLetterOrNumber(c) {
			break
		}
		if c == '.' && reHasAcronymTok.MatchString(original) {
			break
		}
		if c == '\'' && reChillin.MatchString(original) {
			break
		}
		if (c == '+' || c == ')') && reIsFullNumber.MatchString(jsTrim(str)) {
			break
		}
		end--
	}
	pre = string(chars[:start])
	post = string(chars[end:])
	text = string(chars[start:end])
	if text == "" {
		// original.replace(/ *$/, after => { post = after })
		trimmed := strings.TrimRight(original, " ")
		post = original[len(trimmed):]
		text = trimmed
		pre = ""
	}
	return text, pre, post
}

// --- normalisation ----------------------------------------------------------

var (
	reTrailingPunct = regexp.MustCompile(`[,;.!?]+$`)
	reEllipsis3     = regexp.MustCompile(`\.{3,}$`)
	reTrailQuote    = regexp.MustCompile(`[",.!:;?)]+$`)
	reLeadQuote     = regexp.MustCompile(`^['"(]+`)
	reZeroWidth     = regexp.MustCompile(`[\x{200B}-\x{200D}\x{FEFF}]`)
	reNumberComma   = regexp.MustCompile(`([0-9]),([0-9])`)
)

// replaceAllNonOverlapping mimics JS /g replace with $1$2 for ([0-9]),([0-9]):
// RE2's ReplaceAll matches the same non-overlapping spans as V8.
func cleanNormal(str string) string {
	str = jsTrim(jsToLower(str))
	original := str
	str = reTrailingPunct.ReplaceAllString(str, "")
	str = strings.ReplaceAll(str, "…", "...")
	str = strings.ReplaceAll(str, "–", "-")
	if !(strings.HasPrefix(str, ":") || strings.HasPrefix(str, ";")) {
		str = reEllipsis3.ReplaceAllString(str, "")
		str = reTrailQuote.ReplaceAllString(str, "")
		str = reLeadQuote.ReplaceAllString(str, "")
	}
	str = reZeroWidth.ReplaceAllString(str, "")
	str = jsTrim(str)
	if str == "" {
		str = original
	}
	str = reNumberComma.ReplaceAllString(str, "$1$2")
	return str
}

func killUnicode(str string, m *model) string {
	if isASCII(str) {
		return str
	}
	var b strings.Builder
	for _, r := range str {
		if v, ok := m.unicode[r]; ok {
			b.WriteString(v)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var (
	rePeriodAcronym    = regexp.MustCompile(`([A-Z]\.)+[A-Z]?,?$`)
	reOneLetterAcronym = regexp.MustCompile(`^[A-Z]\.,?$`)
	reNoPeriodAcronym  = regexp.MustCompile(`[A-Z]{2,}('s|,)?$`)
	reLowerAcronym     = regexp.MustCompile(`([a-z]\.)+[a-z]\.?$`)
)

func doAcronym(str string) string {
	if rePeriodAcronym.MatchString(str) || reLowerAcronym.MatchString(str) ||
		reOneLetterAcronym.MatchString(str) || reNoPeriodAcronym.MatchString(str) {
		return strings.ReplaceAll(str, ".", "")
	}
	return str
}

func normalizeTerm(t *Term, m *model) {
	str := cleanNormal(t.Text)
	str = killUnicode(str, m)
	str = doAcronym(str)
	t.Normal = str
}

// --- parse ------------------------------------------------------------------

func parseTerms(sentence string, m *model) []*Term {
	words := splitWords(sentence, m)
	terms := make([]*Term, len(words))
	for i, w := range words {
		text, pre, post := normalizePunctuation(w, m)
		t := &Term{Text: text, Pre: pre, Post: post}
		normalizeTerm(t, m)
		terms[i] = t
	}
	return terms
}

// fromString is methods.one.tokenize.fromString.
func fromString(input string, m *model) [][]*Term {
	sentences := splitSentences(input, m)
	doc := make([][]*Term, 0, len(sentences))
	for _, s := range sentences {
		doc = append(doc, parseTerms(s, m))
	}
	return doc
}

// --- per-term computes ------------------------------------------------------

var (
	reAliasDomain = regexp.MustCompile(`(?i)[a-z]\.[a-z]`)
	reAliasMath   = regexp.MustCompile(`[0-9]`)
)

func computeAlias(t *Term, m *model) {
	str := t.Normal
	if str == "" {
		str = t.Text
	}
	if str == "" {
		str = t.Machine
	}
	if v, ok := m.aliases[str]; ok {
		t.Alias = append(t.Alias, v)
	}
	if strings.Contains(str, "/") && !reAliasDomain.MatchString(str) && !reAliasMath.MatchString(str) {
		arr := strings.Split(str, "/")
		if len(arr) <= 3 {
			for _, w := range arr {
				w = jsTrim(w)
				if w != "" {
					t.Alias = append(t.Alias, w)
				}
			}
		}
	}
}

var (
	reMachineDash   = regexp.MustCompile(`^\p{L}+-\p{L}+$`)
	reApostropheS   = regexp.MustCompile(`['’]s$`)
	reSApostrophe   = regexp.MustCompile(`s['’]$`)
	reLookin        = regexp.MustCompile(`([aeiou][ktrp])in'$`)
	reHashOrMention = regexp.MustCompile(`^[#@]`)
)

func computeMachine(t *Term) {
	str := t.Implicit
	if str == "" {
		str = t.Normal
	}
	if str == "" {
		str = t.Text
	}
	str = reApostropheS.ReplaceAllString(str, "")
	str = reSApostrophe.ReplaceAllString(str, "s")
	str = reLookin.ReplaceAllString(str, "${1}ing")
	if reMachineDash.MatchString(str) {
		str = strings.ReplaceAll(str, "-", "")
	}
	str = reHashOrMention.ReplaceAllString(str, "")
	if str != t.Normal {
		t.Machine = str
	}
}
