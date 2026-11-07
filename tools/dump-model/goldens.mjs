// Writes the parity goldens the Go tests compare against: for every document
// in a corpus file, what JS compromise produces.
//
//   node goldens.mjs ../../testdata/corpus/drafts.txt > ../../testdata/golden/drafts.jsonl
//
// Corpus format: documents separated by a line holding only "---" (or, with
// --lines, one document per line).
//
// noLlmSpeak and noNThings below are the check() bodies of
// @faircopy/rules-nlp 1.26.0 with the source map set to identity, so their
// spans are what Fair Copy reports for plain text. noNThingsFixed is the
// corrected no-n-things that the Go port implements: it counts each match on
// its own clone, from the value right before the noun, instead of re-finding
// the match by text on a document that toNumber() has already rewritten.
import nlp from 'compromise'
import { readFileSync } from 'node:fs'

const args = process.argv.slice(2)
const byLine = args.includes('--lines')
const file = args.find(a => !a.startsWith('--'))
const raw = readFileSync(file, 'utf8')
const docs = (byLine ? raw.split('\n') : raw.split(/\n---\n/)).map(s => (byLine ? s : s.trim())).filter(s => s.trim())

const LLM_PHRASES = [
  'delve into', "it's important to note", "it's worth noting", 'in the ever-evolving landscape',
  "in today's digital age", 'a myriad of', 'a wealth of', 'in the realm of', 'at the forefront of',
  'navigate the complexities of', 'sheds light on', 'underscores', 'furthermore', 'moreover',
  'in conclusion', 'it is interesting to note', 'it should be noted', 'it is worth noting',
  'as mentioned earlier', 'as noted above', 'going forward', 'moving forward', 'tapestry',
  'multifaceted', 'intricate', 'meticulous', 'pivotal', 'crucial', 'robust',
]

const span = (entry) => {
  const terms = entry.terms
  if (!terms?.length) return null
  const a = terms[0].offset
  const b = terms[terms.length - 1].offset
  const start = a.start
  const end = b.start + b.length
  return end <= start ? null : [start, end]
}

function noLlmSpeak(text) {
  const out = []
  const doc = nlp(text)
  for (const phrase of LLM_PHRASES) {
    const json = doc.match(phrase).json({ offset: true, text: true, terms: { offset: true, text: true } })
    for (const entry of json) {
      const s = span(entry)
      if (s) out.push(s)
    }
  }
  return out.sort((l, r) => l[0] - r[0])
}

function noNThings(text) {
  const out = []
  if (text.length === 0) return out
  const doc = nlp(text)
  const matches = doc.match('#Value+ (thing|things)')
    .json({ offset: true, text: true, terms: { offset: true, text: true, tags: true } })
  for (const entry of matches) {
    const terms = entry.terms ?? []
    if (terms.length < 2) continue
    const s = span(entry)
    if (!s) continue
    const matchedText = text.slice(s[0], s[1])
    const countOut = doc.match(matchedText).values?.().toNumber().out('array') ?? []
    const countText = countOut[0]
    const count = typeof countText === 'string' ? Number(countText) : Number.NaN
    if (!Number.isFinite(count) || count < 2) continue
    out.push(s)
  }
  return out.sort((l, r) => l[0] - r[0])
}

function noNThingsFixed(text) {
  const out = []
  if (text.length === 0) return out
  const doc = nlp(text)
  doc.match('#Value+ (thing|things)').forEach(m => {
    const entry = m.json({ offset: true, terms: { offset: true } })[0]
    if (!entry || (entry.terms ?? []).length < 2) return
    const s = span(entry)
    if (!s) return
    const countOut = m.clone().values().toNumber().out('array')
    // brackets and quotes around a count ("(5) things") don't change it
    const countText = (countOut[countOut.length - 1] ?? '').replace(/^[(\[{"'“‘]+|[)\]}"'”’]+$/g, '')
    const count = countText ? Number(countText) : Number.NaN
    if (!Number.isFinite(count) || count < 2) return
    out.push(s)
  })
  return out.sort((l, r) => l[0] - r[0])
}

for (const text of docs) {
  const doc = nlp(text)
  const json = doc.json({ offset: true, terms: { offset: true, tags: true, normal: true, implicit: true, machine: true } })
  const sentences = json.map(s => s.terms.map(t => [
    t.text, t.pre, t.post, t.normal || '', t.implicit || '', t.machine || '', t.offset.start, t.tags,
  ]))
  const values = nlp(text).values()
  const row = {
    text,
    sentences,
    values: values.out('array'),
    toNumber: nlp(text).values().toNumber().out('array'),
    llm: noLlmSpeak(text),
    nthings: noNThings(text),
    nthingsFixed: noNThingsFixed(text),
  }
  process.stdout.write(JSON.stringify(row) + '\n')
}
