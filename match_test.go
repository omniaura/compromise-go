package compromise

import (
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Fixtures below were produced by running real compromise v14.16.0 (the copy
// under tools/dump-model/node_modules) with a generator script:
//   - parse:   methods.one.parseMatch(pattern, {}, world), JSON-serialised
//              (Set -> array, RegExp -> {source, flags}).
//   - terms:   nlp(text).docs for the joined test sentences (text/pre/post/
//              normal/machine/implicit/alias/switch/chunk/tags).
//   - cache:   methods.one.cacheDoc(doc.document), Sets as arrays.
//   - matches: methods.one.match(doc.docs, {regs, group, justOne, notIf}, cache)
//              with and without the cache (pointer ids stripped).
//   - customTerms/customMatches: hand-built terms run through the same JS
//              runMatch, covering alias/machine/@method/fuzzy/chunk quirks.

type fxTerm struct {
	Text     string   `json:"text"`
	Pre      string   `json:"pre"`
	Post     string   `json:"post"`
	Normal   string   `json:"normal"`
	Machine  *string  `json:"machine"`
	Implicit *string  `json:"implicit"`
	Alias    []string `json:"alias"`
	Switch   *string  `json:"switch"`
	Chunk    *string  `json:"chunk"`
	Tags     []string `json:"tags"`
}

type fxMatch struct {
	Pattern      string             `json:"pattern"`
	Group        string             `json:"group"`
	JustOne      bool               `json:"justOne"`
	NotIf        string             `json:"notIf"`
	Ptrs         [][]int            `json:"ptrs"`
	ByGroup      map[string][][]int `json:"byGroup"`
	ByGroupOrder []string           `json:"byGroupOrder"`
	WithCache    *fxMatchResult     `json:"withCache"`
}

type fxMatchResult struct {
	Ptrs         [][]int            `json:"ptrs"`
	ByGroup      map[string][][]int `json:"byGroup"`
	ByGroupOrder []string           `json:"byGroupOrder"`
}

type fxFile struct {
	Prefixes []string `json:"prefixes"`
	Parse    []struct {
		Pattern string          `json:"pattern"`
		Regs    json.RawMessage `json:"regs"`
	} `json:"parse"`
	Terms         [][]fxTerm `json:"terms"`
	Cache         [][]string `json:"cache"`
	Matches       []fxMatch  `json:"matches"`
	CustomTerms   [][]fxTerm `json:"customTerms"`
	CustomMatches []fxMatch  `json:"customMatches"`
}

func loadFx(t *testing.T) *fxFile {
	t.Helper()
	var fx fxFile
	if err := json.Unmarshal([]byte(matchFixtureJSON), &fx); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return &fx
}

func fxPrefixes(fx *fxFile) map[string]bool {
	m := map[string]bool{}
	for _, p := range fx.Prefixes {
		m[p] = true
	}
	return m
}

func fxDocs(in [][]fxTerm) [][]*Term {
	docs := make([][]*Term, len(in))
	for n, s := range in {
		for _, ft := range s {
			t := &Term{Text: ft.Text, Pre: ft.Pre, Post: ft.Post, Normal: ft.Normal, Alias: ft.Alias}
			if ft.Machine != nil {
				t.Machine = *ft.Machine
			}
			if ft.Implicit != nil {
				t.Implicit = *ft.Implicit
			}
			if ft.Switch != nil {
				t.Switch = *ft.Switch
			}
			if ft.Chunk != nil {
				t.Chunk = *ft.Chunk
			}
			for _, tag := range ft.Tags {
				t.Tags.Add(tag)
			}
			docs[n] = append(docs[n], t)
		}
	}
	return docs
}

func fxCache(in [][]string) []map[string]bool {
	out := make([]map[string]bool, len(in))
	for i, s := range in {
		out[i] = map[string]bool{}
		for _, w := range s {
			out[i][w] = true
		}
	}
	return out
}

// normJSON decodes JSON into generic values, sorting fastOr arrays (a Set's
// order is not significant to matching).
func normJSON(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("json: %v", err)
	}
	var walk func(any)
	walk = func(x any) {
		switch x := x.(type) {
		case map[string]any:
			for k, v := range x {
				if k == "fastOr" {
					if arr, ok := v.([]any); ok {
						sort.Slice(arr, func(i, j int) bool { return arr[i].(string) < arr[j].(string) })
					}
				}
				walk(v)
			}
		case []any:
			for _, v := range x {
				walk(v)
			}
		}
	}
	walk(v)
	return v
}

func ptrsToInts(ps []pointer) [][]int {
	out := [][]int{}
	for _, p := range ps {
		out = append(out, []int{p[0], p[1], p[2]})
	}
	return out
}

func checkResult(t *testing.T, label string, got matchResult, want fxMatchResult) {
	t.Helper()
	if want.Ptrs == nil {
		want.Ptrs = [][]int{}
	}
	if g := ptrsToInts(got.ptrs); !reflect.DeepEqual(g, want.Ptrs) {
		t.Errorf("%s: ptrs\n got  %v\n want %v", label, g, want.Ptrs)
	}
	gotBy := map[string][][]int{}
	for k, v := range got.byGroup {
		gotBy[k] = ptrsToInts(v)
	}
	if want.ByGroup == nil {
		want.ByGroup = map[string][][]int{}
	}
	if !reflect.DeepEqual(gotBy, want.ByGroup) {
		t.Errorf("%s: byGroup\n got  %v\n want %v", label, gotBy, want.ByGroup)
	}
	gotOrder := got.byGroupOrder
	if gotOrder == nil {
		gotOrder = []string{}
	}
	if want.ByGroupOrder == nil {
		want.ByGroupOrder = []string{}
	}
	if !reflect.DeepEqual(gotOrder, want.ByGroupOrder) {
		t.Errorf("%s: byGroupOrder got %v want %v", label, gotOrder, want.ByGroupOrder)
	}
}

func runFxMatches(t *testing.T, docs [][]*Term, cache []map[string]bool, prefixes map[string]bool, cases []fxMatch) {
	t.Helper()
	for _, c := range cases {
		todo := matchTodo{regs: parseMatch(c.Pattern, prefixes), group: c.Group, hasGroup: c.Group != "", justOne: c.JustOne}
		if c.NotIf != "" {
			todo.notIf = parseMatch(c.NotIf, prefixes)
		}
		label := c.Pattern
		if c.Group != "" {
			label += " group=" + c.Group
		}
		if c.NotIf != "" {
			label += " notIf=" + c.NotIf
		}
		if c.JustOne {
			label += " justOne"
		}
		want := fxMatchResult{Ptrs: c.Ptrs, ByGroup: c.ByGroup, ByGroupOrder: c.ByGroupOrder}
		checkResult(t, label+" (no cache)", runMatch(docs, todo, nil), want)
		if cache != nil {
			wc := want
			if c.WithCache != nil {
				wc = *c.WithCache
			}
			checkResult(t, label+" (cache)", runMatch(docs, todo, cache), wc)
		}
	}
}

// TestParseMatchParity compares the Go parser with compromise's parsed regs.
func TestParseMatchParity(t *testing.T) {
	fx := loadFx(t)
	prefixes := fxPrefixes(fx)
	for _, p := range fx.Parse {
		regs := parseMatch(p.Pattern, prefixes)
		b, err := json.Marshal(regs)
		if err != nil {
			t.Fatalf("%q: marshal: %v", p.Pattern, err)
		}
		got, want := normJSON(t, b), normJSON(t, p.Regs)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("parseMatch(%q)\n got  %s\n want %s", p.Pattern, b, p.Regs)
		}
	}
}

// TestParseMatchUnmarshalRoundTrip decodes compromise's JSON regs and
// re-encodes them unchanged.
func TestParseMatchUnmarshalRoundTrip(t *testing.T) {
	fx := loadFx(t)
	for _, p := range fx.Parse {
		var regs []*reg
		if err := json.Unmarshal(p.Regs, &regs); err != nil {
			t.Fatalf("%q: unmarshal: %v", p.Pattern, err)
		}
		b, err := json.Marshal(regs)
		if err != nil {
			t.Fatalf("%q: marshal: %v", p.Pattern, err)
		}
		if got, want := normJSON(t, b), normJSON(t, p.Regs); !reflect.DeepEqual(got, want) {
			t.Errorf("round trip %q\n got  %s\n want %s", p.Pattern, b, p.Regs)
		}
	}
}

// TestMatchParity runs every fixture pattern over the JS-tagged sentences,
// with and without the failFast cache.
func TestMatchParity(t *testing.T) {
	fx := loadFx(t)
	runFxMatches(t, fxDocs(fx.Terms), fxCache(fx.Cache), fxPrefixes(fx), fx.Matches)
}

// TestMatchParityCustomTerms covers alias/machine/@method/fuzzy/chunk quirks
// on hand-built terms.
func TestMatchParityCustomTerms(t *testing.T) {
	fx := loadFx(t)
	runFxMatches(t, fxDocs(fx.CustomTerms), nil, fxPrefixes(fx), fx.CustomMatches)
}

// TestMatchJSONRegs runs regs decoded from compromise's own JSON, as the
// sweep loader does, and expects the same pointers as the Go parser's regs.
func TestMatchJSONRegs(t *testing.T) {
	fx := loadFx(t)
	docs := fxDocs(fx.Terms)
	prefixes := fxPrefixes(fx)
	byPattern := map[string]json.RawMessage{}
	for _, p := range fx.Parse {
		byPattern[p.Pattern] = p.Regs
	}
	n := 0
	for _, c := range fx.Matches {
		raw, ok := byPattern[c.Pattern]
		if !ok || c.NotIf != "" {
			continue
		}
		var regs []*reg
		if err := json.Unmarshal(raw, &regs); err != nil {
			t.Fatal(err)
		}
		want := runMatch(docs, matchTodo{regs: parseMatch(c.Pattern, prefixes), group: c.Group, justOne: c.JustOne}, nil)
		got := runMatch(docs, matchTodo{regs: regs, group: c.Group, justOne: c.JustOne}, nil)
		if !reflect.DeepEqual(got.ptrs, want.ptrs) {
			t.Errorf("%q: json regs %v vs parsed regs %v", c.Pattern, got.ptrs, want.ptrs)
		}
		n++
	}
	if n < 10 {
		t.Fatalf("only %d patterns compared", n)
	}
}

func mkTerm(text, normal string, tags ...string) *Term {
	t := &Term{Text: text, Normal: normal, Post: " "}
	for _, tag := range tags {
		t.Tags.Add(tag)
	}
	return t
}

// TestMatchContractionHandBuilt mirrors nlp("It's important to note").json()
// and nlp("it is important to note") by hand.
func TestMatchContractionHandBuilt(t *testing.T) {
	its := [][]*Term{{
		{Text: "It's", Normal: "it's", Implicit: "it", Machine: "it", Post: ""},
		{Text: "", Normal: "", Implicit: "is", Machine: "is", Post: " "},
		mkTerm("important", "important", "Adjective"),
		mkTerm("to", "to", "Conjunction"),
		mkTerm("note", "note", "Infinitive", "Verb"),
	}}
	itIs := [][]*Term{{
		mkTerm("it", "it", "Pronoun"),
		mkTerm("is", "is", "Copula", "Verb"),
		mkTerm("important", "important", "Adjective"),
		mkTerm("to", "to", "Conjunction"),
		mkTerm("note", "note", "Infinitive", "Verb"),
	}}
	cases := []struct {
		docs    [][]*Term
		pattern string
		want    [][]int
	}{
		{its, "it's important to note", [][]int{{0, 0, 5}}},
		{its, "it is important to note", [][]int{{0, 0, 5}}},
		{itIs, "it is important to note", [][]int{{0, 0, 5}}},
		{itIs, "it's important to note", [][]int{}},
		{its, "is important", [][]int{{0, 1, 3}}},
		{its, "@hasContraction important", [][]int{{0, 0, 3}}},
		{its, "#Adjective to", [][]int{{0, 2, 4}}},
	}
	for _, c := range cases {
		got := ptrsToInts(runMatch(c.docs, matchTodo{regs: parseMatch(c.pattern, nil)}, nil).ptrs)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %v want %v", c.pattern, got, c.want)
		}
	}
}

func TestParseMatchRootTokens(t *testing.T) {
	regs := parseMatch("{walk} {run/verb/2}", nil)
	if len(regs) != 2 || regs[0].Root != "walk" || regs[1].Root != "run" || regs[1].Pos != "Verb" || regs[1].Sense != "2" {
		b, _ := json.Marshal(regs)
		t.Fatalf("root parse: %s", b)
	}
	docs := [][]*Term{{mkTerm("walk", "walk", "Verb"), mkTerm("run", "run", "Verb")}}
	if got := runMatch(docs, matchTodo{regs: regs}, nil).ptrs; len(got) != 0 {
		t.Fatalf("root regs should match nothing, got %v", got)
	}
}

func TestParseMatchMinMaxNaN(t *testing.T) {
	r := parseMatch("#Noun{}", nil)[0]
	if !r.HasMin || !math.IsNaN(r.minVal()) || !r.HasMax || !math.IsNaN(r.maxVal()) || !r.Optional || !r.Greedy {
		t.Fatalf("#Noun{}: %+v", r)
	}
}

func TestParseMatchBadCaptureNamePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic like the JS TypeError")
		}
	}()
	parseMatch("[<foo", nil)
}

func TestFuzzyScore(t *testing.T) {
	if got, want := fuzzyMatch("walkin", "walking"), 1-1.0/7; math.Abs(got-want) > 1e-12 {
		t.Fatalf("fuzzy walkin/walking = %v want %v", got, want)
	}
	if got := fuzzyMatch("ab", "abc"); got != 0 {
		t.Fatalf("short strings score 0, got %v", got)
	}
	if got := fuzzyMatch("abcd", "abdc"); math.Abs(got-0.75) > 1e-12 {
		t.Fatalf("transposition = %v", got)
	}
}

func TestAliasHasOwnPropertyQuirk(t *testing.T) {
	alias := []string{"foo", "bar"}
	for w, want := range map[string]bool{"length": true, "0": true, "1": true, "2": false, "01": false, "-0": false, "foo": false} {
		if got := aliasHasOwnProperty(alias, w); got != want {
			t.Errorf("hasOwnProperty(%q) = %v want %v", w, got, want)
		}
	}
	if !strings.Contains(jsRegexpSource("a/b"), `\/`) || jsRegexpSource("") != "(?:)" {
		t.Errorf("jsRegexpSource")
	}
}

// failFast's fastOr branch returns false (does not fail) and stops checking
// the remaining regs.
func TestFailFastFastOrQuirk(t *testing.T) {
	cache := map[string]bool{"dog": true, "#Noun": true}
	if failFast(parseMatch("(zz|yy) missing", nil), cache) {
		t.Error("fastOr with no cache hit must return false without checking later regs")
	}
	if !failFast(parseMatch("missing (zz|yy)", nil), cache) {
		t.Error("a missing word fails fast")
	}
	if !failFast(parseMatch("dog #Verb", nil), cache) {
		t.Error("a missing tag fails fast")
	}
	if failFast(parseMatch("missing? !gone ~fuzz~ dog #Noun", nil), cache) {
		t.Error("optional/negative/fuzzy regs are not checked")
	}
	docs := [][]*Term{{mkTerm("dog", "dog", "Noun")}, {mkTerm("cat", "cat", "Noun")}}
	cacheDocs := []map[string]bool{{"dog": true, "#Noun": true}, nil}
	got := ptrsToInts(runMatch(docs, matchTodo{regs: parseMatch("#Noun", nil)}, cacheDocs).ptrs)
	if want := [][]int{{0, 0, 1}, {1, 0, 1}}; !reflect.DeepEqual(got, want) {
		t.Errorf("nil cache entry must not skip: got %v want %v", got, want)
	}
}

const matchFixtureJSON = `
{"prefixes":["anti","bi","co","contra","de","extra","infra","inter","intra","macro","micro","mis","mono","multi","peri","pre","pro","proto","pseudo","re","sub","supra","trans","tri","un","out","ex"],"parse":[{"pattern":"delve into","regs":[{"word":"delve"},{"word":"into"}]},{"pattern":"it's important to note","regs":[{"word":"it's"},{"word":"important"},{"word":"to"},{"word":"note"}]},{"pattern":"it is important to note","regs":[{"word":"it"},{"word":"is"},{"word":"important"},{"word":"to"},{"word":"note"}]},{"pattern":"#Value+ (thing|things)","regs":[{"greedy":true,"tag":"Value"},{"operator":"or","fastOr":["thing","things"]}]},{"pattern":"[#Value+] #Currency","regs":[{"group":"0","groupStart":true,"groupEnd":true,"greedy":true,"tag":"Value"},{"tag":"Currency"}]},{"pattern":"!once? [(a|an)] (#Duration|hundred)","regs":[{"optional":true,"negative":true,"word":"once"},{"group":"0","groupStart":true,"groupEnd":true,"operator":"or","fastOr":["a","an"]},{"choices":[[{"tag":"Duration"}],[{"word":"hundred"}]],"operator":"or"}]},{"pattern":"#Adverb? [#Adjective]","regs":[{"optional":true,"tag":"Adverb"},{"group":"0","groupStart":true,"groupEnd":true,"tag":"Adjective"}]},{"pattern":"(a|an) #Noun$","regs":[{"operator":"or","fastOr":["a","an"]},{"end":true,"tag":"Noun"}]},{"pattern":"^#Value","regs":[{"start":true,"tag":"Value"}]},{"pattern":"#Noun{2,3}","regs":[{"min":2,"max":3,"greedy":true,"tag":"Noun"}]},{"pattern":".","regs":[{"anything":true}]},{"pattern":"*","regs":[{"anything":true,"greedy":true,"optional":true}]},{"pattern":"@hasComma","regs":[{"method":"hasComma"}]},{"pattern":"/ed$/","regs":[{"regex":{"source":"ed$","flags":""}}]},{"pattern":"%Noun|Verb%","regs":[{"switch":"Noun|Verb"}]},{"pattern":"(#Value && #Cardinal)","regs":[{"choices":[[{"tag":"Value"}],[{"tag":"Cardinal"}]],"operator":"and"}]},{"pattern":"[<money>#Value+ #Currency]","regs":[{"group":"money","groupStart":true,"greedy":true,"tag":"Value"},{"group":"money","groupEnd":true,"tag":"Currency"}]},{"pattern":"foo{,3}","regs":[{"min":0,"max":3,"greedy":true,"optional":true,"word":"foo"}]},{"pattern":"foo{3,}","regs":[{"min":3,"max":999,"greedy":true,"word":"foo"}]},{"pattern":"foo{2}","regs":[{"min":2,"max":2,"greedy":true,"word":"foo"}]},{"pattern":"foo{0,2}","regs":[{"min":0,"max":2,"greedy":true,"optional":true,"word":"foo"}]},{"pattern":"#Noun{}","regs":[{"min":null,"max":null,"greedy":true,"optional":true,"tag":"Noun"}]},{"pattern":"~happi~","regs":[{"fuzzy":true,"min":0.85,"word":"happi"}]},{"pattern":"~(cat|dog)~","regs":[{"fuzzy":true,"min":0.85,"choices":[[{"word":"cat","fuzzy":true,"min":0.85}],[{"word":"dog","fuzzy":true,"min":0.85}]],"operator":"or"}]},{"pattern":"re-do","regs":[{"word":"re-do"}]},{"pattern":"walk-in-closet","regs":[{"word":"walk"},{"word":"in"},{"word":"closet"}]},{"pattern":"co-op","regs":[{"word":"co-op"}]},{"pattern":"<Noun>","regs":[{"chunk":"Noun","greedy":true}]},{"pattern":"foo\\*","regs":[{"greedy":true,"word":"foo\\"}]},{"pattern":"a\\.b\\.c","regs":[{"word":"a.b\\.c"}]},{"pattern":"foo /ab c/ bar","regs":[{"word":"foo"},{"regex":{"source":"ab c","flags":""}},{"word":"bar"}]},{"pattern":"(foo bar|baz)","regs":[{"choices":[[{"word":"foo"},{"word":"bar"}],[{"word":"baz"}]],"operator":"or"}]},{"pattern":"(foo  bar|baz)","regs":[{"choices":[[{"word":"foo"},{},{"word":"bar"}],[{"word":"baz"}]],"operator":"or"}]},{"pattern":"()","regs":[{"operator":"or","fastOr":[]}]},{"pattern":"(&&)","regs":[{"choices":[],"operator":"and"}]},{"pattern":"[foo] [bar]","regs":[{"group":"0","groupStart":true,"groupEnd":true,"word":"foo"},{"group":"1","groupStart":true,"groupEnd":true,"word":"bar"}]},{"pattern":"foo]","regs":[{"group":null,"groupEnd":true,"word":"foo"}]},{"pattern":"[<g>foo]?","regs":[{"optional":true,"group":"g","groupStart":true,"groupEnd":true,"word":"foo"}]},{"pattern":"!foo+","regs":[{"greedy":true,"negative":true,"word":"foo"}]},{"pattern":"!.","regs":[{"negative":true,"anything":true}]},{"pattern":".*","regs":[{"greedy":true,"anything":true}]},{"pattern":".+","regs":[{"greedy":true,"anything":true}]},{"pattern":"foo*","regs":[{"greedy":true,"word":"foo"}]},{"pattern":"foo* bar$","regs":[{"greedy":true,"word":"foo"},{"end":true,"word":"bar"}]},{"pattern":"^foo$","regs":[{"end":true,"start":true,"word":"foo"}]},{"pattern":"(^foo|bar$)","regs":[{"choices":[[{"start":true,"word":"foo"}],[{"end":true,"word":"bar"}]],"operator":"or"}]},{"pattern":"(foo|bar)+","regs":[{"greedy":true,"operator":"or","fastOr":["foo","bar"]}]},{"pattern":"[(a|b)]","regs":[{"group":"0","groupStart":true,"groupEnd":true,"operator":"or","fastOr":["a","b"]}]},{"pattern":"Hello World","regs":[{"word":"hello"},{"word":"world"}]},{"pattern":"FOO","regs":[{"word":"foo"}]},{"pattern":"/^[a-z]+$/","regs":[{"regex":{"source":"^[a-z]+$","flags":""}}]},{"pattern":"(@hasComma|foo)","regs":[{"choices":[[{"method":"hasComma"}],[{"word":"foo"}]],"operator":"or"}]},{"pattern":"(#Noun|foo)","regs":[{"choices":[[{"tag":"Noun"}],[{"word":"foo"}]],"operator":"or"}]},{"pattern":"#adjective","regs":[{"tag":"Adjective"}]},{"pattern":"one|two","regs":[{"word":"one|two"}]},{"pattern":"/a/ /b/","regs":[{"regex":{"source":"a","flags":""}},{"regex":{"source":"b","flags":""}}]},{"pattern":"foo/bar","regs":[{"word":"foo/bar"}]},{"pattern":"/a/b/","regs":[{"regex":{"source":"a\\/b","flags":""}}]},{"pattern":"x{1,2}?","regs":[{"optional":true,"min":1,"max":2,"greedy":true,"word":"x"}]},{"pattern":"foo   bar","regs":[{"word":"foo"},{"word":"bar"}]},{"pattern":"  leading and trailing  ","regs":[{"word":"leading"},{"word":"and"},{"word":"trailing"}]},{"pattern":"[<a> foo] bar","regs":[{"group":"a","groupStart":true},{"group":"a","groupEnd":true,"word":"foo"},{"word":"bar"}]},{"pattern":"!(a|b)","regs":[{"negative":true,"operator":"or","fastOr":["a","b"]}]},{"pattern":"(a|b)$","regs":[{"end":true,"operator":"or","fastOr":["a","b"]}]},{"pattern":"^(a|b)","regs":[{"start":true,"operator":"or","fastOr":["a","b"]}]},{"pattern":"#Noun+? foo","regs":[{"optional":true,"greedy":true,"tag":"Noun"},{"word":"foo"}]},{"pattern":"[#Noun] [#Verb] [#Adverb]","regs":[{"group":"0","groupStart":true,"groupEnd":true,"tag":"Noun"},{"group":"1","groupStart":true,"groupEnd":true,"tag":"Verb"},{"group":"2","groupStart":true,"groupEnd":true,"tag":"Adverb"}]},{"pattern":"x (a b|c d) y","regs":[{"word":"x"},{"choices":[[{"word":"a"},{"word":"b"}],[{"word":"c"},{"word":"d"}]],"operator":"or"},{"word":"y"}]},{"pattern":"(a|!b)","regs":[{"choices":[[{"word":"a"}],[{"negative":true,"word":"b"}]],"operator":"or"}]},{"pattern":"(a|b?)","regs":[{"choices":[[{"word":"a"}],[{"optional":true,"word":"b"}]],"operator":"or"}]},{"pattern":"(a|~bb~)","regs":[{"choices":[[{"word":"a"}],[{"fuzzy":true,"min":0.85,"word":"bb"}]],"operator":"or"}]},{"pattern":"~ab~","regs":[{"fuzzy":true,"min":0.85,"word":"ab"}]},{"pattern":"foo$ bar","regs":[{"end":true,"word":"foo"},{"word":"bar"}]},{"pattern":"[foo bar] baz]","regs":[{"group":"0","groupStart":true,"word":"foo"},{"group":"0","groupEnd":true,"word":"bar"},{"group":null,"groupEnd":true,"word":"baz"}]},{"pattern":"[[foo]]","regs":[{"group":"0","groupStart":true,"groupEnd":true,"word":"foo"}]},{"pattern":"a{2,4}+","regs":[{"greedy":true,"min":2,"max":4,"word":"a"}]},{"pattern":"$","regs":[{"end":true}]},{"pattern":"^","regs":[{"start":true}]},{"pattern":"?","regs":[{"optional":true}]},{"pattern":"%%","regs":[{"switch":""}]},{"pattern":"<>","regs":[{"chunk":"","greedy":true}]},{"pattern":"@","regs":[{"method":""}]},{"pattern":"#","regs":[{"tag":""}]},{"pattern":"~~~","regs":[{"fuzzy":true,"min":0.85,"word":"~"}]},{"pattern":"//","regs":[{"regex":{"source":"(?:)","flags":""}}]},{"pattern":"ß-ok","regs":[{"word":"ß-ok"}]},{"pattern":"ÉCOLE","regs":[{"word":"école"}]},{"pattern":"(a|b c|#Noun)","regs":[{"choices":[[{"word":"a"}],[{"word":"b"},{"word":"c"}],[{"tag":"Noun"}]],"operator":"or"}]},{"pattern":"x\\*y\\*","regs":[{"greedy":true,"word":"x*y\\"}]},{"pattern":"\\*","regs":[{"word":"*"}]},{"pattern":"the (big|red)+ dog","regs":[{"word":"the"},{"greedy":true,"operator":"or","fastOr":["big","red"]},{"word":"dog"}]},{"pattern":"foo /x y/","regs":[{"word":"foo"},{"regex":{"source":"x y","flags":""}}]},{"pattern":"a (b c)+ d","regs":[{"word":"a"},{"greedy":true,"choices":[[{"word":"b"},{"word":"c"}]],"operator":"or"},{"word":"d"}]}],"terms":[[{"text":"It's","pre":"","post":"","normal":"it's","tags":["Noun","Pronoun"],"machine":"it","implicit":"it","chunk":"Noun"},{"text":"","pre":"","post":" ","normal":"","tags":["Verb","Copula","PresentTense"],"machine":"is","implicit":"is","chunk":"Verb"},{"text":"important","pre":"","post":" ","normal":"important","tags":["Adjective"],"chunk":"Adjective"},{"text":"to","pre":"","post":" ","normal":"to","tags":["Conjunction"],"chunk":"Pivot"},{"text":"note","pre":"","post":" ","normal":"note","tags":["Verb","PresentTense","Infinitive"],"switch":"Noun|Verb","chunk":"Verb"},{"text":"that","pre":"","post":" ","normal":"that","tags":["Conjunction"],"chunk":"Verb"},{"text":"we","pre":"","post":" ","normal":"we","tags":["Noun","Pronoun"],"chunk":"Noun"},{"text":"delve","pre":"","post":" ","normal":"delve","tags":["Verb","PresentTense","Infinitive"],"chunk":"Verb"},{"text":"into","pre":"","post":" ","normal":"into","tags":["Preposition"],"chunk":"Pivot"},{"text":"the","pre":"","post":" ","normal":"the","tags":["Determiner"],"chunk":"Noun"},{"text":"details","pre":"","post":". ","normal":"details","tags":["Noun","Plural"],"switch":"Plural|Verb","chunk":"Noun"}],[{"text":"It","pre":"","post":" ","normal":"it","tags":["Noun","Pronoun"],"chunk":"Noun"},{"text":"is","pre":"","post":" ","normal":"is","tags":["Verb","Copula","PresentTense"],"chunk":"Verb"},{"text":"important","pre":"","post":" ","normal":"important","tags":["Adjective"],"chunk":"Adjective"},{"text":"to","pre":"","post":" ","normal":"to","tags":["Conjunction"],"chunk":"Pivot"},{"text":"note","pre":"","post":" ","normal":"note","tags":["Verb","PresentTense","Infinitive"],"switch":"Noun|Verb","chunk":"Verb"},{"text":"the","pre":"","post":" ","normal":"the","tags":["Determiner"],"chunk":"Noun"},{"text":"costs","pre":"","post":", ","normal":"costs","tags":["Noun","Plural"],"switch":"Plural|Verb","chunk":"Noun"},{"text":"sadly","pre":"","post":". ","normal":"sadly","tags":["Adverb"],"chunk":null}],[{"text":"I","pre":"","post":" ","normal":"i","tags":["Noun","Pronoun"],"chunk":"Noun"},{"text":"paid","pre":"","post":" ","normal":"paid","tags":["Verb","PastTense"],"chunk":"Verb"},{"text":"five","pre":"","post":" ","normal":"five","tags":["Value","TextValue","Cardinal","Money"],"chunk":"Noun"},{"text":"hundred","pre":"","post":" ","normal":"hundred","tags":["Value","TextValue","Multiple","Cardinal","Money"],"chunk":"Noun"},{"text":"dollars","pre":"","post":" ","normal":"dollars","tags":["Noun","Currency","Plural","Unit"],"chunk":"Noun"},{"text":"for","pre":"","post":" ","normal":"for","tags":["Preposition"],"chunk":"Pivot"},{"text":"two","pre":"","post":" ","normal":"two","tags":["Value","TextValue","Cardinal"],"chunk":"Noun"},{"text":"things","pre":"","post":", ","normal":"things","tags":["Noun","Plural"],"chunk":"Noun"},{"text":"and","pre":"","post":" ","normal":"and","tags":["Conjunction"],"chunk":"Pivot"},{"text":"once","pre":"","post":" ","normal":"once","tags":["Adverb"],"chunk":null},{"text":"a","pre":"","post":" ","normal":"a","tags":["Determiner"],"chunk":"Noun"},{"text":"hundred","pre":"","post":" ","normal":"hundred","tags":["Value","TextValue","Multiple","Cardinal"],"chunk":"Noun"},{"text":"years","pre":"","post":" ","normal":"years","tags":["Date","Noun","Duration","Plural"],"chunk":"Noun"},{"text":"passed","pre":"","post":". ","normal":"passed","tags":["Verb","PastTense"],"chunk":"Verb"}],[{"text":"He","pre":"","post":" ","normal":"he","tags":["Noun","Pronoun"],"chunk":"Noun"},{"text":"walked","pre":"","post":" ","normal":"walked","tags":["Verb","PastTense"],"chunk":"Verb"},{"text":"quickly","pre":"","post":" ","normal":"quickly","tags":["Adverb"],"chunk":"Verb"},{"text":"and","pre":"","post":" ","normal":"and","tags":["Conjunction"],"chunk":"Pivot"},{"text":"was","pre":"","post":" ","normal":"was","tags":["Verb","Copula","PastTense"],"chunk":"Verb"},{"text":"very","pre":"","post":" ","normal":"very","tags":["Adverb"],"chunk":"Verb"},{"text":"happy","pre":"","post":". ","normal":"happy","tags":["Adjective"],"chunk":"Adjective"}],[{"text":"The","pre":"","post":" ","normal":"the","tags":["Determiner"],"chunk":"Noun"},{"text":"big","pre":"","post":" ","normal":"big","tags":["Adjective"],"chunk":"Noun"},{"text":"red","pre":"","post":" ","normal":"red","tags":["Adjective"],"chunk":"Noun"},{"text":"dog","pre":"","post":" ","normal":"dog","tags":["Noun","Singular"],"chunk":"Noun"},{"text":"barked","pre":"","post":" ","normal":"barked","tags":["Verb","PastTense"],"chunk":"Verb"},{"text":"at","pre":"","post":" ","normal":"at","tags":["Preposition"],"chunk":"Pivot"},{"text":"a","pre":"","post":" ","normal":"a","tags":["Determiner"],"chunk":"Noun"},{"text":"cat","pre":"","post":" ","normal":"cat","tags":["Noun","Singular"],"chunk":"Noun"},{"text":"Seven","pre":"","post":" ","normal":"seven","tags":["ProperNoun","Noun"],"chunk":"Noun"},{"text":"dollars","pre":"","post":" ","normal":"dollars","tags":["Noun","Currency","Plural"],"chunk":"Noun"},{"text":"is","pre":"","post":" ","normal":"is","tags":["Verb","Copula","PresentTense"],"chunk":"Verb"},{"text":"a","pre":"","post":" ","normal":"a","tags":["Adverb"],"chunk":"Verb"},{"text":"lot","pre":"","post":" ","normal":"lot","tags":["Adverb"],"chunk":"Verb"},{"text":"of","pre":"","post":" ","normal":"of","tags":["Preposition"],"chunk":"Pivot"},{"text":"money","pre":"","post":" ","normal":"money","tags":["Noun","Uncountable"],"chunk":"Noun"},{"text":"for","pre":"","post":" ","normal":"for","tags":["Preposition"],"chunk":"Pivot"},{"text":"an","pre":"","post":" ","normal":"an","tags":["Determiner"],"chunk":"Noun"},{"text":"apple","pre":"","post":" ","normal":"apple","tags":["Noun","Singular"],"chunk":"Noun"},{"text":"We've","pre":"","post":"","normal":"we've","tags":["Noun","Pronoun","ProperNoun"],"machine":"we","implicit":"we","chunk":"Noun"},{"text":"","pre":"","post":" ","normal":"","tags":["Verb","Auxiliary"],"machine":"have","implicit":"have","chunk":"Verb"},{"text":"finished","pre":"","post":" ","normal":"finished","tags":["Verb","PastTense"],"switch":"Adj|Past","chunk":"Verb"},{"text":"the","pre":"","post":" ","normal":"the","tags":["Determiner"],"chunk":"Noun"},{"text":"job","pre":"","post":", ","normal":"job","tags":["Noun","Singular"],"chunk":"Noun"},{"text":"sadly","pre":"","post":". ","normal":"sadly","tags":["Adverb"],"chunk":null}],[{"text":"the","pre":"","post":" ","normal":"the","tags":["Determiner"],"chunk":"Noun"},{"text":"3","pre":"","post":" ","normal":"3","tags":["Value","Cardinal","NumericValue"],"chunk":"Noun"},{"text":"things","pre":"","post":" ","normal":"things","tags":["Noun","Plural"],"chunk":"Noun"},{"text":"in","pre":"","post":" ","normal":"in","tags":["Preposition"],"chunk":"Pivot"},{"text":"20","pre":"","post":" ","normal":"20","tags":["Value","Cardinal","NumericValue"],"chunk":"Noun"},{"text":"thing","pre":"","post":" ","normal":"thing","tags":["Noun","Singular"],"chunk":"Noun"},{"text":"a","pre":"","post":" ","normal":"a","tags":["Determiner"],"chunk":"Noun"},{"text":"big","pre":"","post":" ","normal":"big","tags":["Adjective"],"chunk":"Noun"},{"text":"big","pre":"","post":" ","normal":"big","tags":["Adjective"],"chunk":"Noun"},{"text":"big","pre":"","post":" ","normal":"big","tags":["Adjective"],"chunk":"Noun"},{"text":"red","pre":"","post":" ","normal":"red","tags":["Adjective"],"chunk":"Noun"},{"text":"dog","pre":"","post":" ","normal":"dog","tags":["Noun","Singular"],"chunk":"Noun"},{"text":"walked","pre":"","post":" ","normal":"walked","tags":["Verb","PastTense"],"chunk":"Verb"},{"text":"an","pre":"","post":" ","normal":"an","tags":["Determiner","Value"],"chunk":"Noun"},{"text":"hour","pre":"","post":"","normal":"hour","tags":["Date","Noun","Duration","Singular"],"chunk":"Noun"}]],"cache":[["it's","it","#Noun","#Pronoun","is","#Verb","#Copula","#PresentTense","important","#Adjective","to","#Conjunction","note","%Noun|Verb%","#Infinitive","that","we","delve","into","#Preposition","the","#Determiner","details","%Plural|Verb%","#Plural"],["it","#Noun","#Pronoun","is","#Verb","#Copula","#PresentTense","important","#Adjective","to","#Conjunction","note","%Noun|Verb%","#Infinitive","the","#Determiner","costs","%Plural|Verb%","#Plural","sadly","#Adverb"],["i","#Noun","#Pronoun","paid","#Verb","#PastTense","five","#Value","#TextValue","#Cardinal","#Money","hundred","#Multiple","dollars","#Currency","#Plural","#Unit","for","#Preposition","two","things","and","#Conjunction","once","#Adverb","a","#Determiner","years","#Date","#Duration","passed"],["he","#Noun","#Pronoun","walked","#Verb","#PastTense","quickly","#Adverb","and","#Conjunction","was","#Copula","very","happy","#Adjective"],["the","#Determiner","big","#Adjective","red","dog","#Noun","#Singular","barked","#Verb","#PastTense","at","#Preposition","a","cat","seven","#ProperNoun","dollars","#Currency","#Plural","is","#Copula","#PresentTense","#Adverb","lot","of","money","#Uncountable","for","an","apple","we've","we","#Pronoun","have","#Auxiliary","finished","%Adj|Past%","job","sadly"],["the","#Determiner","3","#Value","#Cardinal","#NumericValue","things","#Noun","#Plural","in","#Preposition","20","thing","#Singular","a","big","#Adjective","red","dog","walked","#Verb","#PastTense","an","hour","#Date","#Duration"]],"matches":[{"pattern":"delve into","ptrs":[[0,7,9]],"byGroup":{},"byGroupOrder":[]},{"pattern":"it's important to note","ptrs":[[0,0,5]],"byGroup":{},"byGroupOrder":[]},{"pattern":"it is important to note","ptrs":[[0,0,5],[1,0,5]],"byGroup":{},"byGroupOrder":[]},{"pattern":"#Value+ (thing|things)","ptrs":[[2,6,8],[5,1,3],[5,4,6]],"byGroup":{},"byGroupOrder":[]},{"pattern":"[#Value+] #Currency","ptrs":[[2,2,4]],"byGroup":{},"byGroupOrder":[],"group":"0"},{"pattern":"[#Value+] #Currency","ptrs":[[2,2,5]],"byGroup":{"0":[[2,2,4]]},"byGroupOrder":["0"]},{"pattern":"!once? [(a|an)] (#Duration|hundred)","ptrs":[[5,13,14]],"byGroup":{},"byGroupOrder":[],"group":"0"},{"pattern":"!once? [(a|an)] (#Duration|hundred)","ptrs":[[2,10,12],[5,12,15]],"byGroup":{"0":[[5,13,14]]},"byGroupOrder":["0"]},{"pattern":"#Adverb? [#Adjective]","ptrs":[[0,2,3],[1,2,3],[3,6,7],[4,1,2],[4,2,3],[5,7,8],[5,8,9],[5,9,10],[5,10,11]],"byGroup":{},"byGroupOrder":[],"group":"0"},{"pattern":"(a|an) #Noun$","ptrs":[[5,13,15]],"byGroup":{},"byGroupOrder":[]},{"pattern":"^#Value","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"#Noun{2,3}","ptrs":[[4,7,10],[4,17,19]],"byGroup":{},"byGroupOrder":[]},{"pattern":".","ptrs":[[0,0,1],[0,1,2],[0,2,3],[0,3,4],[0,4,5],[0,5,6],[0,6,7],[0,7,8],[0,8,9],[0,9,10],[0,10,11],[1,0,1],[1,1,2],[1,2,3],[1,3,4],[1,4,5],[1,5,6],[1,6,7],[1,7,8],[2,0,1],[2,1,2],[2,2,3],[2,3,4],[2,4,5],[2,5,6],[2,6,7],[2,7,8],[2,8,9],[2,9,10],[2,10,11],[2,11,12],[2,12,13],[2,13,14],[3,0,1],[3,1,2],[3,2,3],[3,3,4],[3,4,5],[3,5,6],[3,6,7],[4,0,1],[4,1,2],[4,2,3],[4,3,4],[4,4,5],[4,5,6],[4,6,7],[4,7,8],[4,8,9],[4,9,10],[4,10,11],[4,11,12],[4,12,13],[4,13,14],[4,14,15],[4,15,16],[4,16,17],[4,17,18],[4,18,19],[4,19,20],[4,20,21],[4,21,22],[4,22,23],[4,23,24],[5,0,1],[5,1,2],[5,2,3],[5,3,4],[5,4,5],[5,5,6],[5,6,7],[5,7,8],[5,8,9],[5,9,10],[5,10,11],[5,11,12],[5,12,13],[5,13,14],[5,14,15]],"byGroup":{},"byGroupOrder":[]},{"pattern":"*","ptrs":[[0,0,11],[1,0,8],[2,0,14],[3,0,7],[4,0,24],[5,0,15]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasComma","ptrs":[[1,6,7],[2,7,8],[4,22,23]],"byGroup":{},"byGroupOrder":[]},{"pattern":"/ed$/","ptrs":[[2,3,4],[2,11,12],[2,13,14],[3,1,2],[4,2,3],[4,4,5],[4,20,21],[5,10,11],[5,12,13]],"byGroup":{},"byGroupOrder":[]},{"pattern":"%Noun|Verb%","ptrs":[[0,4,5],[1,4,5]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(#Value && #Cardinal)","ptrs":[[2,2,3],[2,3,4],[2,6,7],[2,11,12],[5,1,2],[5,4,5]],"byGroup":{},"byGroupOrder":[]},{"pattern":"[<money>#Value+ #Currency]","ptrs":[[2,2,5]],"byGroup":{},"byGroupOrder":[],"group":"money"},{"pattern":"[<money>#Value+ #Currency]","ptrs":[[2,2,5]],"byGroup":{"money":[[2,2,5]]},"byGroupOrder":["money"]},{"pattern":"we've","ptrs":[[4,18,20]],"byGroup":{},"byGroupOrder":[]},{"pattern":"we have","ptrs":[[4,18,20]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasContraction finished","ptrs":[[4,18,21]],"byGroup":{},"byGroupOrder":[]},{"pattern":"!#Noun+ #Noun","ptrs":[[0,1,7],[0,7,11],[1,1,7],[2,1,5],[2,5,8],[2,8,13],[4,0,4],[4,4,8],[4,10,15],[4,15,18],[4,19,23],[5,0,3],[5,3,6],[5,6,12],[5,12,15]],"byGroup":{},"byGroupOrder":[]},{"pattern":".* dollars","ptrs":[[2,0,5],[4,0,10]],"byGroup":{},"byGroupOrder":[]},{"pattern":"the .{1,2} dog","ptrs":[[4,0,4]],"byGroup":{},"byGroupOrder":[]},{"pattern":"~happi~","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"(big red|the) dog","ptrs":[[4,1,4],[5,9,12]],"byGroup":{},"byGroupOrder":[]},{"pattern":"#Adjective+$","ptrs":[[3,6,7]],"byGroup":{},"byGroupOrder":[]},{"pattern":"^the","ptrs":[[4,0,1],[5,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"#Determiner [#Adjective+] #Noun","ptrs":[[4,1,3],[5,7,11]],"byGroup":{},"byGroupOrder":[],"group":"0"},{"pattern":"[#Determiner] [#Adjective]","ptrs":[[4,0,2],[5,6,8]],"byGroup":{"0":[[4,0,1],[5,6,7]],"1":[[4,1,2],[5,7,8]]},"byGroupOrder":["0","1"]},{"pattern":"[#Determiner] [#Adjective]","ptrs":[[4,1,2],[5,7,8]],"byGroup":{},"byGroupOrder":[],"group":"1"},{"pattern":"big+ red","ptrs":[[4,1,3],[5,7,11]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(big)+ red","ptrs":[[4,1,3],[5,7,11]],"byGroup":{},"byGroupOrder":[]},{"pattern":"#Adjective{2}","ptrs":[[4,1,3],[5,7,9],[5,9,11]],"byGroup":{},"byGroupOrder":[]},{"pattern":"!#Adjective? dog","ptrs":[[4,3,4],[5,11,12]],"byGroup":{},"byGroupOrder":[]},{"pattern":"a !big? red? dog","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"#Noun","ptrs":[[0,0,1],[0,6,7],[0,10,11],[1,0,1],[1,6,7],[2,0,1],[2,4,5],[2,7,8],[2,12,13],[3,0,1],[4,7,8],[4,8,9],[4,9,10],[4,14,15],[4,17,18],[4,18,19],[4,22,23],[5,2,3],[5,5,6],[5,14,15]],"byGroup":{},"byGroupOrder":[],"notIf":"dog"},{"pattern":"#Noun","ptrs":[[0,0,1]],"byGroup":{},"byGroupOrder":[],"justOne":true},{"pattern":"it is","ptrs":[[0,0,2],[1,0,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"it's","ptrs":[[0,0,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"it","ptrs":[[0,0,1],[1,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"is","ptrs":[[0,1,2],[1,1,2],[4,10,11]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(it|is) important","ptrs":[[0,1,3],[1,1,3]],"byGroup":{},"byGroupOrder":[]},{"pattern":"#Copula","ptrs":[[0,1,2],[1,1,2],[3,4,5],[4,10,11]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@isTitleCase","ptrs":[[0,0,1],[1,0,1],[3,0,1],[4,0,1],[4,8,9],[4,18,19]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasPeriod","ptrs":[[0,10,11],[1,7,8],[2,13,14],[3,6,7],[4,23,24]],"byGroup":{},"byGroupOrder":[]},{"pattern":".+ dog$","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"big* red","ptrs":[[4,1,3],[5,7,11]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(a|an)? #Noun","ptrs":[[0,0,1],[0,6,7],[0,10,11],[1,0,1],[1,6,7],[2,0,1],[2,4,5],[2,7,8],[2,12,13],[3,0,1],[4,3,4],[4,6,8],[4,8,9],[4,9,10],[4,14,15],[4,16,18],[4,18,19],[4,22,23],[5,2,3],[5,5,6],[5,11,12],[5,13,15]],"byGroup":{},"byGroupOrder":[]},{"pattern":"#Value #Value? #Plural","ptrs":[[2,2,5],[2,6,8],[2,11,13],[5,1,3]],"byGroup":{},"byGroupOrder":[]},{"pattern":"^. .","ptrs":[[0,0,2],[1,0,2],[2,0,2],[3,0,2],[4,0,2],[5,0,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"* dog","ptrs":[[4,0,4],[5,0,12]],"byGroup":{},"byGroupOrder":[]},{"pattern":"[*] dog","ptrs":[[4,0,3],[5,0,11]],"byGroup":{},"byGroupOrder":[],"group":"0"},{"pattern":"!walked+ walked","ptrs":[[3,0,2],[5,0,13]],"byGroup":{},"byGroupOrder":[]},{"pattern":"!dog{1,2}","ptrs":[[0,0,2],[0,2,4],[0,4,6],[0,6,8],[0,8,10],[0,10,11],[1,0,2],[1,2,4],[1,4,6],[1,6,8],[2,0,2],[2,2,4],[2,4,6],[2,6,8],[2,8,10],[2,10,12],[2,12,14],[3,0,2],[3,2,4],[3,4,6],[3,6,7],[4,0,2],[4,2,3],[4,4,6],[4,6,8],[4,8,10],[4,10,12],[4,12,14],[4,14,16],[4,16,18],[4,18,20],[4,20,22],[4,22,24],[5,0,2],[5,2,4],[5,4,6],[5,6,8],[5,8,10],[5,10,11],[5,12,14],[5,14,15]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(#Adjective && big)","ptrs":[[4,1,2],[5,7,8],[5,8,9],[5,9,10]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(red dog|big)","ptrs":[[4,1,2],[4,2,4],[5,7,8],[5,8,9],[5,9,10],[5,10,12]],"byGroup":{},"byGroupOrder":[]},{"pattern":"#Adjective (dog|cat)$","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"hundred","ptrs":[[2,3,4],[2,11,12]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(hundred|five)","ptrs":[[2,2,3],[2,3,4],[2,11,12]],"byGroup":{},"byGroupOrder":[]},{"pattern":".* (red dog|zz)","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"big? (red dog|zz)","ptrs":[[4,1,4],[5,9,12]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(dog|cat)$","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"a (big|zz)+ red","ptrs":[[5,6,11]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(#Adjective && big)$","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"(big|red) (#Noun && dog)","ptrs":[[4,2,4],[5,10,12]],"byGroup":{},"byGroupOrder":[]},{"pattern":"[<adj>#Adjective+] [<n>#Noun]","ptrs":[[4,1,4],[5,7,12]],"byGroup":{"adj":[[4,1,3],[5,7,11]],"n":[[4,3,4],[5,11,12]]},"byGroupOrder":["adj","n"]},{"pattern":"[<z>#Adjective] [<1>#Noun]","ptrs":[[4,2,4],[5,10,12]],"byGroup":{"1":[[4,3,4],[5,11,12]],"z":[[4,2,3],[5,10,11]]},"byGroupOrder":["1","z"]},{"pattern":"#Adjective+ dog","ptrs":[],"byGroup":{},"byGroupOrder":[],"notIf":"red"},{"pattern":"^#Noun","ptrs":[[0,0,1],[1,0,1],[2,0,1],[3,0,1]],"byGroup":{},"byGroupOrder":[],"justOne":true},{"pattern":"big{2,}","ptrs":[[5,7,10]],"byGroup":{},"byGroupOrder":[]},{"pattern":"!big{2,3} dog","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"big{0,1} red","ptrs":[[4,1,3],[5,9,11]],"byGroup":{},"byGroupOrder":[]},{"pattern":"the !small? tiny? big red dog","ptrs":[[4,0,4]],"byGroup":{},"byGroupOrder":[]},{"pattern":"big? (&&) red","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":".* (&&)","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"the (&&)? big","ptrs":[[4,0,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"big .* (&&)?","ptrs":[[4,1,2],[5,7,8],[5,8,9],[5,9,10]],"byGroup":{},"byGroupOrder":[]},{"pattern":"the .* (big red|zz) dog","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"big{3}","ptrs":[[5,7,10]],"byGroup":{},"byGroupOrder":[]},{"pattern":"the big{2,}","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"the big{3,}","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"a big{2}","ptrs":[[5,6,9]],"byGroup":{},"byGroupOrder":[]},{"pattern":"a big{4,} red","ptrs":[[5,6,11]],"byGroup":{},"byGroupOrder":[]},{"pattern":"a big{5,} red","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"red? (big red|zz) dog","ptrs":[[4,1,4],[5,9,12]],"byGroup":{},"byGroupOrder":[]},{"pattern":"[.] [.]","ptrs":[[0,0,2],[0,2,4],[0,4,6],[0,6,8],[0,8,10],[1,0,2],[1,2,4],[1,4,6],[1,6,8],[2,0,2],[2,2,4],[2,4,6],[2,6,8],[2,8,10],[2,10,12],[2,12,14],[3,0,2],[3,2,4],[3,4,6],[4,0,2],[4,2,4],[4,4,6],[4,6,8],[4,8,10],[4,10,12],[4,12,14],[4,14,16],[4,16,18],[4,18,20],[4,20,22],[4,22,24],[5,0,2],[5,2,4],[5,4,6],[5,6,8],[5,8,10],[5,10,12],[5,12,14]],"byGroup":{"0":[[0,0,1],[0,2,3],[0,4,5],[0,6,7],[0,8,9],[1,0,1],[1,2,3],[1,4,5],[1,6,7],[2,0,1],[2,2,3],[2,4,5],[2,6,7],[2,8,9],[2,10,11],[2,12,13],[3,0,1],[3,2,3],[3,4,5],[4,0,1],[4,2,3],[4,4,5],[4,6,7],[4,8,9],[4,10,11],[4,12,13],[4,14,15],[4,16,17],[4,18,19],[4,20,21],[4,22,23],[5,0,1],[5,2,3],[5,4,5],[5,6,7],[5,8,9],[5,10,11],[5,12,13]],"1":[[0,1,2],[0,3,4],[0,5,6],[0,7,8],[0,9,10],[1,1,2],[1,3,4],[1,5,6],[1,7,8],[2,1,2],[2,3,4],[2,5,6],[2,7,8],[2,9,10],[2,11,12],[2,13,14],[3,1,2],[3,3,4],[3,5,6],[4,1,2],[4,3,4],[4,5,6],[4,7,8],[4,9,10],[4,11,12],[4,13,14],[4,15,16],[4,17,18],[4,19,20],[4,21,22],[4,23,24],[5,1,2],[5,3,4],[5,5,6],[5,7,8],[5,9,10],[5,11,12],[5,13,14]]},"byGroupOrder":["0","1"]},{"pattern":"[.] [.]","ptrs":[[0,1,2],[0,3,4],[0,5,6],[0,7,8],[0,9,10],[1,1,2],[1,3,4],[1,5,6],[1,7,8],[2,1,2],[2,3,4],[2,5,6],[2,7,8],[2,9,10],[2,11,12],[2,13,14],[3,1,2],[3,3,4],[3,5,6],[4,1,2],[4,3,4],[4,5,6],[4,7,8],[4,9,10],[4,11,12],[4,13,14],[4,15,16],[4,17,18],[4,19,20],[4,21,22],[4,23,24],[5,1,2],[5,3,4],[5,5,6],[5,7,8],[5,9,10],[5,11,12],[5,13,14]],"byGroup":{},"byGroupOrder":[],"group":"1"},{"pattern":"%Adj|Present%","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"walked","ptrs":[[3,1,2],[5,12,13]],"byGroup":{},"byGroupOrder":[]},{"pattern":"walk","ptrs":[],"byGroup":{},"byGroupOrder":[]}],"customTerms":[[{"text":"Foo/Bar","pre":"\"","post":"\" ","normal":"foo/bar","alias":["foo","bar"],"tags":["Noun"]},{"text":"walking","pre":"","post":" - ","normal":"walking","tags":["Gerund","Verb"],"chunk":"Verb"},{"text":"Isn't","pre":"","post":" ","normal":"isn't","machine":"is","implicit":"is","tags":["Copula"]},{"text":"","pre":"","post":"","normal":"","machine":"not","implicit":"not","tags":["Negative"]},{"text":"NASA","pre":"","post":"... ","normal":"nasa","tags":["Acronym","Organization"]},{"text":"empty","pre":"-","post":"?! ","normal":"empty","alias":[],"tags":[]},{"text":"go","pre":"","post":";","normal":"go","tags":["Verb"],"switch":"Noun|Verb"}],[{"text":"Long","pre":"","post":": ","normal":"long","tags":["Adjective"],"chunk":"Noun"},{"text":"words","pre":"","post":". ","normal":"words","tags":["Plural","Noun"],"chunk":"Noun"}]],"customMatches":[{"pattern":"length","ptrs":[[0,0,1],[0,5,6]],"byGroup":{},"byGroupOrder":[]},{"pattern":"0","ptrs":[[0,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"1","ptrs":[[0,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"2","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"foo","ptrs":[[0,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"bar","ptrs":[[0,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"foo/bar","ptrs":[[0,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"is","ptrs":[[0,2,3]],"byGroup":{},"byGroupOrder":[]},{"pattern":"is not","ptrs":[[0,2,4]],"byGroup":{},"byGroupOrder":[]},{"pattern":"isn't","ptrs":[[0,2,4]],"byGroup":{},"byGroupOrder":[]},{"pattern":"not","ptrs":[[0,3,4]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(foo|walk)","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"(is|zz) not","ptrs":[[0,2,4]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasQuote","ptrs":[[0,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasQuotation","ptrs":[[0,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasHyphen","ptrs":[[0,5,6]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasDash","ptrs":[[0,1,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@isUpperCase","ptrs":[[0,4,5]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@isKnown","ptrs":[[0,0,1],[0,1,2],[0,2,3],[0,3,4],[0,4,5],[0,6,7],[1,0,1],[1,1,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasEllipses","ptrs":[[0,4,5]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasQuestionMark","ptrs":[[0,5,6]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasSlash","ptrs":[[0,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasSemicolon","ptrs":[[0,6,7]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasColon","ptrs":[[1,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@isAcronym","ptrs":[[0,4,5]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasExclamation","ptrs":[[0,5,6]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@toString","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasTitleCase","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"@isTitleCase","ptrs":[[0,0,1],[0,2,3],[1,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasComma","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasPeriod","ptrs":[[1,1,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasContraction","ptrs":[[0,2,4]],"byGroup":{},"byGroupOrder":[]},{"pattern":"@hasContraction #Organization","ptrs":[[0,2,5]],"byGroup":{},"byGroupOrder":[]},{"pattern":"~walkin~","ptrs":[[0,1,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"~walkn~","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"~(walkin|nope)~","ptrs":[[0,1,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"<Noun>","ptrs":[[1,0,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"<Verb>","ptrs":[[0,1,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"%Noun|Verb%","ptrs":[[0,6,7]],"byGroup":{},"byGroupOrder":[]},{"pattern":"#Copula #Negative","ptrs":[[0,2,4]],"byGroup":{},"byGroupOrder":[]},{"pattern":"isn't nasa","ptrs":[[0,2,5]],"byGroup":{},"byGroupOrder":[]},{"pattern":"is #Negative","ptrs":[[0,2,4]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(#Copula|zz) nasa","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"!#Noun+","ptrs":[[0,1,7],[1,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"#Noun+$","ptrs":[[1,1,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"words$","ptrs":[[1,1,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(long|words)$","ptrs":[[1,1,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"^(walking|long)","ptrs":[[1,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(walking|long)","ptrs":[[0,1,2],[1,0,1]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(long|words)? ^(long|words)","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"(#Plural && words)$","ptrs":[[1,1,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"(long && #Adjective)$","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"/^w/","ptrs":[[0,1,2],[1,1,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"/s$/","ptrs":[[1,1,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":".{2}","ptrs":[[0,0,2],[0,2,4],[0,4,6],[1,0,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":".{,2}","ptrs":[[0,0,2],[0,2,4],[0,4,6],[0,6,7],[1,0,2]],"byGroup":{},"byGroupOrder":[]},{"pattern":"[.{2,}] words","ptrs":[],"byGroup":{},"byGroupOrder":[]},{"pattern":"* nasa","ptrs":[[0,0,5]],"byGroup":{},"byGroupOrder":[]},{"pattern":". [*] .","ptrs":[[0,0,2],[0,2,4],[0,4,6],[1,0,2]],"byGroup":{"0":[[0,1,1],[0,3,3],[0,5,5],[1,1,1]]},"byGroupOrder":["0"]},{"pattern":". [*] .","ptrs":[[0,1,1],[0,3,3],[0,5,5],[1,1,1]],"byGroup":{},"byGroupOrder":[],"group":"0"},{"pattern":"!.","ptrs":[],"byGroup":{},"byGroupOrder":[]}]}
`
