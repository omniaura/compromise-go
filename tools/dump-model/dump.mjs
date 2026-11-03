// Dumps the compiled compromise model that the Go port embeds.
//
// The Go package ports compromise's algorithms; the data (lexicon, tag set,
// suffix/regex tables, switch clues and the post-tagger rules, already parsed
// by compromise's own match-syntax parser) comes straight from the JS world so
// the two cannot drift apart silently. Re-run after bumping compromise:
//
//   cd tools/dump-model && npm ci && node dump.mjs > ../../model/model.json
import nlp from 'compromise'
import { readFileSync } from 'node:fs'


const version = JSON.parse(readFileSync(new URL('./node_modules/compromise/package.json', import.meta.url))).version

const world = nlp.world()
const { model, methods } = world

const re = r => ({ source: r.source, flags: r.flags })

// regex tables are [RegExp, tag, reason?]
const regTable = arr => arr.map(([r, tag, reason]) => ({ ...re(r), tag, reason: reason || '' }))

const tagSet = {}
for (const [k, v] of Object.entries(model.one.tagSet)) {
  tagSet[k] = { not: v.not || [], parents: v.parents || [], children: v.children || [] }
}

const endsWith = {}
for (const [ch, arr] of Object.entries(model.two.endsWith)) {
  endsWith[ch] = regTable(arr)
}

// one parsed match token; Sets and RegExps become plain JSON
const reg = r => {
  const out = {}
  for (const [k, v] of Object.entries(r)) {
    if (v === undefined) continue
    if (k === 'choices') out.choices = v.map(block => block.map(reg))
    else if (k === 'fastOr') out.fastOr = Array.from(v)
    else if (k === 'regex') out.regex = re(v)
    else out[k] = v
  }
  return out
}

// The post-tagger net, built exactly as postTagger() builds it.
const matches = model.two.matches.map(m => ({ ...m }))
const net = methods.one.buildNet(matches, world)
const index = new Map(matches.map((m, i) => [m, i]))
const netOut = {
  matches: matches.map(m => {
    const o = {}
    for (const [k, v] of Object.entries(m)) {
      if (v === undefined) continue
      if (k === 'regs' || k === 'notIf') o[k] = v.map(reg)
      else o[k] = v
    }
    return o
  }),
  // Object.keys order is significant: getHooks() concatenates in this order.
  hooks: Object.keys(net.hooks).map(k => [k, net.hooks[k].map(m => index.get(m))]),
  always: net.always.map(m => index.get(m)),
}

const keys = obj => Object.keys(obj)

const out = {
  version,
  one: {
    tagSet,
    contractions: model.one.contractions,
    numberSuffixes: keys(model.one.numberSuffixes),
    aliases: model.one.aliases,
    abbreviations: keys(model.one.abbreviations),
    prefixes: keys(model.one.prefixes),
    suffixes: keys(model.one.suffixes),
    prePunctuation: keys(model.one.prePunctuation),
    postPunctuation: keys(model.one.postPunctuation),
    lexicon: model.one.lexicon,
    unicode: model.one.unicode,
    emoticons: keys(model.one.emoticons),
    multiCache: model.one._multiCache,
    frozenLex: model.one.frozenLex,
  },
  two: {
    switches: model.two.switches,
    clues: model.two.clues,
    neighbours: model.two.neighbours,
    regexNormal: regTable(model.two.regexNormal),
    regexText: regTable(model.two.regexText),
    regexNumbers: regTable(model.two.regexNumbers),
    endsWith,
    // index i holds the patterns of length i (index 0/1 are empty objects)
    suffixPatterns: model.two.suffixPatterns,
    prefixPatterns: model.two.prefixPatterns,
    orgWords: keys(model.two.orgWords),
    placeWords: keys(model.two.placeWords),
  },
  net: netOut,
}

process.stdout.write(JSON.stringify(out))
