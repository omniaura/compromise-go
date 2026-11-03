package compromise

// Port of compromise v14.16.0 src/1-one/match/methods/match/** and
// termMethods.js: runMatch (failFast, fromHere, notIf, getGroup), the match
// steps, the term matcher (doesMatch wrapped by the negation wrapper) and the
// '@method' term methods. Quirks are kept on purpose; see the comments
// marked "quirk".

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

type matchTodo struct {
	regs     []*reg
	group    string // JS todo.group (a number group is passed as its String())
	hasGroup bool   // group mode is `if (group)`, i.e. a non-empty group string
	justOne  bool
	notIf    []*reg // nil == undefined
}

// pointer is [sentence n, start, end).
type pointer [3]int

type matchResult struct {
	ptrs    []pointer
	byGroup map[string][]pointer
	// byGroupOrder is Object.keys(byGroup): array-index keys ascending, then
	// the rest in insertion order.
	byGroupOrder []string
}

// one fromHere result
type foundMatch struct {
	pointer    pointer
	groups     map[string]pointer
	groupOrder []string // Object.keys order
}

// ---------------------------------------------------------------------------
// index.js

func runMatch(docs [][]*Term, todo matchTodo, cache []map[string]bool) matchResult {
	regs := todo.regs
	if len(regs) == 0 {
		return matchResult{ptrs: []pointer{}, byGroup: map[string][]pointer{}}
	}
	minLength := 0
	for _, r := range regs {
		if !r.Optional && !r.Negative {
			minLength++
		}
	}
	var results []*foundMatch
docs:
	for n := 0; n < len(docs); n++ {
		terms := docs[n]
		// can we skip this sentence?
		if n < len(cache) && cache[n] != nil && failFast(regs, cache[n]) {
			continue
		}
		// ^start regs only run once, per phrase
		if regs[0].Start {
			if res := fromHere(terms, regs, 0, len(terms)); res != nil {
				addSentence(res, n)
				results = append(results, res)
			}
			continue
		}
		//ok, try starting the match now from every term
		for i := 0; i < len(terms); i++ {
			slice := terms[i:]
			// ensure it's long-enough
			if len(slice) < minLength {
				break
			}
			res := fromHere(slice, regs, i, len(terms))
			if res != nil {
				addSentence(res, n)
				results = append(results, res)
				if todo.justOne {
					break docs
				}
				// skip ahead, over these results (quirk: Math.abs(end - 1))
				end := res.pointer[2]
				if absInt(end-1) > i {
					i = absInt(end - 1)
				}
			}
		}
	}
	// ensure any end-results ($) match until the last term
	if regs[len(regs)-1].End {
		kept := results[:0]
		for _, res := range results {
			if len(docs[res.pointer[0]]) == res.pointer[2] {
				kept = append(kept, res)
			}
		}
		results = kept
	}
	if todo.notIf != nil {
		results = notIf(results, todo.notIf, docs)
	}
	// grab the requested group
	return getGroupResults(results, todo.group)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func addSentence(res *foundMatch, n int) {
	res.pointer[0] = n
	for k, g := range res.groups {
		g[0] = n
		res.groups[k] = g
	}
}

// ---------------------------------------------------------------------------
// 01-failFast.js

func failFast(regs []*reg, cache map[string]bool) bool {
	has := func(s string) bool { _, ok := cache[s]; return ok }
	for _, r := range regs {
		if r.Optional || r.Negative || r.Fuzzy {
			continue
		}
		// is the word missing from the cache?
		if r.Word != "" && !has(r.Word) {
			return true
		}
		// is the tag missing?
		if r.tagDefined() && !has("#"+r.Tag) {
			return true
		}
		// perform a speedup for fast-or (quirk: returns false, i.e. never
		// fails, and stops checking the remaining regs)
		if r.FastOr != nil {
			found := false
			for w := range cache {
				if r.FastOr[w] {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 03-notIf.js

func notIf(results []*foundMatch, not []*reg, docs [][]*Term) []*foundMatch {
	kept := results[:0:0]
outer:
	for _, res := range results {
		n, start, end := res.pointer[0], res.pointer[1], res.pointer[2]
		terms := jsSlice(docs[n], start, end)
		for i := 0; i < len(terms); i++ {
			if fromHere(terms[i:], not, i, len(terms)) != nil {
				continue outer
			}
		}
		kept = append(kept, res)
	}
	return kept
}

// jsSlice is Array.prototype.slice(start, end) for non-negative bounds.
func jsSlice(terms []*Term, start, end int) []*Term {
	if start > len(terms) {
		start = len(terms)
	}
	if end > len(terms) {
		end = len(terms)
	}
	if start < 0 {
		start = 0
	}
	if end < start {
		return nil
	}
	return terms[start:end]
}

// ---------------------------------------------------------------------------
// 03-getGroup.js

func getGroupResults(res []*foundMatch, group string) matchResult {
	out := matchResult{ptrs: []pointer{}, byGroup: map[string][]pointer{}}
	if len(res) == 0 {
		return out
	}
	if group != "" {
		for _, r := range res {
			if g, ok := r.groups[group]; ok {
				out.ptrs = append(out.ptrs, g)
			}
		}
		return out
	}
	var order []string
	for _, r := range res {
		out.ptrs = append(out.ptrs, r.pointer)
		for _, k := range r.groupOrder {
			if _, ok := out.byGroup[k]; !ok {
				order = append(order, k)
			}
			out.byGroup[k] = append(out.byGroup[k], r.groups[k])
		}
	}
	out.byGroupOrder = jsObjectKeyOrder(order)
	return out
}

// isArrayIndexKey reports whether a property key is a canonical array index
// ("0", "17"; not "01", "-1"), which JS objects enumerate first, ascending.
func isArrayIndexKey(k string) (uint64, bool) {
	if k == "" || len(k) > 10 || (len(k) > 1 && k[0] == '0') {
		return 0, false
	}
	for i := 0; i < len(k); i++ {
		if k[i] < '0' || k[i] > '9' {
			return 0, false
		}
	}
	v, err := strconv.ParseUint(k, 10, 64)
	if err != nil || v >= math.MaxUint32 {
		return 0, false
	}
	return v, true
}

// jsObjectKeyOrder orders keys (given in insertion order) like Object.keys.
func jsObjectKeyOrder(keys []string) []string {
	var idx, rest []string
	for _, k := range keys {
		if _, ok := isArrayIndexKey(k); ok {
			idx = append(idx, k)
		} else {
			rest = append(rest, k)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool {
		va, _ := isArrayIndexKey(idx[a])
		vb, _ := isArrayIndexKey(idx[b])
		return va < vb
	})
	return append(idx, rest...)
}

// ---------------------------------------------------------------------------
// 02-from-here.js

type groupSpan struct {
	start, length int
}

type matchState struct {
	t         int
	terms     []*Term
	r         int
	regs      []*reg
	groups    map[string]*groupSpan
	groupKeys []string // insertion order
	startI    int
	phraseLen int
	inGroup   string
	hasGroup  bool
}

func (st *matchState) term(i int) *Term {
	if i < 0 || i >= len(st.terms) {
		return nil
	}
	return st.terms[i]
}

func (st *matchState) regAt(i int) *reg {
	if i < 0 || i >= len(st.regs) {
		return nil
	}
	return st.regs[i]
}

// _lib.js getGroup
func (st *matchState) getGroup(termIndex int) *groupSpan {
	if g, ok := st.groups[st.inGroup]; ok {
		return g
	}
	g := &groupSpan{start: termIndex}
	if st.groups == nil {
		st.groups = map[string]*groupSpan{}
	}
	st.groups[st.inGroup] = g
	st.groupKeys = append(st.groupKeys, st.inGroup)
	return g
}

// fromHere tries a sequence of regs on terms, starting at this term.
func fromHere(terms []*Term, regs []*reg, startI, phraseLength int) *foundMatch {
	if len(terms) == 0 || len(regs) == 0 {
		return nil
	}
	st := &matchState{
		terms:     terms,
		regs:      regs,
		startI:    startI,
		phraseLen: phraseLength,
	}
	for ; st.r < len(regs); st.r++ {
		rg := regs[st.r]
		// Check if this reg has a named capture group
		st.hasGroup = rg.Group != ""
		if st.hasGroup {
			st.inGroup = rg.Group
		} else {
			st.inGroup = ""
		}
		//have we run-out of terms?
		if st.term(st.t) == nil {
			//are all remaining regs optional or negative?
			alive := false
			for _, remain := range regs[st.r:] {
				if !remain.Optional {
					alive = true
					break
				}
			}
			if !alive {
				break //done!
			}
			return nil // die
		}
		// support 'unspecific greedy' .* properly
		if rg.Anything && rg.Greedy {
			if !st.doAstrix() {
				return nil
			}
			continue
		}
		// slow-OR - multi-word OR (a|b|foo bar)
		if rg.Choices != nil && rg.Operator == "or" {
			if !st.orBlock() {
				return nil
			}
			continue
		}
		// slow-AND - multi-word AND (#Noun && foo) blocks
		if rg.Choices != nil && rg.Operator == "and" {
			if !st.andBlock() {
				return nil
			}
			continue
		}
		// support '.' as any-single
		if rg.Anything {
			// '!.' negative anything should insta-fail
			if rg.Negative {
				return nil
			}
			if !st.simpleMatch() {
				return nil
			}
			continue
		}
		// support 'foo*$' until the end
		if st.isEndGreedy(rg) {
			if !st.simpleMatch() {
				return nil
			}
			continue
		}
		// ok, it doesn't match - but maybe it wasn't *supposed* to?
		if rg.Negative {
			if !st.doNegative() {
				return nil
			}
			continue
		}
		// ok, finally test the term-reg
		if doesMatch(st.terms[st.t], rg, st.startI+st.t, st.phraseLen) {
			if !st.simpleMatch() {
				return nil
			}
			continue
		}
		//ok who cares, keep going
		if rg.Optional {
			continue
		}
		// finally, we die
		return nil
	}
	//return our results, as pointers
	ptr := pointer{0, startI, st.t + startI}
	if ptr[1] == ptr[2] {
		return nil //found 0 terms
	}
	res := &foundMatch{pointer: ptr}
	if len(st.groupKeys) > 0 {
		res.groups = make(map[string]pointer, len(st.groupKeys))
	}
	for _, k := range jsObjectKeyOrder(st.groupKeys) {
		o := st.groups[k]
		start := startI + o.start
		res.groups[k] = pointer{0, start, start + o.length}
		res.groupOrder = append(res.groupOrder, k)
	}
	return res
}

// ---------------------------------------------------------------------------
// steps/astrix.js

// support 'unspecific greedy' .* properly
func (st *matchState) doAstrix() bool {
	rg := st.regs[st.r]
	skipto, ok := st.greedyTo(st.regAt(st.r + 1))
	//maybe we couldn't find it
	if !ok || skipto == 0 {
		return false
	}
	// ensure it's long enough
	if rg.HasMin && float64(skipto-st.t) < rg.minVal() {
		return false
	}
	// reduce it back, if it's too long
	if rg.HasMax && float64(skipto-st.t) > rg.maxVal() {
		st.t = st.t + floatToInt(rg.maxVal())
		return true
	}
	// set the group result
	if st.hasGroup {
		g := st.getGroup(st.t)
		g.length = skipto - st.t
	}
	st.t = skipto
	return true
}

// ---------------------------------------------------------------------------
// steps/or-block.js, steps/and-block.js

func (st *matchState) orBlock() bool {
	rg := st.regs[st.r]
	skipNum := st.doOrBlock(0)
	// did we find a match?
	if skipNum != 0 {
		// handle 'not' logic
		if rg.Negative {
			return false // die
		}
		// tuck in as named-group
		if st.hasGroup {
			g := st.getGroup(st.t)
			g.length += skipNum
		}
		// ensure we're at the end
		if rg.End {
			end := st.phraseLen
			if st.t+st.startI+skipNum != end {
				return false
			}
		}
		st.t += skipNum
		return true
	} else if !rg.Optional {
		return false //die
	}
	return true
}

func (st *matchState) andBlock() bool {
	rg := st.regs[st.r]
	skipNum := st.doAndBlock()
	if skipNum != 0 {
		// handle 'not' logic
		if rg.Negative {
			return false // die
		}
		if st.hasGroup {
			g := st.getGroup(st.t)
			g.length += skipNum
		}
		// ensure we're at the end (quirk: phrase_length - 1, unlike orBlock)
		if rg.End {
			end := st.phraseLen - 1
			if st.t+st.startI != end {
				return false
			}
		}
		st.t += skipNum
		return true
	} else if !rg.Optional {
		return false //die
	}
	return true
}

// ---------------------------------------------------------------------------
// steps/logic/and-or.js

func (st *matchState) doOrBlock(skipN int) int {
	block := st.regs[st.r]
	wasFound := false
	// do each multiword sequence
	for c := 0; c < len(block.Choices); c++ {
		regs := block.Choices[c]
		wasFound = true
		for wIndex, cr := range regs {
			extra := 0
			t := st.t + wIndex + skipN + extra
			tm := st.term(t)
			if tm == nil {
				wasFound = false
				break
			}
			foundBlock := doesMatch(tm, cr, t+st.startI, st.phraseLen)
			// this can be greedy - '(foo+ bar)'
			if foundBlock && cr.Greedy {
				for i := 1; i < len(st.terms); i++ {
					if term := st.term(t + i); term != nil {
						// quirk: index is start_i + i, not t + i
						if doesMatch(term, cr, st.startI+i, st.phraseLen) {
							extra++
						} else {
							break
						}
					}
				}
			}
			// quirk: extras accumulate even when this choice later fails
			skipN += extra
			if !foundBlock {
				wasFound = false
				break
			}
		}
		if wasFound {
			skipN += len(regs)
			break
		}
	}
	// we found a match -  is it greedy though?
	if wasFound && block.Greedy {
		return st.doOrBlock(skipN) // try it again!
	}
	return skipN
}

func (st *matchState) doAndBlock() int {
	longest := 0
	// all blocks must match, and we return the greediest match
	rg := st.regs[st.r]
	for _, block := range rg.Choices {
		//  for multi-word blocks, all must match
		allWords := true
		for wIndex, cr := range block {
			tryTerm := st.t + wIndex
			tm := st.term(tryTerm)
			// quirk: index is tryTerm, without start_i
			if tm == nil || !doesMatch(tm, cr, tryTerm, st.phraseLen) {
				allWords = false
				break
			}
		}
		if allWords && len(block) > longest {
			longest = len(block)
		}
		if !allWords {
			return 0 // false
		}
	}
	return longest
}

// ---------------------------------------------------------------------------
// steps/negative.js, steps/logic/negative-greedy.js

// '!foo' should match anything that isn't 'foo'
func (st *matchState) doNegative() bool {
	rg := st.regs[st.r]
	// match *anything* but this term
	tmp := *rg
	tmp.Negative = false // try removing it
	// found it? if so, we die here
	if doesMatch(st.terms[st.t], &tmp, st.startI+st.t, st.phraseLen) {
		return false //bye
	}
	// should we skip the term too?
	if rg.Optional {
		// "before after" - "before !foo? after"
		// does the next reg match the this term?
		if nextReg := st.regAt(st.r + 1); nextReg != nil {
			if doesMatch(st.terms[st.t], nextReg, st.startI+st.t, st.phraseLen) {
				st.r++
			} else if nextReg.Optional && st.regAt(st.r+2) != nil {
				// support "!foo? extra? need"
				if doesMatch(st.terms[st.t], st.regs[st.r+2], st.startI+st.t, st.phraseLen) {
					st.r += 2
				}
			}
		}
	}
	// negative greedy - !foo+  - super hard!
	if rg.Greedy {
		return st.negGreedy(&tmp, st.regAt(st.r+1))
	}
	st.t++
	return true
}

func (st *matchState) negGreedy(rg *reg, nextReg *reg) bool {
	skip := 0
	for t := st.t; t < len(st.terms); t++ {
		// quirk: the index passed is start_i + state.t, not start_i + t
		if doesMatch(st.terms[t], rg, st.startI+st.t, st.phraseLen) {
			break //stop going
		}
		// "!foo+ after"  should stop at 'after'
		if nextReg != nil && doesMatch(st.terms[t], nextReg, st.startI+st.t, st.phraseLen) {
			break
		}
		skip++
		// is it max-length now?
		if rg.HasMax && float64(skip) == rg.maxVal() {
			break
		}
	}
	if skip == 0 {
		return false //dead
	}
	// did we satisfy min for !foo{min,max}
	if jsTruthyNum(rg.minVal()) && rg.HasMin && rg.minVal() > float64(skip) {
		return false //dead
	}
	st.t += skip
	return true
}

// ---------------------------------------------------------------------------
// steps/simple-match.js, optional-match.js, contraction-skip.js, greedy-match.js

// '[foo]' should also be logged as a group
func (st *matchState) setGroup(startAt int) {
	rg := st.regs[st.r]
	g := st.getGroup(startAt)
	if st.t > 1 && rg.Greedy {
		g.length += st.t - startAt
	} else {
		g.length++
	}
}

// when a reg matches a term
func (st *matchState) simpleMatch() bool {
	rg := st.regs[st.r]
	term := st.terms[st.t]
	startAt := st.t
	// if it's a negative optional match... :0
	if rg.Optional && st.regAt(st.r+1) != nil && rg.Negative {
		return true
	}
	// okay, it was a match, but if it's optional too,
	// we should check the next reg too, to skip it?
	if rg.Optional && st.regAt(st.r+1) != nil {
		st.foundOptional()
	}
	// did we match the first part of a contraction?
	// (quirk: contractionSkip, greedyMatch and setGroup read regs[state.r]
	// after foundOptional may have advanced it)
	if term.Implicit != "" && st.term(st.t+1) != nil {
		st.contractionSkip()
	}
	//advance to the next term!
	st.t++
	//if this isn't the last term, refuse the match
	if rg.End && st.t != len(st.terms) && !rg.Greedy {
		return false //die
	}
	// keep 'foo+' going...
	if rg.Greedy {
		if !st.greedyMatch() {
			return false
		}
	}
	// log '[foo]' as a group
	if st.hasGroup {
		st.setGroup(startAt)
	}
	return true
}

// 'foo? foo' matches are tricky.
func (st *matchState) foundOptional() {
	rg := st.regs[st.r]
	term := st.terms[st.t]
	next := st.regs[st.r+1]
	// does the next reg match it too?
	nextRegMatched := doesMatch(term, next, st.startI+st.t, st.phraseLen)
	if rg.Negative || nextRegMatched {
		// but does the next reg match the next term??
		// only skip if it doesn't (quirk: index stays start_i + t)
		nextTerm := st.term(st.t + 1)
		if nextTerm == nil || !doesMatch(nextTerm, next, st.startI+st.t, st.phraseLen) {
			st.r++
		}
	}
}

// a match for "we have" works as normal, but matching "we've" skips over
// the implicit term
func (st *matchState) contractionSkip() {
	term := st.terms[st.t]
	rg := st.regs[st.r]
	if term.Implicit != "" && st.term(st.t+1) != nil {
		nextTerm := st.terms[st.t+1]
		// ensure next word is implicit
		if nextTerm.Implicit == "" {
			return
		}
		// we matched "we've" - skip-over [we, have]
		if rg.Word != "" && rg.Word == term.Normal {
			st.t++
		}
		// also skip for @hasContraction
		if rg.Method == "hasContraction" {
			st.t++
		}
	}
}

// keep 'foo+' or 'foo*' going..
func (st *matchState) greedyMatch() bool {
	rg := st.regs[st.r]
	t, ok := st.getGreedy(st.regAt(st.r + 1))
	if !ok {
		return false //greedy was too short
	}
	st.t = t
	// foo{2,4} - has a greed-minimum (quirk: compared with state.t, not a count)
	if rg.HasMin && jsTruthyNum(rg.minVal()) && rg.minVal() > float64(st.t) {
		return false //greedy was too short
	}
	// 'foo+$' - if also an end-anchor, ensure we really reached the end
	if rg.End && st.startI+st.t != st.phraseLen {
		return false //greedy didn't reach the end
	}
	return true
}

// ---------------------------------------------------------------------------
// steps/logic/greedy.js

func (st *matchState) getGreedy(endReg *reg) (int, bool) {
	cp := *st.regs[st.r]
	cp.Start, cp.End = false, false
	start := st.t
	for ; st.t < len(st.terms); st.t++ {
		//stop for next-reg match
		if endReg != nil && doesMatch(st.terms[st.t], endReg, st.startI+st.t, st.phraseLen) {
			return st.t, true
		}
		count := st.t - start + 1
		// is it max-length now?
		if cp.HasMax && float64(count) == cp.maxVal() {
			return st.t, true
		}
		//stop here
		if !doesMatch(st.terms[st.t], &cp, st.startI+st.t, st.phraseLen) {
			// is it too short?
			if cp.HasMin && float64(count) < cp.minVal() {
				return 0, false
			}
			return st.t, true
		}
	}
	return st.t, true
}

func (st *matchState) greedyTo(nextReg *reg) (int, bool) {
	//if there's no next one, just go off the end!
	if nextReg == nil {
		return len(st.terms), true
	}
	//otherwise, we're looking for the next one
	for t := st.t; t < len(st.terms); t++ {
		if doesMatch(st.terms[t], nextReg, st.startI+t, st.phraseLen) {
			return t, true
		}
	}
	//guess it doesn't exist, then.
	return 0, false
}

func (st *matchState) isEndGreedy(rg *reg) bool {
	if rg.End && rg.Greedy {
		if st.startI+st.t < st.phraseLen-1 {
			tmp := *rg
			tmp.End = false
			if doesMatch(st.terms[st.t], &tmp, st.startI+st.t, st.phraseLen) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// term/doesMatch.js

// doesMatch is compromise's exported wrapMatch: the term matcher with
// '!negative' logic applied.
func doesMatch(t *Term, r *reg, index, length int) bool {
	if t == nil || r == nil {
		return false
	}
	result := doesMatchRaw(t, r, index, length)
	if r.Negative {
		return !result
	}
	return result
}

// aliasHasOwnProperty is `term.alias.hasOwnProperty(word)` on a JS array:
// true for "length" and for canonical indices below the length.
func aliasHasOwnProperty(alias []string, word string) bool {
	if word == "length" {
		return true
	}
	v, ok := isArrayIndexKey(word)
	return ok && v < uint64(len(alias))
}

// termField is term[reg.use].
func termField(t *Term, name string) (string, bool) {
	switch name {
	case "text":
		return t.Text, true
	case "normal":
		return t.Normal, true
	case "pre":
		return t.Pre, true
	case "post":
		return t.Post, true
	case "machine":
		return t.Machine, t.Machine != ""
	case "implicit":
		return t.Implicit, t.Implicit != ""
	case "switch":
		return t.Switch, t.Switch != ""
	case "chunk":
		return t.Chunk, t.Chunk != ""
	}
	return "", false
}

/** ignore optional/greedy logic, straight-up term match*/
func doesMatchRaw(term *Term, r *reg, index, length int) bool {
	// support '.'
	if r.Anything {
		return true
	}
	// support '^' (in parentheses)
	if r.Start && index != 0 {
		return false
	}
	// support '$' (in parentheses)
	if r.End && index != length-1 {
		return false
	}
	// (reg.id is never produced by parseMatch)
	//support a text match
	if r.Word != "" {
		// check case-sensitivity, etc
		if r.Use != "" {
			v, ok := termField(term, r.Use)
			return ok && r.Word == v
		}
		//match contractions, machine-form (undefined machine never equals)
		if term.Machine != "" && term.Machine == r.Word {
			return true
		}
		// term aliases for slashes and things
		if term.Alias != nil && aliasHasOwnProperty(term.Alias, r.Word) {
			return true
		}
		// support ~ fuzzy match (term.root is always undefined here)
		if r.Fuzzy {
			if fuzzyMatch(r.Word, term.Normal) >= r.minVal() {
				return true
			}
		}
		// match slashes and things
		for _, a := range term.Alias {
			if a == r.Word {
				return true
			}
		}
		//match either .normal or .text
		return r.Word == term.Text || r.Word == term.Normal
	}
	//support #Tag
	if r.tagDefined() {
		return term.Tags.Has(r.Tag)
	}
	//support @method
	if r.methodDefined() {
		if fn, ok := termMethods[r.Method]; ok && fn(term) {
			return true
		}
		return false
	}
	// (reg.pre / reg.post are never produced by parseMatch)
	//support /reg/
	if r.Regex != nil {
		str := term.Normal
		if r.Use != "" {
			v, ok := termField(term, r.Use)
			if !ok {
				v = "undefined" // RegExp.test(undefined) tests "undefined"
			}
			str = v
		}
		return r.Regex.MatchString(str)
	}
	//support <chunk>
	if r.chunkDefined() {
		return term.Chunk != "" && term.Chunk == r.Chunk
	}
	//support %Noun|Verb%
	if r.switchDefined() {
		return term.Switch != "" && term.Switch == r.Switch
	}
	// (reg.machine is never produced by parseMatch)
	//support {word/sense} (term.sense is always undefined)
	if r.Sense != "" {
		return false
	}
	// '{root}' regs: the inflect-root step is not ported, so they match nothing
	if r.Root != "" {
		return false
	}
	// support optimized (one|two)
	if r.FastOr != nil {
		// {work/verb} must be a verb
		if r.Pos != "" && !term.Tags.Has(r.Pos) {
			return false // JS returns null
		}
		// term.root || term.implicit || term.machine || term.normal
		str := term.Normal
		if term.Implicit != "" {
			str = term.Implicit
		} else if term.Machine != "" {
			str = term.Machine
		}
		return r.FastOr[str] || r.FastOr[term.Text]
	}
	//support slower (one|two)
	if r.Choices != nil {
		// quirk: JS calls wrapMatch(term, block) where each choices entry is an
		// ARRAY of regs, not a reg. Every property read on the array is
		// undefined, so each call returns false: an 'and' is true only when it
		// has no blocks ([].every), an 'or' is always false.
		if r.Operator == "and" {
			return len(r.Choices) == 0
		}
		return false
	}
	return false
}

// ---------------------------------------------------------------------------
// termMethods.js

var (
	startQuoteRe = regexp.MustCompile(`([\x{22}\x{FF02}\x{27}\x{201C}\x{2018}\x{201F}\x{201B}\x{201E}\x{2E42}\x{201A}\x{AB}\x{2039}\x{2035}\x{2036}\x{2037}\x{301D}\x{60}\x{301F}])`)
	endQuoteRe   = regexp.MustCompile(`([\x{22}\x{FF02}\x{27}\x{201D}\x{2019}\x{BB}\x{203A}\x{2032}\x{2033}\x{2034}\x{301E}\x{B4}])`)
	hasHyphenRe  = regexp.MustCompile(`^[-–—]$`)
	hasDashTMRe  = regexp.MustCompile(` [-–—]{1,3} `)
	titleCaseRe  = regexp.MustCompile(`^\p{Lu}[a-z'\x{C0}-\x{FF}]`)
	upperCaseRe  = regexp.MustCompile(`^\p{Lu}+$`)
)

func hasPost(t *Term, punct string) bool { return strings.Contains(t.Post, punct) }

// termMethods are called with '@hasComma' in the match syntax.
var termMethods = map[string]func(*Term) bool{
	"hasQuote":        func(t *Term) bool { return startQuoteRe.MatchString(t.Pre) || endQuoteRe.MatchString(t.Post) },
	"hasComma":        func(t *Term) bool { return hasPost(t, ",") },
	"hasPeriod":       func(t *Term) bool { return hasPost(t, ".") && !hasPost(t, "...") },
	"hasExclamation":  func(t *Term) bool { return hasPost(t, "!") },
	"hasQuestionMark": func(t *Term) bool { return hasPost(t, "?") || hasPost(t, "¿") },
	"hasEllipses":     func(t *Term) bool { return hasPost(t, "..") || hasPost(t, "…") },
	"hasSemicolon":    func(t *Term) bool { return hasPost(t, ";") },
	"hasColon":        func(t *Term) bool { return hasPost(t, ":") },
	"hasSlash":        func(t *Term) bool { return strings.Contains(t.Text, "/") },
	"hasHyphen":       func(t *Term) bool { return hasHyphenRe.MatchString(t.Post) || hasHyphenRe.MatchString(t.Pre) },
	"hasDash":         func(t *Term) bool { return hasDashTMRe.MatchString(t.Post) || hasDashTMRe.MatchString(t.Pre) },
	"hasContraction":  func(t *Term) bool { return t.Implicit != "" },
	"isAcronym":       func(t *Term) bool { return t.Tags.Has("Acronym") },
	"isKnown":         func(t *Term) bool { return t.Tags.Size() > 0 },
	"isTitleCase":     func(t *Term) bool { return titleCaseRe.MatchString(t.Text) },
	"isUpperCase":     func(t *Term) bool { return upperCaseRe.MatchString(t.Text) },
}

func init() {
	// aliases
	termMethods["hasQuotation"] = termMethods["hasQuote"]
}

// ---------------------------------------------------------------------------
// term/_fuzzy.js — damerau-levenshtein on UTF-16 code units

const fuzzyUndef = -1

func editDistance(a, b []uint16) int {
	aLength, bLength := len(a), len(b)
	// fail-fast
	if aLength == 0 {
		return bLength
	}
	if bLength == 0 {
		return aLength
	}
	limit := aLength
	if bLength > aLength {
		limit = bLength
	}
	limit++
	if absInt(aLength-bLength) > limit {
		return limit
	}
	// init the array (holes are undefined)
	matrix := make([][]int, limit)
	for i := range matrix {
		matrix[i] = make([]int, limit)
		for j := range matrix[i] {
			matrix[i][j] = fuzzyUndef
		}
		matrix[i][0] = i
	}
	for i := 0; i < limit; i++ {
		matrix[0][i] = i
	}
	for i := 1; i <= aLength; i++ {
		aIndex := a[i-1]
		for j := 1; j <= bLength; j++ {
			// Check the jagged distance total so far (undefined > 4 is false)
			if i == j && matrix[i][j] != fuzzyUndef && matrix[i][j] > 4 {
				return aLength
			}
			bIndex := b[j-1]
			cost := 1
			if aIndex == bIndex {
				cost = 0
			}
			min := matrix[i-1][j] + 1 // Deletion.
			if t := matrix[i][j-1] + 1; t < min {
				min = t // Insertion.
			}
			if t := matrix[i-1][j-1] + cost; t < min {
				min = t // Substitution.
			}
			if i > 1 && j > 1 && aIndex == b[j-2] && a[i-2] == bIndex {
				if t := matrix[i-2][j-2] + cost; t < min {
					matrix[i][j] = t
					continue
				}
			}
			matrix[i][j] = min
		}
	}
	return matrix[aLength][bLength]
}

// fuzzyMatch scores similarity from 0-1 (steps/length).
func fuzzyMatch(strA, strB string) float64 {
	if strA == strB {
		return 1
	}
	a := utf16.Encode([]rune(strA))
	b := utf16.Encode([]rune(strB))
	//don't even bother on tiny strings
	if len(a) < 3 || len(b) < 3 {
		return 0
	}
	steps := editDistance(a, b)
	length := len(a)
	if len(b) > length {
		length = len(b)
	}
	relative := 0.0
	if length != 0 {
		relative = float64(steps) / float64(length)
	}
	return 1 - relative
}
