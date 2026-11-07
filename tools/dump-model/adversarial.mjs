// Deterministically generates testdata/corpus/adversarial.txt: rule-focused
// documents that vary number forms, case, punctuation, quoting, contractions
// and repetition around the no-n-things and no-llm-speak triggers.
//
//   node adversarial.mjs > ../../testdata/corpus/adversarial.txt
const counts = [
  'two', 'Two', 'TWO', 'three', '3', '5', '10', '42', 'twenty-one', 'Twenty-One', 'twenty one', 'ninety-nine',
  'one hundred', 'two hundred and five', 'a hundred', 'a thousand', 'one thousand', 'a dozen', 'two dozen',
  'a couple', 'a couple of', 'a few', 'few', 'several', 'many', 'both', 'one', '1', '0', 'zero', 'first', '1st',
  'second', '2nd', 'third', '3rd', '5,000', '1.5', '3.5', '$5', '€5', '5%', '#5', '(5)', '"three"', '“three”',
  'five-ish', 'seven or eight', '7-8', '7 to 8', 'ten-plus', 'eleven', 'twelve', 'nineteen', 'forty', 'fourty',
  'half a dozen', 'two-thirds of', 'three quarters of the', 'million', 'a million', '2 million', 'seventy-seven',
  'II', 'IV', 'XII',
]
const nouns = ['things', 'thing', 'Things', 'THINGS', "thing's", "things'", 'things,', 'things.', 'things!', 'things?', 'things:', 'things-']
const frames = [
  n => `We found ${n} to fix.`,
  n => `${n} to know before you buy`,
  n => `Here are ${n}: speed, cost and reach.`,
  n => `"${n}," she said.`,
  n => `It's about ${n}, really.`,
  n => `I learned ${n} and then ${n} more.`,
]
const docs = []
let i = 0
for (const c of counts) {
  for (const noun of nouns) {
    const frame = frames[i++ % frames.length]
    docs.push(frame(`${c} ${noun}`))
  }
}
const phrases = [
  "It's important to note", "it’s important to note", 'It is important to note', 'IT\'S IMPORTANT TO NOTE',
  "Its important to note", "it's  important to note", "it's important\nto note", "it's important-to-note",
  'Delve into', 'delve  into', 'delve-into', 'delving into', 'delved into', 'delves into',
  'In the ever-evolving landscape', 'in the ever evolving landscape', 'in the ever—evolving landscape',
  "In today's digital age", 'in todays digital age', 'in today’s digital age',
  'A myriad of', 'a myriad of', 'myriad of', 'a wealth of', 'A WEALTH OF', 'in the realm of', 'In The Realm Of',
  'at the forefront of', 'navigate the complexities of', 'navigating the complexities of', 'sheds light on', 'shed light on',
  'underscores', 'underscore', 'Furthermore,', 'furthermore', 'Moreover;', 'In conclusion:', 'in-conclusion',
  'it is interesting to note', 'It should be noted', 'it is worth noting', "it's worth noting", 'As mentioned earlier',
  'as noted above', 'Going forward', 'going-forward', 'Moving forward', 'tapestry', 'tapestries', 'multifaceted',
  'multi-faceted', 'intricate', 'intricately', 'meticulous', 'meticulously', 'pivotal', 'crucial', 'crucially',
  'robust', 'robustness', '"robust"', '(crucial)', 'robust/pivotal',
]
const pframes = [
  p => `${p} that the plan works.`,
  p => `Honestly, ${p} the details matter.`,
  p => `Q: ${p}? A: no.`,
  p => `- ${p}\n- next item`,
  p => `${p}. ${p}.`,
]
i = 0
for (const p of phrases) {
  for (const f of pframes) docs.push(f(p))
}
// a few long mixed documents
for (let k = 0; k < 12; k++) {
  const parts = []
  for (let j = 0; j < 20; j++) parts.push(docs[(k * 97 + j * 31) % docs.length])
  docs.push(parts.join(' '))
}
process.stdout.write(docs.join('\n---\n') + '\n')
