package compromise

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf16"
)

// jsSpace is the character set JavaScript's \s matches. Go's \s is ASCII-only,
// and compromise splits words on JS whitespace, so every translated \s and \S
// uses this class instead.
const jsSpace = `\t\n\x{0b}\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

// jsRegexp is a JavaScript regular expression translated to RE2, or, for the
// one model pattern RE2 cannot express, a hand-written matcher.
type jsRegexp struct {
	source string
	re     *regexp.Regexp
	fn     func(string) bool
}

func (r *jsRegexp) MatchString(s string) bool {
	if r.fn != nil {
		return r.fn(s)
	}
	return r.re.MatchString(s)
}

var unicodeProps = map[string]string{
	"Letter":          "L",
	"L":               "L",
	"Lu":              "Lu",
	"Ll":              "Ll",
	"Number":          "N",
	"N":               "N",
	"Currency_Symbol": "Sc",
	"Sc":              "Sc",
}

// compileJSRegexp translates the JS regex dialect compromise's model uses.
func compileJSRegexp(source, flags string) (*jsRegexp, error) {
	if source == `^[\p{Emoji_Presentation}\p{Extended_Pictographic}]` {
		return &jsRegexp{source: source, fn: startsWithEmoji}, nil
	}
	var b strings.Builder
	if strings.Contains(flags, "i") {
		b.WriteString("(?i)")
	}
	inClass := false
	for i := 0; i < len(source); i++ {
		c := source[i]
		if c == '\\' && i+1 < len(source) {
			n := source[i+1]
			switch n {
			case 's':
				if inClass {
					b.WriteString(jsSpace)
				} else {
					b.WriteString("[" + jsSpace + "]")
				}
				i++
				continue
			case 'S':
				if inClass {
					return nil, fmt.Errorf("unsupported \\S inside a class: %q", source)
				}
				b.WriteString("[^" + jsSpace + "]")
				i++
				continue
			case 'u':
				if i+5 < len(source) {
					b.WriteString(`\x{` + source[i+2:i+6] + `}`)
					i += 5
					continue
				}
			case 'p', 'P':
				end := strings.IndexByte(source[i:], '}')
				if end < 0 || source[i+2] != '{' {
					return nil, fmt.Errorf("bad property escape in %q", source)
				}
				name := source[i+3 : i+end]
				goName, ok := unicodeProps[name]
				if !ok {
					return nil, fmt.Errorf("unsupported unicode property %q in %q", name, source)
				}
				b.WriteString(`\` + string(n) + `{` + goName + `}`)
				i += end
				continue
			}
			b.WriteByte(c)
			b.WriteByte(n)
			i++
			continue
		}
		switch c {
		case '[':
			inClass = true
		case ']':
			inClass = false
		}
		b.WriteByte(c)
	}
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("translating %q: %w", source, err)
	}
	return &jsRegexp{source: source, re: re}, nil
}

func mustJSRegexp(source, flags string) *jsRegexp {
	r, err := compileJSRegexp(source, flags)
	if err != nil {
		panic(err)
	}
	return r
}

// startsWithEmoji approximates /^[\p{Emoji_Presentation}\p{Extended_Pictographic}]/u,
// which RE2 has no property for. It covers the pictographic blocks; rare
// symbols outside them are a known gap (see PARITY.md).
func startsWithEmoji(s string) bool {
	for _, r := range s {
		switch {
		case r == 0xa9 || r == 0xae || r == 0x203c || r == 0x2049 || r == 0x2122 || r == 0x2139:
			return true
		case r >= 0x2194 && r <= 0x21aa:
			return true
		case r >= 0x231a && r <= 0x23ff:
			return true
		case r >= 0x24c2 && r <= 0x24c2, r >= 0x25aa && r <= 0x25fe:
			return true
		case r >= 0x2600 && r <= 0x27bf:
			return true
		case r >= 0x2934 && r <= 0x2935, r >= 0x2b05 && r <= 0x2b55:
			return true
		case r == 0x3030 || r == 0x303d || r == 0x3297 || r == 0x3299:
			return true
		case r >= 0x1f000 && r <= 0x1faff:
			return true
		}
		return false
	}
	return false
}

// --- JavaScript string semantics -------------------------------------------
//
// compromise measures lengths and offsets in UTF-16 code units. These helpers
// reproduce that for the places where it changes a decision.

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// jsLen is String.prototype.length.
func jsLen(s string) int {
	if isASCII(s) {
		return len(s)
	}
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// jsSubstring is String.prototype.substring(start, end) with clamping.
func jsSubstring(s string, start, end int) string {
	if isASCII(s) {
		n := len(s)
		start, end = clampRange(start, end, n)
		return s[start:end]
	}
	u := utf16.Encode([]rune(s))
	start, end = clampRange(start, end, len(u))
	return string(utf16.Decode(u[start:end]))
}

func clampRange(start, end, n int) (int, int) {
	if start < 0 {
		start = 0
	}
	if end < 0 {
		end = 0
	}
	if start > n {
		start = n
	}
	if end > n {
		end = n
	}
	if start > end {
		start, end = end, start
	}
	return start, end
}

// jsLastChar is str[str.length-1] (a single UTF-16 unit; for an astral
// character that is a lone surrogate, which never equals a model key).
func jsLastChar(s string) (rune, bool) {
	if s == "" {
		return 0, false
	}
	if isASCII(s) {
		return rune(s[len(s)-1]), true
	}
	rs := []rune(s)
	r := rs[len(rs)-1]
	if r >= 0x10000 {
		return 0xdc00, true
	}
	return r, true
}

// jsToLower is String.prototype.toLowerCase for the cases compromise sees.
func jsToLower(s string) string {
	if isASCII(s) {
		return strings.ToLower(s)
	}
	var b strings.Builder
	for _, r := range s {
		if r == 0x130 { // İ lowercases to i + combining dot in JS
			b.WriteString("i̇")
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// jsTrim is String.prototype.trim.
func jsTrim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return isJSSpace(r) })
}

// isObjectProtoKey reports keys that a plain JS object answers truthily
// through Object.prototype. compromise looks some words up with `obj[word]`
// rather than hasOwnProperty, so "constructor" is a hit there.
func isObjectProtoKey(k string) bool {
	switch k {
	case "constructor", "toString", "toLocaleString", "valueOf", "hasOwnProperty",
		"isPrototypeOf", "propertyIsEnumerable", "__proto__", "__defineGetter__",
		"__defineSetter__", "__lookupGetter__", "__lookupSetter__":
		return true
	}
	return false
}
