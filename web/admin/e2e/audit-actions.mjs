import { readFileSync } from 'node:fs'

const path = process.argv[2]
if (!path) {
  console.error('Usage: node e2e/audit-actions.mjs <action-audit-jsonl>')
  process.exit(2)
}

let entries
try {
  entries = readFileSync(path, 'utf8').split('\n').filter(Boolean).map((line) => JSON.parse(line))
} catch (error) {
  console.error(`Could not read action audit: ${error}`)
  process.exit(2)
}

const observed = new Set(entries.flatMap((entry) => entry.actions))
const expected = Array.from({ length: 49 }, (_, index) => `C${String(index + 1).padStart(2, '0')}`)
const missing = expected.filter((action) => !observed.has(action))

console.log(`Inspected ${entries.length} E2E databases; ${observed.size}/49 actions reached command admission.`)
console.log(`Observed: ${[...observed].sort().join(', ') || '(none)'}`)
console.log(`Missing: ${missing.join(', ') || '(none)'}`)
console.log('Command admission alone does not establish UI, receipt, recovery, or financial correctness.')
if (missing.length > 0) process.exitCode = 1
