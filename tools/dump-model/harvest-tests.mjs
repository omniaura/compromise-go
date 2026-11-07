// Collects sentence-like string literals from compromise's own test suite
// (MIT, Spencer Kelly) into a one-document-per-line corpus. The Penn/UD
// tagger sample (_pennSample.js) is skipped: it is not ours to redistribute.
//
//   node harvest-tests.mjs <compromise checkout>/tests > ../../testdata/corpus/compromise-tests.txt
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'

const root = process.argv[2]
const files = []
const walk = dir => {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name)
    if (statSync(p).isDirectory()) walk(p)
    else if (name.endsWith('.js') && name !== '_pennSample.js') files.push(p)
  }
}
walk(root)
files.sort()

const seen = new Set()
const lit = /'((?:[^'\\\n]|\\.){8,400})'|"((?:[^"\\\n]|\\.){8,400})"|`((?:[^`\\$]|\\.){8,400})`/g
for (const f of files) {
  const src = readFileSync(f, 'utf8')
  for (const m of src.matchAll(lit)) {
    let s = m[1] ?? m[2] ?? m[3]
    s = s.replace(/\\(['"`\\])/g, '$1').replace(/\\n/g, ' ')
    if (!/ /.test(s) || !/[a-z]{2}/i.test(s)) continue
    if (/^[#@%~\[\(\/!^]|[#@{]\w/.test(s)) continue // match patterns, not prose
    if (/\n/.test(s) || seen.has(s)) continue
    seen.add(s)
    process.stdout.write(s + '\n')
  }
}
