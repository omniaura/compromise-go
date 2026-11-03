package compromise

import (
	"math"
	"regexp"
	"strings"
)

// Port of compromise/src/3-three/numbers/fractions/parse.js.

type fraction struct {
	numerator, denominator float64
	hasNum, hasDen         bool // false where JS holds null
	decimal                float64
}

var fractionNamed = map[string]float64{"half": 2, "halve": 2, "quarter": 4}

var reSlashFraction = regexp.MustCompile(`^([-+]?[0-9]+)/([-+]?[0-9]+)(st|nd|rd|th)?s?$`)

// parseNumberText is fractions' parseNumber: parseText of the reduced text.
func parseNumberText(v *View) (float64, bool) {
	return parseText(v.textForm("root"))
}

func (v *View) fracNamed() *fraction {
	if d, ok := fractionNamed[v.textForm("root")]; ok {
		return &fraction{numerator: 1, denominator: d, hasNum: true, hasDen: true}
	}
	return nil
}

func (v *View) fracSlash() *fraction {
	f := reSlashFraction.FindStringSubmatch(v.textForm("root"))
	if f == nil || f[1] == "" {
		return nil
	}
	return &fraction{numerator: jsToNumber(f[1]), denominator: jsToNumber(f[2]), hasNum: true, hasDen: true}
}

func (v *View) fracOutOf() *fraction {
	const p = "[<num>#Value+] out of every? [<den>#Value+]"
	if !v.Match(p).Found() {
		return nil
	}
	num, den := v.MatchGroup(p, "num"), v.MatchGroup(p, "den")
	if !num.Found() || !den.Found() {
		return nil
	}
	n, ok1 := parseNumberText(num)
	d, ok2 := parseNumberText(den)
	if !ok1 || !ok2 || n == 0 || d == 0 || math.IsNaN(n) || math.IsNaN(d) {
		return nil
	}
	return &fraction{numerator: n, denominator: d, hasNum: true, hasDen: true}
}

func (v *View) fracOrdinal() *fraction {
	const p = "[<num>(#Cardinal|a)+] [<den>#Fraction+]"
	if !v.Match(p).Found() {
		return nil
	}
	numV, denV := v.MatchGroup(p, "num"), v.MatchGroup(p, "den")
	var n float64
	var nOK bool
	if numV.Has("a") {
		n, nOK = 1, true
	} else {
		n, nOK = parseNumberText(numV)
	}
	str := denV.textForm("root")
	str = strings.TrimSuffix(str, "s") // den.replaceWith(str without the s)
	var d float64
	var dOK bool
	if named, ok := fractionNamed[str]; ok {
		d, dOK = named, true
	} else {
		d, dOK = parseText(str)
	}
	if nOK && dOK {
		return &fraction{numerator: n, denominator: d, hasNum: true, hasDen: true}
	}
	return nil
}

func (v *View) fracOneNth() *fraction {
	found := v.Match("^#Ordinal$")
	if !found.Found() {
		return nil
	}
	// m.lookAhead('^of .') returns a View, which is always truthy.
	d, ok := parseNumberText(found)
	return &fraction{numerator: 1, hasNum: true, denominator: d, hasDen: ok}
}

func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

func (v *View) parseFraction() *fraction {
	res := v.fracNamed()
	if res == nil {
		res = v.fracSlash()
	}
	if res == nil {
		res = v.fracOutOf()
	}
	if res == nil {
		res = v.fracOrdinal()
	}
	if res == nil {
		res = v.fracOneNth()
	}
	if res == nil {
		return nil
	}
	truthy := func(x float64) bool { return x != 0 && !math.IsNaN(x) }
	if res.hasNum && res.hasDen && truthy(res.numerator) && truthy(res.denominator) {
		dec := res.numerator / res.denominator
		rounded := jsRound(dec*1000) / 1000
		if rounded == 0 && dec != 0 {
			rounded = dec
		}
		res.decimal = rounded
	}
	return res
}
