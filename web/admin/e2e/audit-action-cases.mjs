import { readFileSync } from 'node:fs'

const [path, selectedAction] = process.argv.slice(2)
if (!path) {
  console.error('Usage: node e2e/audit-action-cases.mjs <case-audit-jsonl> [C01–C49]')
  process.exit(2)
}

let entries
try {
  entries = readFileSync(path, 'utf8').split('\n').filter(Boolean).map((line) => JSON.parse(line))
} catch (error) {
  console.error(`Could not read case audit: ${error}`)
  process.exit(2)
}

const expected = Array.from({ length: 49 }, (_, index) => `C${String(index + 1).padStart(2, '0')}`)
if (selectedAction && !expected.includes(selectedAction)) {
  console.error(`Unknown action: ${selectedAction}`)
  process.exit(2)
}

const missing = []
const withoutReceipt = []
for (const action of expected) {
  const cases = entries.filter((entry) => entry.commands.some((command) => command.action === action))
  if (cases.length === 0) missing.push(action)
  if (!cases.some((entry) => entry.commands.some((command) => command.action === action && command.status === 'succeeded' && command.receipt))) {
    withoutReceipt.push(action)
  }
  if (selectedAction === action) {
    console.log(`${action}: ${cases.length} test cases`)
    for (const entry of cases) {
      const outcomes = entry.commands.filter((command) => command.action === action)
        .map((command) => `${command.status}${command.receipt ? '+receipt' : ''}`)
      console.log(`- ${entry.test}: ${outcomes.join(', ')}`)
    }
  }
}

console.log(`Inspected ${entries.length} browser cases; ${49 - missing.length}/49 actions admitted and ${49 - withoutReceipt.length}/49 have a succeeded command with a receipt at case end.`)
console.log(`No case: ${missing.join(', ') || '(none)'}`)
console.log(`No succeeded receipt: ${withoutReceipt.join(', ') || '(none)'}`)
console.log('This audit does not prove that a UI interaction caused each command or validate its financial facts and recovery path.')
if (missing.length > 0 || withoutReceipt.length > 0) process.exitCode = 1
