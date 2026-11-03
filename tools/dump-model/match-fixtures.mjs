// Generates the JSON fixture inlined in match_test.go from real JS compromise:
//   cd tools/dump-model && node match-fixtures.mjs
import nlp from 'compromise'

const world = nlp.world()
const methods = nlp.methods()
const parse = (s) => methods.one.parseMatch(s, {}, world)

const replacer = (k, v) => {
  if (v instanceof Set) return Array.from(v)
  if (v instanceof RegExp) return { source: v.source, flags: v.flags }
  return v
}
const clean = (x) => JSON.parse(JSON.stringify(x, replacer))

const parsePatterns = [
  'delve into', "it's important to note", 'it is important to note', '#Value+ (thing|things)', '[#Value+] #Currency',
  '!once? [(a|an)] (#Duration|hundred)', '#Adverb? [#Adjective]', '(a|an) #Noun$', '^#Value', '#Noun{2,3}', '.', '*',
  '@hasComma', '/ed$/', '%Noun|Verb%', '(#Value && #Cardinal)', '[<money>#Value+ #Currency]', 'foo{,3}', 'foo{3,}',
  'foo{2}', 'foo{0,2}', '#Noun{}', '~happi~', '~(cat|dog)~', 're-do', 'walk-in-closet', 'co-op', '<Noun>', 'foo\\*',
  'a\\.b\\.c', 'foo /ab c/ bar', '(foo bar|baz)', '(foo  bar|baz)', '()', '(&&)', '[foo] [bar]', 'foo]', '[<g>foo]?',
  '!foo+', '!.', '.*', '.+', 'foo*', 'foo* bar$', '^foo$', '(^foo|bar$)', '(foo|bar)+', '[(a|b)]', 'Hello World', 'FOO',
  '/^[a-z]+$/', '(@hasComma|foo)', '(#Noun|foo)', '#adjective', 'one|two', '/a/ /b/', 'foo/bar', '/a/b/', 'x{1,2}?',
  'foo   bar', '  leading and trailing  ', '[<a> foo] bar', '!(a|b)', '(a|b)$', '^(a|b)', '#Noun+? foo', '[#Noun] [#Verb] [#Adverb]',
  'x (a b|c d) y', '(a|!b)', '(a|b?)', '(a|~bb~)', '~ab~', 'foo$ bar', '[foo bar] baz]', '[[foo]]', 'a{2,4}+', '$', '^', '?',
  '%%', '<>', '@', '#', '~~~', '//', 'ß-ok', 'ÉCOLE', '(a|b c|#Noun)', 'x\\*y\\*', '\\*', 'the (big|red)+ dog', 'foo /x y/', 'a (b c)+ d',
]

const sentences = [
  "It's important to note that we delve into the details.",
  'It is important to note the costs, sadly.',
  'I paid five hundred dollars for two things, and once a hundred years passed.',
  'He walked quickly and was very happy.',
  'The big red dog barked at a cat',
  'Seven dollars is a lot of money for an apple',
  "We've finished the job, sadly.",
  'the 3 things in 20 thing',
  'a big big big red dog walked an hour',
]

const matchCases = [
  ['delve into'], ["it's important to note"], ['it is important to note'], ['#Value+ (thing|things)'],
  ['[#Value+] #Currency', 0], ['[#Value+] #Currency'], ['!once? [(a|an)] (#Duration|hundred)', 0], ['!once? [(a|an)] (#Duration|hundred)'],
  ['#Adverb? [#Adjective]', 0], ['(a|an) #Noun$'], ['^#Value'], ['#Noun{2,3}'], ['.'], ['*'], ['@hasComma'], ['/ed$/'],
  ['%Noun|Verb%'], ['(#Value && #Cardinal)'], ['[<money>#Value+ #Currency]', 'money'], ['[<money>#Value+ #Currency]'],
  ["we've"], ['we have'], ['@hasContraction finished'], ['!#Noun+ #Noun'], ['.* dollars'], ['the .{1,2} dog'], ['~happi~'],
  ['(big red|the) dog'], ['#Adjective+$'], ['^the'], ['#Determiner [#Adjective+] #Noun', 0], ['[#Determiner] [#Adjective]'],
  ['[#Determiner] [#Adjective]', 1], ['big+ red'], ['(big)+ red'], ['#Adjective{2}'], ['!#Adjective? dog'], ['a !big? red? dog'],
  ['#Noun', null, false, 'dog'], ['#Noun', null, true], ['it is'], ["it's"], ['it'], ['is'], ['(it|is) important'],
  ['#Copula'], ['@isTitleCase'], ['@hasPeriod'], ['.+ dog$'], ['big* red'], ['(a|an)? #Noun'], ['#Value #Value? #Plural'],
  ['^. .'], ['* dog'], ['[*] dog', 0], ['!walked+ walked'], ['!dog{1,2}'], ['(#Adjective && big)'], ['(red dog|big)'], ['#Adjective (dog|cat)$'],
  ['hundred'], ['(hundred|five)'], ['.* (red dog|zz)'], ['big? (red dog|zz)'], ['(dog|cat)$'], ['a (big|zz)+ red'], ['(#Adjective && big)$'], ['(big|red) (#Noun && dog)'], ['[<adj>#Adjective+] [<n>#Noun]'], ['[<z>#Adjective] [<1>#Noun]'], ['#Adjective+ dog', null, false, 'red'], ['^#Noun', null, true], ['big{2,}'], ['!big{2,3} dog'], ['big{0,1} red'], ['the !small? tiny? big red dog'], ['big? (&&) red'], ['.* (&&)'], ['the (&&)? big'], ['big .* (&&)?'], ['the .* (big red|zz) dog'], ['big{3}'], ['the big{2,}'], ['the big{3,}'], ['a big{2}'], ['a big{4,} red'], ['a big{5,} red'], ['red? (big red|zz) dog'], ['[.] [.]'], ['[.] [.]', 1], ['%Adj|Present%'], ['walked'], ['walk'],
]

const docs = sentences.map(text => {
  const doc = nlp(text)
  return doc
})
// one doc of all sentences so multi-sentence behaviour is covered
const big = nlp(sentences.join(' '))
const termOut = big.docs.map(terms => terms.map(t => {
  const o = { text: t.text, pre: t.pre, post: t.post, normal: t.normal, tags: Array.from(t.tags) }
  if (t.machine !== undefined) o.machine = t.machine
  if (t.implicit !== undefined) o.implicit = t.implicit
  if (t.alias !== undefined) o.alias = t.alias
  if (t.switch !== undefined) o.switch = t.switch
  if (t.chunk !== undefined) o.chunk = t.chunk
  if (t.root !== undefined) o.root = t.root
  return o
}))
const cache = methods.one.cacheDoc(big.document).map(s => Array.from(s))

const matches = matchCases.map(([pattern, group, justOne, notIf]) => {
  const regs = parse(pattern)
  const todo = { regs }
  if (group !== undefined && group !== null) todo.group = group
  if (justOne) todo.justOne = true
  if (notIf) todo.notIf = parse(notIf)
  const noCache = methods.one.match(big.docs, todo, null)
  const withCache = methods.one.match(big.docs, todo, methods.one.cacheDoc(big.document))
  const strip = r => ({ ptrs: r.ptrs.map(p => p.slice(0, 3)), byGroup: Object.fromEntries(Object.entries(r.byGroup).map(([k, v]) => [k, v.map(p => p.slice(0, 3))])), byGroupOrder: Object.keys(r.byGroup) })
  const out = { pattern, ...strip(noCache) }
  if (group !== undefined && group !== null) out.group = String(group)
  if (justOne) out.justOne = true
  if (notIf) out.notIf = notIf
  const wc = strip(withCache)
  if (JSON.stringify(wc) !== JSON.stringify(strip(noCache))) out.withCache = wc
  return out
})

// hand-built terms exercising alias/machine/method/fuzzy/chunk quirks
const custom = [
  [
    { text: 'Foo/Bar', pre: '"', post: '" ', normal: 'foo/bar', alias: ['foo', 'bar'], tags: ['Noun'] },
    { text: 'walking', pre: '', post: ' - ', normal: 'walking', tags: ['Gerund', 'Verb'], chunk: 'Verb' },
    { text: "Isn't", pre: '', post: ' ', normal: "isn't", machine: 'is', implicit: 'is', tags: ['Copula'] },
    { text: '', pre: '', post: '', normal: '', machine: 'not', implicit: 'not', tags: ['Negative'] },
    { text: 'NASA', pre: '', post: '... ', normal: 'nasa', tags: ['Acronym', 'Organization'] },
    { text: 'empty', pre: '-', post: '?! ', normal: 'empty', alias: [], tags: [] },
    { text: 'go', pre: '', post: ';', normal: 'go', tags: ['Verb'], switch: 'Noun|Verb' },
  ],
  [
    { text: 'Long', pre: '', post: ': ', normal: 'long', tags: ['Adjective'], chunk: 'Noun' },
    { text: 'words', pre: '', post: '. ', normal: 'words', tags: ['Plural', 'Noun'], chunk: 'Noun' },
  ],
]
const customDocs = custom.map(s => s.map(t => Object.assign({}, t, { tags: new Set(t.tags) })))
const customPatterns = ['length', '0', '1', '2', 'foo', 'bar', 'foo/bar', 'is', 'is not', "isn't", 'not', '(foo|walk)', '(is|zz) not',
  '@hasQuote', '@hasQuotation', '@hasHyphen', '@hasDash', '@isUpperCase', '@isKnown', '@hasEllipses', '@hasQuestionMark', '@hasSlash',
  '@hasSemicolon', '@hasColon', '@isAcronym', '@hasExclamation', '@toString', '@hasTitleCase', '@isTitleCase', '@hasComma', '@hasPeriod',
  '@hasContraction', '@hasContraction #Organization', '~walkin~', '~walkn~', '~(walkin|nope)~', '<Noun>', '<Verb>', '%Noun|Verb%',
  '#Copula #Negative', "isn't nasa", 'is #Negative', '(#Copula|zz) nasa', '!#Noun+', '#Noun+$', 'words$', '(long|words)$', '^(walking|long)',
  '(walking|long)', '(long|words)? ^(long|words)', '(#Plural && words)$', '(long && #Adjective)$', '/^w/', '/s$/', '.{2}', '.{,2}', '[.{2,}] words', '* nasa', '. [*] .', '. [*] .', '!.']
const customMatches = customPatterns.map((pattern, i) => {
  const todo = { regs: parse(pattern) }
  if (i === customPatterns.length - 2) todo.group = 0
  const r = methods.one.match(customDocs, todo, null)
  const out = { pattern, ptrs: r.ptrs.map(p => p.slice(0, 3)), byGroup: Object.fromEntries(Object.entries(r.byGroup).map(([k, v]) => [k, v.map(p => p.slice(0, 3))])), byGroupOrder: Object.keys(r.byGroup) }
  if (todo.group !== undefined) out.group = String(todo.group)
  return out
})

const parsed = parsePatterns.map(p => ({ pattern: p, regs: clean(parse(p)) }))
const prefixes = Object.keys(world.model.one.prefixes)
console.log(JSON.stringify({ prefixes, parse: parsed, terms: termOut, cache, matches, customTerms: custom, customMatches }, null, 0))
