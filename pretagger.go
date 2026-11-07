package compromise

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Port of compromise/src/2-two/preTagger/compute/tagger and
// methods/quickSplit.

func quickSplitOn(terms []*Term, i int) bool {
	if i >= len(terms) {
		return false
	}
	t := terms[i]
	maybeDate := func(s string) bool { return s == "may" || s == "april" || s == "august" || s == "jan" }
	if t.Normal == "like" || maybeDate(t.Normal) {
		return false
	}
	if t.Tags.Has("Place") || t.Tags.Has("Date") {
		return false
	}
	if i > 0 {
		last := terms[i-1]
		if last.Tags.Has("Date") || maybeDate(last.Normal) {
			return false
		}
		if last.Tags.Has("Adjective") || t.Tags.Has("Adjective") {
			return false
		}
	}
	str := t.Normal
	if l := jsLen(str); l == 1 || l == 2 || l == 4 {
		if isAllDigits(str) {
			return false
		}
	}
	return true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func quickSplit(document [][]*Term) [][]*Term {
	var arr [][]*Term
	for _, terms := range document {
		start := 0
		for i, t := range terms {
			if strings.ContainsAny(t.Post, ",:;") && quickSplitOn(terms, i+1) {
				arr = append(arr, terms[start:i+1])
				start = i + 1
			}
		}
		if start < len(terms) {
			arr = append(arr, terms[start:])
		}
	}
	return arr
}

func isYelling(terms []*Term) bool {
	notProper := 0
	for _, t := range terms {
		if !t.Tags.Has("ProperNoun") {
			notProper++
		}
	}
	if notProper <= 3 {
		return false
	}
	for _, t := range terms {
		if t.Text != "" && t.Text[0] >= 'a' && t.Text[0] <= 'z' {
			return false
		}
	}
	return true
}

func (m *model) preTagger(docs [][]*Term) {
	for _, terms := range docs {
		m.colons(terms)
	}
	for _, terms := range quickSplit(docs) {
		yelling := isYelling(terms)
		m.secondPass(terms, yelling)
		m.thirdPass(terms, yelling)
	}
}

func (m *model) secondPass(terms []*Term, yelling bool) {
	for i, t := range terms {
		if t.Frozen {
			continue
		}
		m.tagSwitch(terms, i)
		if !yelling {
			m.checkCase(terms, i)
		}
		m.checkSuffix(terms, i)
		m.checkRegex(terms, i)
		m.checkPrefix(terms, i)
		tagYear(terms, i)
	}
}

func (m *model) thirdPass(terms []*Term, yelling bool) {
	for i := range terms {
		found := m.checkAcronym(terms, i)
		m.fillTags(terms, i)
		if !found {
			found = m.tagNeighbours(terms, i)
		}
		if !found {
			m.nounFallback(terms, i)
		}
	}
	for i, t := range terms {
		if t.Frozen {
			continue
		}
		m.tagOrgWords(terms, i, yelling)
		m.tagPlaceWords(terms, i, yelling)
		m.switchTag(terms, i)
		m.verbType(terms, i)
		m.hyphens(terms, i)
	}
	m.imperative(terms)
}

// --- 1st pass ---------------------------------------------------------------

func (m *model) colons(terms []*Term) {
	if len(terms) >= 3 && strings.Contains(terms[0].Post, ":") {
		next := terms[1]
		if next.Tags.Has("Value") || next.Tags.Has("Email") || next.Tags.Has("PhoneNumber") {
			return
		}
		m.setTag(terms[:1], "Expression", false)
	}
}

func (m *model) hyphens(terms []*Term, i int) {
	if terms[i].Post == "-" && i+1 < len(terms) {
		m.setTag(terms[i:i+2], "Hyphenated", false)
	}
}

// --- 2nd pass ---------------------------------------------------------------

var reSwitchPrefix = regexp.MustCompile(`^(under|over|mis|re|un|dis|semi)-?`)

func (m *model) tagSwitch(terms []*Term, i int) {
	t := terms[i]
	if sw, ok := m.switches[t.Normal]; ok {
		t.Switch = sw
		return
	}
	if reSwitchPrefix.MatchString(t.Normal) {
		stem := reSwitchPrefix.ReplaceAllString(t.Normal, "")
		if sw, ok := m.switches[stem]; ok && jsLen(stem) > 3 {
			t.Switch = sw
		}
	}
}

var (
	reTitleCase     = regexp.MustCompile(`^\p{Lu}[\p{Ll}'’]`)
	reHasNumber     = regexp.MustCompile(`[0-9]`)
	reHasIVX        = regexp.MustCompile(`[IVX]`)
	reRomanNumeral  = regexp.MustCompile(`^[IVXLCDM]{2,}$`)
	reRomanNumValid = regexp.MustCompile(`^M{0,4}(CM|CD|D?C{0,3})(XC|XL|L?X{0,3})(IX|IV|V?I{0,3})$`)
	notProperTags   = []string{"Date", "Month", "WeekDay", "Unit", "Expression"}
	romanNope       = map[string]bool{"li": true, "dc": true, "md": true, "dm": true, "ml": true}
)

func (m *model) checkCase(terms []*Term, i int) {
	t := terms[i]
	str := t.Text
	if t.Index[1] != 0 && reTitleCase.MatchString(str) && !reHasNumber.MatchString(str) {
		for _, tg := range notProperTags {
			if t.Tags.Has(tg) {
				return
			}
		}
		if strings.HasSuffix(t.Pre, `"`) || strings.HasSuffix(t.Pre, "'") {
			return
		}
		if t.Normal == "the" {
			return
		}
		m.fillTags(terms, i)
		if !t.Tags.Has("Noun") && !t.Frozen {
			t.Tags.Clear()
		}
		fastTag(t, "ProperNoun")
		return
	}
	if jsLen(str) >= 2 && reRomanNumeral.MatchString(str) && reHasIVX.MatchString(str) && reRomanNumValid.MatchString(str) && !romanNope[t.Normal] {
		fastTag(t, "RomanNumeral")
	}
}

func affixLookup(patterns []map[string]string, s string) (string, bool) {
	n := jsLen(s)
	if n >= len(patterns) || patterns[n] == nil {
		return "", false
	}
	tag, ok := patterns[n][s]
	return tag, ok
}

func suffixLoop(str string, patterns []map[string]string) string {
	n := jsLen(str)
	max := 7
	if n <= max {
		max = n - 1
	}
	for i := max; i > 1; i-- {
		if tag, ok := affixLookup(patterns, jsSubstring(str, n-i, n)); ok {
			return tag
		}
	}
	return ""
}

func (m *model) checkSuffix(terms []*Term, i int) {
	t := terms[i]
	if t.Tags.Size() != 0 {
		return
	}
	if tag := suffixLoop(t.Normal, m.suffixPatterns); tag != "" {
		fastTag(t, tag)
		return
	}
	if t.Implicit != "" {
		if tag := suffixLoop(t.Implicit, m.suffixPatterns); tag != "" {
			fastTag(t, tag)
		}
	}
}

func hasApostropheChar(s string) bool {
	return strings.ContainsAny(s, "'‘’‛‵′`´")
}

func doRegs(str string, regs []tagRegex) *tagRegex {
	for i := range regs {
		if regs[i].re.MatchString(str) {
			return &regs[i]
		}
	}
	return nil
}

func (m *model) checkRegex(terms []*Term, i int) {
	t := terms[i]
	normal := t.machineOrNormal()
	text := t.Text
	if hasApostropheChar(t.Post) && !hasApostropheChar(t.Pre) {
		text += jsTrim(t.Post)
	}
	arr := doRegs(text, m.regexText)
	if arr == nil {
		arr = doRegs(normal, m.regexNormal)
	}
	if arr == nil && reHasNumber.MatchString(normal) {
		arr = doRegs(normal, m.regexNumbers)
	}
	if arr == nil && t.Tags.Size() == 0 {
		if ch, ok := jsLastChar(normal); ok {
			arr = doRegs(normal, m.endsWith[ch])
		}
	}
	if arr != nil {
		m.setTags([]*Term{t}, arr.tag, false)
	}
}

func prefixLoop(str string, patterns []map[string]string) string {
	n := jsLen(str)
	max := 7
	if max > n-3 {
		max = n - 3
	}
	for i := max; i > 2; i-- {
		if tag, ok := affixLookup(patterns, jsSubstring(str, 0, i)); ok {
			return tag
		}
	}
	return ""
}

func (m *model) checkPrefix(terms []*Term, i int) {
	t := terms[i]
	if t.Tags.Size() != 0 {
		return
	}
	if tag := prefixLoop(t.Normal, m.prefixPatterns); tag != "" {
		fastTag(t, tag)
	}
}

var yearDateWords = map[string]bool{
	"in": true, "on": true, "by": true, "until": true, "for": true, "to": true, "during": true,
	"throughout": true, "through": true, "within": true, "before": true, "after": true, "of": true,
	"this": true, "next": true, "last": true, "circa": true, "around": true, "post": true, "pre": true,
	"budget": true, "classic": true, "plan": true, "may": true,
}

func hasDateTag(t *Term) bool {
	return t.Tags.Has("Date") || t.Tags.Has("Month") || t.Tags.Has("WeekDay") || t.Tags.Has("Year")
}

func termAt(terms []*Term, i int) *Term {
	if i < 0 || i >= len(terms) {
		return nil
	}
	return terms[i]
}

func tagYear(terms []*Term, i int) {
	t := terms[i]
	if !(t.Tags.Has("NumericValue") && t.Tags.Has("Cardinal") && jsLen(t.Normal) == 4) {
		return
	}
	num, err := strconv.Atoi(t.Normal)
	if err != nil || num == 0 || !(num > 1400 && num < 2100) {
		return
	}
	last, next := termAt(terms, i-1), termAt(terms, i+1)
	seemsGood := func(x *Term) bool {
		if x == nil {
			return false
		}
		if yearDateWords[x.normalOrImplicit()] {
			return true
		}
		return hasDateTag(x) || x.Tags.Has("ProperNoun")
	}
	seemsOkay := func(x *Term) bool {
		if x == nil {
			return false
		}
		if x.Tags.Has("Ordinal") {
			return true
		}
		if x.Tags.Has("Cardinal") && jsLen(x.Normal) < 3 {
			return true
		}
		return x.Normal == "is" || x.Normal == "was"
	}
	seemsFine := func(x *Term) bool { return x != nil && hasDateTag(x) }
	if seemsGood(last) || seemsGood(next) {
		fastTag(t, "Year")
		return
	}
	if num >= 1920 && num < 2025 {
		if seemsOkay(last) || seemsOkay(next) {
			fastTag(t, "Year")
			return
		}
		if seemsFine(termAt(terms, i-2)) || seemsFine(termAt(terms, i+2)) {
			fastTag(t, "Year")
			return
		}
		if last != nil && (last.Tags.Has("Determiner") || last.Tags.Has("Possessive")) {
			if next != nil && next.Tags.Has("Noun") && !next.Tags.Has("Plural") {
				fastTag(t, "Year")
			}
		}
	}
}

// --- 3rd pass ---------------------------------------------------------------

var (
	reAcrOneLetter   = regexp.MustCompile(`^[A-Z]('s|,)?$`)
	reAcrUpper       = regexp.MustCompile(`^[A-Z-]+$`)
	reAcrUpperThenS  = regexp.MustCompile(`^[A-Z]+s$`)
	acrOneLetterWord = map[string]bool{"I": true, "A": true}
	acrPlaces        = map[string]bool{"la": true, "ny": true, "us": true, "dc": true, "gb": true, "uk": true}
)

func (m *model) isNoPeriodAcronym(t *Term) bool {
	str := t.Text
	if !reAcrUpper.MatchString(str) {
		if jsLen(str) > 3 && reAcrUpperThenS.MatchString(str) {
			str = strings.TrimSuffix(str, "s")
		} else {
			return false
		}
	} else if acrPlaces[t.Normal] {
		return true
	}
	if jsLen(str) > 5 {
		return false
	}
	if acrOneLetterWord[str] {
		return false
	}
	if _, ok := m.lexicon[t.Normal]; ok {
		return false
	}
	return rePeriodAcronym.MatchString(str) || reLowerAcronym.MatchString(str) ||
		reAcrOneLetter.MatchString(str) || reNoPeriodAcronym.MatchString(str)
}

func (m *model) checkAcronym(terms []*Term, i int) bool {
	t := terms[i]
	if t.Tags.Has("RomanNumeral") || t.Tags.Has("Acronym") || t.Frozen {
		return false
	}
	if m.isNoPeriodAcronym(t) {
		t.Tags.Clear()
		fastTag(t, "Acronym", "Noun")
		if acrPlaces[t.Normal] {
			fastTag(t, "Place")
		}
		if reAcrUpperThenS.MatchString(t.Text) {
			fastTag(t, "Plural")
		}
		return true
	}
	if !acrOneLetterWord[t.Text] && reAcrOneLetter.MatchString(t.Text) {
		t.Tags.Clear()
		fastTag(t, "Acronym", "Noun")
		return true
	}
	if t.Tags.Has("Organization") && jsLen(t.Text) <= 3 {
		fastTag(t, "Acronym")
		return true
	}
	if t.Tags.Has("Organization") && reAcrUpper.MatchString(t.Text) && jsLen(t.Text) <= 6 {
		fastTag(t, "Acronym")
		return true
	}
	return false
}

func lookAtWord(t *Term, words [][2]string) string {
	if t == nil {
		return ""
	}
	for _, a := range words {
		if t.Normal == a[0] {
			return a[1]
		}
	}
	return ""
}

func lookAtTag(t *Term, tags [][2]string) string {
	if t == nil {
		return ""
	}
	for _, a := range tags {
		if t.Tags.Has(a[0]) {
			return a[1]
		}
	}
	return ""
}

func (m *model) tagNeighbours(terms []*Term, i int) bool {
	t := terms[i]
	if t.Tags.Size() != 0 {
		return false
	}
	nb := &m.neighbours
	tag := lookAtWord(termAt(terms, i-1), nb.LeftWords)
	if tag == "" {
		tag = lookAtWord(termAt(terms, i+1), nb.RightWords)
	}
	if tag == "" {
		tag = lookAtTag(termAt(terms, i-1), nb.LeftTags)
	}
	if tag == "" {
		tag = lookAtTag(termAt(terms, i+1), nb.RightTags)
	}
	if tag != "" {
		fastTag(t, tag)
		m.fillTags(terms, i)
		return true
	}
	return false
}

func (m *model) nounFallback(terms []*Term, i int) {
	tags := &terms[i].Tags
	empty := tags.Size() == 0
	if tags.Size() == 1 && (tags.Has("Hyphenated") || tags.Has("HashTag") || tags.Has("Prefix") || tags.Has("SlashedTerm")) {
		empty = true
	}
	if empty {
		fastTag(terms[i], "Noun")
		m.fillTags(terms, i)
	}
}

func isTitleCaseText(s string) bool { return reTitleCase.MatchString(s) }

func (m *model) tagOrgWords(terms []*Term, i int, yelling bool) {
	isOrg := func(k int) bool {
		t := termAt(terms, k)
		if t == nil {
			return false
		}
		if t.Tags.Has("FirstName") || t.Tags.Has("Place") {
			return false
		}
		if t.Tags.Has("ProperNoun") || t.Tags.Has("Organization") || t.Tags.Has("Acronym") {
			return true
		}
		if !yelling && isTitleCaseText(t.Text) {
			if k == 0 {
				return t.Tags.Has("Singular")
			}
			return true
		}
		return false
	}
	if m.orgWords[terms[i].machineOrNormal()] && isOrg(i-1) {
		m.setTag(terms[i:i+1], "Organization", false)
		for k := i; k >= 0; k-- {
			if !isOrg(k) {
				break
			}
			m.setTag(terms[k:k+1], "Organization", false)
		}
	}
}

var (
	placeCont     = map[string]bool{"athletic": true, "city": true, "community": true, "eastern": true, "federal": true, "financial": true, "great": true, "historic": true, "historical": true, "local": true, "memorial": true, "municipal": true, "national": true, "northern": true, "provincial": true, "southern": true, "state": true, "western": true, "spring": true, "pine": true, "sunset": true, "view": true, "oak": true, "maple": true, "spruce": true, "cedar": true, "willow": true}
	placeNoBefore = map[string]bool{"center": true, "centre": true, "way": true, "range": true, "bar": true, "bridge": true, "field": true, "pit": true}
)

func (m *model) tagPlaceWords(terms []*Term, i int, yelling bool) {
	isPlace := func(k int) bool {
		t := termAt(terms, k)
		if t == nil {
			return false
		}
		if t.Tags.Has("Organization") || t.Tags.Has("Possessive") || strings.HasSuffix(t.Normal, "'s") {
			return false
		}
		if t.Tags.Has("ProperNoun") || t.Tags.Has("Place") {
			return true
		}
		if !yelling && isTitleCaseText(t.Text) {
			if k == 0 {
				return t.Tags.Has("Singular")
			}
			return true
		}
		return false
	}
	str := terms[i].machineOrNormal()
	if !m.placeWords[str] {
		return
	}
	for n := i - 1; n >= 0; n-- {
		if placeCont[terms[n].Normal] {
			continue
		}
		if isPlace(n) {
			m.setTag(terms[n:i+1], "Place", false)
			continue
		}
		break
	}
	if placeNoBefore[str] {
		return
	}
	for n := i + 1; n < len(terms); n++ {
		if isPlace(n) {
			m.setTag(terms[i:n+1], "Place", false)
			return
		}
		if terms[n].Normal == "of" || placeCont[terms[n].Normal] {
			continue
		}
		break
	}
}

func clueWord(t *Term, words map[string]string) string {
	if t == nil || words == nil {
		return ""
	}
	return words[t.normalOrImplicit()]
}

func (m *model) clueTag(t *Term, tags map[string]string) string {
	if t == nil || tags == nil {
		return ""
	}
	list := t.Tags.List()
	parents := func(tag string) int {
		if k := m.tagSet[tag]; k != nil {
			return len(k.Parents)
		}
		return 0
	}
	sort.SliceStable(list, func(a, b int) bool { return parents(list[a]) > parents(list[b]) })
	for _, tg := range list {
		if v := tags[tg]; v != "" {
			return v
		}
	}
	return ""
}

var reAdhocTitle = regexp.MustCompile(`^[A-Z][a-z]`)

func adhocSwitch(form string, terms []*Term, i int) string {
	isCapital := func() string {
		if terms[i].Tags.Has("ProperNoun") && reAdhocTitle.MatchString(terms[i].Text) {
			return "Noun"
		}
		return ""
	}
	isAlone := func(tag string) string {
		if i == 0 && len(terms) < 2 {
			return tag
		}
		return ""
	}
	or := func(a, b string) string {
		if a != "" {
			return a
		}
		return b
	}
	switch form {
	case "Adj|Gerund", "Actor|Verb", "Adj|Past", "Adj|Present", "Noun|Gerund", "Person|Noun":
		return isCapital()
	case "Adj|Noun":
		end := ""
		if i+1 >= len(terms) && i > 0 && terms[i-1].Tags.Has("Determiner") {
			end = "Noun"
		}
		return or(isCapital(), end)
	case "Noun|Verb":
		if i > 0 {
			if c := isCapital(); c != "" {
				return c
			}
		}
		return isAlone("Infinitive")
	case "Plural|Verb":
		start := ""
		if i == 0 && len(terms) > 3 {
			start = "Plural"
		}
		return or(or(isCapital(), isAlone("PresentTense")), start)
	case "Person|Verb":
		if i != 0 {
			return isCapital()
		}
		return ""
	case "Person|Adj":
		if i == 0 && len(terms) > 1 {
			return "Person"
		}
		if isCapital() != "" {
			return "Person"
		}
		return ""
	}
	return ""
}

func (m *model) switchTag(terms []*Term, i int) {
	t := terms[i]
	if t.Switch == "" {
		return
	}
	form := t.Switch
	if t.Tags.Has("Acronym") || t.Tags.Has("PhrasalVerb") {
		return
	}
	clues := m.clues[form]
	tag := ""
	if clues != nil {
		before := i - 1
		if i > 0 && terms[i-1].Text == "also" {
			before = i - 2
			if before < 0 {
				before = 0
			}
		}
		tag = clueWord(termAt(terms, i+1), clues.AfterWords)
		if tag == "" {
			tag = clueWord(termAt(terms, before), clues.BeforeWords)
		}
		if tag == "" {
			tag = m.clueTag(termAt(terms, before), clues.BeforeTags)
		}
		if tag == "" {
			tag = m.clueTag(termAt(terms, i+1), clues.AfterTags)
		}
	}
	if a := adhocSwitch(form, terms, i); a != "" {
		tag = a
	}
	if tag != "" {
		m.setTag([]*Term{t}, tag, false)
		m.fillTags(terms, i)
	}
}

var verbTypes = []string{"PastTense", "PresentTense", "Auxiliary", "Modal", "Particle"}

func (m *model) verbType(terms []*Term, i int) {
	t := terms[i]
	if !t.Tags.Has("Verb") {
		return
	}
	for _, typ := range verbTypes {
		if t.Tags.Has(typ) {
			return
		}
	}
	m.setTag([]*Term{t}, "Infinitive", false)
}

var imperativeBeside = map[string]bool{"there": true, "this": true, "it": true, "him": true, "her": true, "us": true}

func (m *model) imperative(terms []*Term) {
	if len(terms) == 0 {
		return
	}
	t := terms[0]
	isRight := t.Switch == "Noun|Verb" || t.Tags.Has("Infinitive")
	if !isRight || len(terms) < 2 {
		return
	}
	if len(terms) < 4 && !imperativeBeside[terms[1].Normal] && !isObjectProtoKey(terms[1].Normal) {
		return
	}
	if _, ok := m.multiCache[t.Normal]; !t.Tags.Has("PhrasalVerb") && ok {
		return
	}
	if terms[1].Tags.Has("Noun") || terms[1].Tags.Has("Determiner") {
		soonVerb := false
		end := 3
		if end > len(terms) {
			end = len(terms)
		}
		for _, x := range terms[1:end] {
			if x.Tags.Has("Verb") {
				soonVerb = true
			}
		}
		if !soonVerb || t.Tags.Has("#PhrasalVerb") {
			m.setTag([]*Term{t}, "Imperative", false)
		}
	}
}
