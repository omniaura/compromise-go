package compromise

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Port of compromise/src/3-three/numbers: values() (find), parse and
// toNumber(), including fraction parsing.

const (
	numOnes  = "one|two|three|four|five|six|seven|eight|nine"
	numTens  = "twenty|thirty|forty|fifty|sixty|seventy|eighty|ninety|fourty"
	numTeens = "eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen"
)

// Values is view.values() / view.numbers().
func (v *View) Values() *View {
	m := v.Match("#Value+")
	if m.Has("#NumericValue #NumericValue") {
		if m.Has("#Value @hasComma #Value") {
			m.SplitAfter("@hasComma") // JS discards this result
		} else if m.Has("#NumericValue #Fraction") {
			m.SplitAfter("#NumericValue #Fraction") // JS discards this result
		} else {
			m = m.SplitAfter("#NumericValue")
		}
	}
	if m.Has("#Value #Value #Value") && !m.Has("#Multiple") {
		if m.Has("(" + numTens + ") #Cardinal #Cardinal") {
			m = m.SplitAfter("(" + numTens + ") #Cardinal")
		}
	}
	if m.Has("#Value #Value") {
		if m.Has("#NumericValue #NumericValue") {
			m = m.SplitOn("#Year")
		}
		if m.Has("(" + numTens + ") (" + numTeens + ")") {
			m = m.SplitAfter("(" + numTens + ")")
		}
		double := m.Match("#Cardinal #Cardinal")
		if double.Found() && !m.Has("(point|decimal|#Fraction)") {
			if !double.Has("#Cardinal (#Multiple|point|decimal)") {
				noMultiple := m.Has("(" + numOnes + ") (" + numTens + ")")
				tensVal := double.Has("(" + numTens + ") #Cardinal")
				multVal := double.Has("#Multiple #Value")
				if !noMultiple && !tensVal && !multVal {
					for _, d := range double.Terms().Each() {
						m = m.SplitOnView(d)
					}
				}
			}
		}
		if m.Match("#Ordinal #Ordinal").Match("#TextValue").Found() && !m.Has("#Multiple") {
			if !m.Has("(" + numTens + ") #Ordinal") {
				m = m.SplitAfter("#Ordinal")
			}
		}
		if m.Has("#Time") {
			m = m.SplitOn("#Time")
		}
		m = m.SplitBefore("#Ordinal [#Cardinal]", "0")
		if m.Has("#TextValue #NumericValue") && !m.Has("("+numTens+"|#Multiple)") {
			m = m.SplitBefore("#TextValue #NumericValue")
		}
	}
	m = m.SplitAfter("#NumberRange")
	m = m.SplitBefore("#Year")
	return m
}

// ParsedNumber is the result of parsing one value.
type ParsedNumber struct {
	Prefix, Suffix string
	// Num is the value; NaN when JS Number() would give NaN. Valid is false
	// when compromise returns null.
	Num      float64
	Valid    bool
	HasComma bool
}

var (
	reCommaDigits = regexp.MustCompile(`[0-9],[0-9]`)
)

// splitDigits reproduces `[prefix, num] = str.split(/([0-9.,]*)/)` and
// `suffix = arr.slice(2).join(”)`. The pattern can match the empty string,
// so JS advances one UTF-16 unit at a time between matches. hasNum is false
// when JS leaves num undefined (no split happened at all).
func splitDigits(str string) (prefix, num, rest string, hasNum bool) {
	isNum := func(c byte) bool { return (c >= '0' && c <= '9') || c == '.' || c == ',' }
	var parts []string
	p, q := 0, 0
	size := len(str)
	for q < size {
		e := q
		for e < size && isNum(str[e]) {
			e++
		}
		if e == p {
			_, w := utf8.DecodeRuneInString(str[q:])
			q += w
			continue
		}
		parts = append(parts, str[p:q], str[q:e])
		p = e
		q = p
	}
	parts = append(parts, str[p:])
	if len(parts) < 2 {
		return parts[0], "", "", false
	}
	return parts[0], parts[1], strings.Join(parts[2:], ""), true
}

// jsToNumber is Number(str) for the strings parseNumeric produces.
func jsToNumber(s string) float64 {
	t := jsTrim(s)
	if t == "" {
		return 0
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil || strings.ContainsAny(t, "_xXpP") || strings.EqualFold(t, "inf") || strings.EqualFold(t, "infinity") || strings.EqualFold(t, "nan") {
		if t == "Infinity" || t == "+Infinity" {
			return math.Inf(1)
		}
		if t == "-Infinity" {
			return math.Inf(-1)
		}
		return math.NaN()
	}
	return f
}

func parseNumericValue(str string, viewLen int) (ParsedNumber, bool) {
	str = strings.ReplaceAll(str, ",", "")
	prefix, num, suffix, hasNum := splitDigits(str)
	// JS: `if (num !== '' && m.length < 2)`, where an undefined num passes.
	if (hasNum && num == "") || viewLen >= 2 {
		return ParsedNumber{}, false
	}
	// Number(num || str)
	n := jsToNumber(num)
	if num == "" {
		n = jsToNumber(str)
	}
	if suffix == "st" || suffix == "nd" || suffix == "rd" || suffix == "th" {
		suffix = ""
	}
	return ParsedNumber{Prefix: prefix, Num: n, Valid: true, Suffix: suffix}, true
}

// parseValue is numbers/parse/index.js on a single-range view.
func (v *View) parseValue() ParsedNumber {
	str := v.textForm("root")
	hasComma := reCommaDigits.MatchString(v.textForm("text"))
	if termCount(v) == 1 && !v.Has("#Multiple") {
		if res, ok := parseNumericValue(str, v.Len()); ok {
			res.HasComma = hasComma
			return res
		}
	}
	m := v
	frPart := m.Match("#Fraction{2,}$")
	if !frPart.Found() {
		frPart = m.Match("^#Fraction$")
	}
	var frac *fraction
	if frPart.Found() {
		if frPart.Has("#Value and #Value #Fraction") {
			frPart = frPart.Match("and #Value #Fraction")
		}
		frac = frPart.parseFraction()
		m = m.Not(frPart)
		m = m.Not(m.Match("and$"))
		str = m.textForm("root")
	}
	num := 0.0
	if str != "" {
		if n, ok := parseText(str); ok && n != 0 && !math.IsNaN(n) {
			num = n
		}
	}
	if frac != nil && frac.decimal != 0 && !math.IsNaN(frac.decimal) {
		num += frac.decimal
	}
	return ParsedNumber{HasComma: hasComma, Num: num, Valid: true}
}

func termCount(v *View) int {
	n := 0
	docs, _ := v.docs()
	for _, d := range docs {
		n += len(d)
	}
	return n
}

var numWords = struct {
	ones, teens, tens, multiples map[string]float64
}{
	ones: map[string]float64{"zeroth": 0, "first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5, "sixth": 6, "seventh": 7, "eighth": 8, "ninth": 9,
		"zero": 0, "one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8, "nine": 9},
	teens: map[string]float64{"tenth": 10, "eleventh": 11, "twelfth": 12, "thirteenth": 13, "fourteenth": 14, "fifteenth": 15, "sixteenth": 16, "seventeenth": 17, "eighteenth": 18, "nineteenth": 19,
		"ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16, "seventeen": 17, "eighteen": 18, "nineteen": 19},
	tens: map[string]float64{"twentieth": 20, "thirtieth": 30, "fortieth": 40, "fourtieth": 40, "fiftieth": 50, "sixtieth": 60, "seventieth": 70, "eightieth": 80, "ninetieth": 90,
		"twenty": 20, "thirty": 30, "forty": 40, "fourty": 40, "fifty": 50, "sixty": 60, "seventy": 70, "eighty": 80, "ninety": 90},
	multiples: map[string]float64{"hundredth": 100, "thousandth": 1000, "millionth": 1e6, "billionth": 1e9, "trillionth": 1e12, "quadrillionth": 1e15, "quintillionth": 1e18, "sextillionth": 1e21, "septillionth": 1e24,
		"hundred": 100, "thousand": 1000, "million": 1e6, "billion": 1e9, "trillion": 1e12, "quadrillion": 1e15, "quintillion": 1e18, "sextillion": 1e21, "septillion": 1e24, "grand": 1000},
}

var casualForms = map[string]float64{"a few": 3, "a couple": 2, "a dozen": 12, "two dozen": 24, "zero": 0}

var (
	reMinus         = regexp.MustCompile(`(?i)^(minus|negative)[` + jsSpace + `-]`)
	reHalf          = regexp.MustCompile(`(?i)^(a[` + jsSpace + `])?half[` + jsSpace + `-](of[` + jsSpace + `])?`)
	reImproperFrac  = regexp.MustCompile(`^([0-9,. ]+)/([0-9,. ]+)$`)
	reNumericOnly   = regexp.MustCompile(`^[0-9.]+$`)
	reOrdNum4       = regexp.MustCompile(`([4567890])r?th$`)
	reLeadCurrency  = regexp.MustCompile(`^[$€¥£¢]`)
	reTrailCurrency = regexp.MustCompile(`[%$€¥£¢]$`)
	reNumLetters    = regexp.MustCompile(`([0-9])([a-z\x{C0}-\x{FF}]{1,2})$`)
	reSingleDigit   = regexp.MustCompile(`^[0-9]$`)
)

func parseNumericWord(str string) string {
	if strings.HasSuffix(str, "1st") {
		str = strings.TrimSuffix(str, "1st") + "1"
	}
	if strings.HasSuffix(str, "2nd") {
		str = strings.TrimSuffix(str, "2nd") + "2"
	}
	if strings.HasSuffix(str, "3rd") {
		str = strings.TrimSuffix(str, "3rd") + "3"
	}
	str = reOrdNum4.ReplaceAllString(str, "$1")
	str = reLeadCurrency.ReplaceAllString(str, "")
	str = reTrailCurrency.ReplaceAllString(str, "")
	str = strings.ReplaceAll(str, ",", "")
	str = reNumLetters.ReplaceAllString(str, "$1")
	return str
}

func sectionSum(has map[string]float64) float64 {
	s := 0.0
	for _, k := range []string{"ones", "teens", "tens"} {
		s += has[k]
	}
	return s
}

func isValidNumWord(w string, has map[string]float64) bool {
	_, hOnes := has["ones"]
	_, hTeens := has["teens"]
	_, hTens := has["tens"]
	// JS tests truthiness: a zero value counts as absent.
	hOnes = hOnes && has["ones"] != 0
	hTeens = hTeens && has["teens"] != 0
	hTens = hTens && has["tens"] != 0
	if _, ok := numWords.ones[w]; ok {
		return !(hOnes || hTeens)
	}
	if _, ok := numWords.teens[w]; ok {
		return !(hOnes || hTeens || hTens)
	}
	if _, ok := numWords.tens[w]; ok {
		return !(hOnes || hTeens || hTens)
	}
	return true
}

func parseDecimals(arr []string) float64 {
	str := "0."
	for _, w := range arr {
		if v, ok := numWords.ones[w]; ok {
			str += strconv.FormatFloat(v, 'f', -1, 64)
		} else if v, ok := numWords.teens[w]; ok {
			str += strconv.FormatFloat(v, 'f', -1, 64)
		} else if v, ok := numWords.tens[w]; ok {
			str += strconv.FormatFloat(v, 'f', -1, 64)
		} else if reSingleDigit.MatchString(w) {
			str += w
		} else {
			return 0
		}
	}
	f, err := strconv.ParseFloat(str, 64)
	if err != nil {
		return math.NaN()
	}
	return f
}

// jsParseFloat is parseFloat: the longest numeric prefix, NaN if none.
func jsParseFloat(s string) float64 {
	s = strings.TrimLeftFunc(s, isJSSpace)
	loc := reFloatPrefix.FindString(s)
	if loc == "" {
		return math.NaN()
	}
	f, err := strconv.ParseFloat(loc, 64)
	if err != nil {
		return math.NaN()
	}
	return f
}

var reFloatPrefix = regexp.MustCompile(`^[+-]?([0-9]+(\.[0-9]*)?|\.[0-9]+)([eE][+-]?[0-9]+)?`)

// parseText is numbers/parse/toNumber: words to a number. ok is false for null.
func parseText(str string) (float64, bool) {
	if v, ok := casualForms[str]; ok {
		return v, true
	}
	if str == "a" || str == "an" {
		return 1, true
	}
	amount := 1.0
	if reMinus.MatchString(str) {
		amount = -1
		str = replaceFirst(reMinus, str, "")
	} else if reHalf.MatchString(str) {
		amount = 0.5
		str = replaceFirst(reHalf, str, "")
	}
	var lastMult float64
	hasLast := false
	has := map[string]float64{}
	sum := 0.0
	isNegative := false
	terms := splitSpaceDash(str)
	for i := 0; i < len(terms); i++ {
		w := parseNumericWord(terms[i])
		if w == "" || w == "and" {
			continue
		}
		if w == "-" || w == "negative" {
			isNegative = true
			continue
		}
		if w[0] == '-' {
			isNegative = true
			w = w[1:]
		}
		if w == "point" {
			sum += sectionSum(has)
			sum += parseDecimals(terms[i+1:])
			sum *= amount
			return sum, true
		}
		if fm := reImproperFrac.FindStringSubmatch(w); fm != nil {
			num := jsParseFloat(strings.NewReplacer(",", "", " ", "").Replace(fm[1]))
			denom := jsParseFloat(strings.NewReplacer(",", "", " ", "").Replace(fm[2]))
			if denom != 0 && !math.IsNaN(denom) {
				q := num / denom
				if math.IsNaN(q) {
					q = 0
				}
				sum += q
			}
			continue
		}
		if _, ok := numWords.tens[w]; ok {
			if v, ok := has["ones"]; ok && v != 0 && len(has) == 1 {
				sum = v * 100
				has = map[string]float64{}
			}
		}
		if !isValidNumWord(w, has) {
			return 0, false
		}
		if reNumericOnly.MatchString(w) {
			has["ones"] = jsParseFloat(w)
		} else if v, ok := numWords.ones[w]; ok {
			has["ones"] = v
		} else if v, ok := numWords.teens[w]; ok {
			has["teens"] = v
		} else if v, ok := numWords.tens[w]; ok {
			has["tens"] = v
		} else if mult, ok := numWords.multiples[w]; ok {
			if hasLast && mult == lastMult {
				return 0, false
			}
			if mult == 100 && i+1 < len(terms) {
				if m2, ok := numWords.multiples[terms[i+1]]; ok && m2 != 0 {
					mult *= m2
					i++
				} else if isObjectProtoKey(terms[i+1]) {
					// words.multiples[w2] is a prototype function: mult becomes NaN
					mult = math.NaN()
					i++
				}
			}
			if !hasLast || mult < lastMult {
				s := sectionSum(has)
				if s == 0 {
					s = 1
				}
				sum += s * mult
			} else {
				sum += sectionSum(has)
				if sum == 0 {
					sum = 1
				}
				sum *= mult
			}
			lastMult = mult
			hasLast = true
			has = map[string]float64{}
		}
	}
	sum += sectionSum(has)
	sum *= amount
	if isNegative {
		sum *= -1
	}
	if sum == 0 && len(has) == 0 {
		return 0, false
	}
	return sum, true
}

// splitSpaceDash is str.split(/[ -]/), keeping empty pieces.
func splitSpaceDash(s string) []string {
	var out []string
	last := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '-' {
			out = append(out, s[last:i])
			last = i + 1
		}
	}
	return append(out, s[last:])
}

// JSNumber is JavaScript's Number(str): what Fair Copy applies to
// toNumber()'s output. "3" is 3; "1st", "5,000" and "(3)" are NaN.
func JSNumber(s string) float64 { return jsToNumber(s) }
