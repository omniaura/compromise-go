package compromise

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// Port of compromise/src/1-one/sweep and src/2-two/postTagger: the 570
// match-and-tag rules, pre-parsed by compromise and embedded as data.

type netMatchJSON struct {
	Match    string          `json:"match"`
	Tag      tagList         `json:"tag"`
	UnTag    string          `json:"unTag"`
	Chunk    string          `json:"chunk"`
	Reason   string          `json:"reason"`
	Regs     []*reg          `json:"regs"`
	NotIf    []*reg          `json:"notIf"`
	Needs    []string        `json:"needs"`
	Wants    []string        `json:"wants"`
	MinWant  int             `json:"minWant"`
	MinWords int             `json:"minWords"`
	Group    json.RawMessage `json:"group"`
	IfNo     []string        `json:"ifNo"`
	Safe     bool            `json:"safe"`
	Freeze   bool            `json:"freeze"`
}

type netMatch struct {
	netMatchJSON
	group    string
	hasGroup bool
	// tagStr is the tag when it is a single string (it may still hold
	// several space-separated tags, '#Percent #Unit'); "" for an array.
	tagStr string
}

type hookEntry struct {
	key     string
	matches []int
}

type net struct {
	matches []*netMatch
	hooks   []hookEntry
	always  []int

	hookIndex map[string]int // hook key -> position in hooks
	strID     []int          // match -> id of its match string
	numStr    int
}

func loadNet(matches []netMatchJSON, hooks []json.RawMessage, always []int) (*net, error) {
	n := &net{always: always}
	for i := range matches {
		nm := &netMatch{netMatchJSON: matches[i]}
		if len(nm.Group) > 0 && string(nm.Group) != "null" {
			nm.hasGroup = true
			var num float64
			if err := json.Unmarshal(nm.Group, &num); err == nil {
				nm.group = strconv.FormatFloat(num, 'f', -1, 64)
			} else if err := json.Unmarshal(nm.Group, &nm.group); err != nil {
				return nil, fmt.Errorf("match %q: bad group %s", nm.Match, nm.Group)
			}
		}
		if len(nm.Tag) == 1 {
			nm.tagStr = nm.Tag[0]
		}
		n.matches = append(n.matches, nm)
	}
	for _, raw := range hooks {
		var pair [2]json.RawMessage
		if err := json.Unmarshal(raw, &pair); err != nil {
			return nil, err
		}
		var h hookEntry
		if err := json.Unmarshal(pair[0], &h.key); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(pair[1], &h.matches); err != nil {
			return nil, err
		}
		n.hooks = append(n.hooks, h)
	}
	n.index()
	return n, nil
}

// cacheDoc is methods.one.cacheDoc: every word form and #tag in a sentence.
func cacheDoc(document [][]*Term) []map[string]bool {
	out := make([]map[string]bool, len(document))
	for n, terms := range document {
		out[n] = make(map[string]bool, len(terms)*4)
		fillCache(out[n], terms)
	}
	return out
}

func fillCache(items map[string]bool, terms []*Term) {
	for _, t := range terms {
		if t.Normal != "" {
			items[t.Normal] = true
		}
		if t.Switch != "" {
			items["%"+t.Switch+"%"] = true
		}
		if t.Implicit != "" {
			items[t.Implicit] = true
		}
		if t.Machine != "" {
			items[t.Machine] = true
		}
		for _, a := range t.Alias {
			items[a] = true
		}
		for _, tg := range t.Tags.list {
			items["#"+tg] = true
		}
	}
}

type sweepHit struct {
	m   *netMatch
	ptr pointer
}

// index builds the lookup tables bulkMatch uses instead of scanning every
// hook key and keeping a per-sentence string set.
func (n *net) index() {
	n.hookIndex = make(map[string]int, len(n.hooks))
	for i, h := range n.hooks {
		n.hookIndex[h.key] = i
	}
	byString := map[string]int{}
	n.strID = make([]int, len(n.matches))
	for i, mt := range n.matches {
		id, ok := byString[mt.Match]
		if !ok {
			id = len(byString)
			byString[mt.Match] = id
		}
		n.strID[i] = id
	}
	n.numStr = len(byString)
}

// bulkMatch is sweep(): find every rule that matches, before tagging any.
// Candidate order matters (rules apply in it), so this keeps getHooks'
// order: hook keys in Object.keys order, first object per match string.
func (n *net) bulkMatch(document [][]*Term) []sweepHit {
	var results []sweepHit
	haves := make(map[string]bool, 64)
	seen := make([]int, n.numStr) // generation stamps, so nothing is cleared per sentence
	var hookIdx []int
	var list []*netMatch
	for d, terms := range document {
		clear(haves)
		fillCache(haves, terms)
		gen := d + 1
		hookIdx = hookIdx[:0]
		for key := range haves {
			if i, ok := n.hookIndex[key]; ok {
				hookIdx = append(hookIdx, i)
			}
		}
		sort.Ints(hookIdx)
		list = list[:0]
		for _, hi := range hookIdx {
			for _, idx := range n.hooks[hi].matches {
				sid := n.strID[idx]
				if seen[sid] == gen {
					continue
				}
				seen[sid] = gen
				mt := n.matches[idx]
				if !mt.fits(haves) {
					continue
				}
				list = append(list, mt)
			}
		}
		for _, idx := range n.always {
			list = append(list, n.matches[idx])
		}
		for _, mt := range list {
			if len(terms) < mt.MinWords {
				continue
			}
			todo := matchTodo{regs: mt.Regs, group: mt.group, hasGroup: mt.hasGroup, notIf: mt.NotIf}
			res := runMatch([][]*Term{terms}, todo, nil)
			for _, p := range res.ptrs {
				p[0] = d
				results = append(results, sweepHit{m: mt, ptr: p})
			}
		}
	}
	return results
}

// fits is trimDown: every need present, no ifNo present, enough wants.
func (mt *netMatch) fits(haves map[string]bool) bool {
	for _, need := range mt.Needs {
		if !haves[need] {
			return false
		}
	}
	for _, no := range mt.IfNo {
		if haves[no] {
			return false
		}
	}
	if len(mt.Wants) > 0 {
		found := 0
		for _, w := range mt.Wants {
			if haves[w] {
				found++
			}
		}
		if found < mt.MinWant {
			return false
		}
	}
	return true
}

// bulkTagger applies the found rules in order.
func (m *model) bulkTagger(hits []sweepHit, document [][]*Term) {
	for _, h := range hits {
		mt := h.m
		if len(mt.Tag) == 0 && mt.Chunk == "" && mt.UnTag == "" {
			continue
		}
		terms := document[h.ptr[0]][h.ptr[1]:h.ptr[2]]
		if len(terms) == 0 {
			continue
		}
		if mt.Safe {
			// An array tag is looked up as "A,B", which is never in the tag set.
			if mt.tagStr != "" && !m.canBe(terms, mt.tagStr) {
				continue
			}
			if terms[len(terms)-1].Post == "-" {
				continue
			}
		}
		if len(mt.Tag) > 0 {
			m.setTags(terms, mt.Tag, mt.Safe)
			if mt.tagStr == "Noun" {
				last := terms[len(terms)-1]
				if looksPlural(last.Text) {
					m.setTag([]*Term{last}, "Plural", mt.Safe)
				} else {
					m.setTag([]*Term{last}, "Singular", mt.Safe)
				}
			}
			if mt.Freeze {
				for _, t := range terms {
					t.Frozen = true
				}
			}
		}
		if mt.UnTag != "" {
			// The sweep passes `world` where unTag expects the tag set, so
			// the children lookup never finds anything: only the tag itself
			// (or everything, for '*') is removed.
			tag := mt.UnTag
			for _, t := range terms {
				if t.Frozen {
					continue
				}
				if tag == "*" {
					t.Tags.Clear()
					continue
				}
				t.Tags.Delete(trimHash(tag))
			}
		}
		if mt.Chunk != "" {
			for _, t := range terms {
				t.Chunk = mt.Chunk
			}
		}
	}
}

func trimHash(s string) string {
	if len(s) > 0 && s[0] == '#' {
		return s[1:]
	}
	return s
}

func (m *model) postTagger(document [][]*Term) {
	clauses := quickSplit(document)
	hits := m.net.bulkMatch(clauses)
	m.bulkTagger(hits, clauses)
	unfreeze(document)
}
