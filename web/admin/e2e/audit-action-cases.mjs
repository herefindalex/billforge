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
const withoutBrowserRequest = []
const withoutBrowserReceipt = []
for (const action of expected) {
  const cases = entries.filter((entry) => entry.commands.some((command) => command.action === action))
  if (cases.length === 0) missing.push(action)
  if (!cases.some((entry) => entry.commands.some((command) => command.action === action && command.status === 'succeeded' && command.receipt))) {
    withoutReceipt.push(action)
  }
  if (!entries.some((entry) => entry.browser_actions?.includes(action))) withoutBrowserRequest.push(action)
  if (!cases.some((entry) => entry.browser_actions?.includes(action) && entry.browser_receipt_actions?.includes(action) && entry.commands.some((command) => command.action === action && command.status === 'succeeded' && command.receipt))) {
    withoutBrowserReceipt.push(action)
  }
  if (selectedAction === action) {
    console.log(`${action}: ${cases.length} test cases`)
    for (const entry of cases) {
      const outcomes = entry.commands.filter((command) => command.action === action)
        .map((command) => `${command.status}${command.receipt ? '+receipt' : ''}`)
      console.log(`- ${entry.test}: ${outcomes.join(', ')}; browser request: ${entry.browser_actions?.includes(action) ? 'yes' : 'no'}; matched browser receipt: ${entry.browser_receipt_actions?.includes(action) ? 'yes' : 'no'}`)
    }
  }
}

console.log(`Inspected ${entries.length} browser cases; ${49 - missing.length}/49 actions admitted and ${49 - withoutReceipt.length}/49 have a succeeded command with a receipt at case end.`)
console.log(`${49 - withoutBrowserReceipt.length}/49 actions have a browser request matched by idempotency key to a succeeded command receipt.`)
console.log(`No case: ${missing.join(', ') || '(none)'}`)
console.log(`No succeeded receipt: ${withoutReceipt.join(', ') || '(none)'}`)
console.log(`No browser request: ${withoutBrowserRequest.join(', ') || '(none)'}`)
console.log(`No browser request matched to a succeeded receipt: ${withoutBrowserReceipt.join(', ') || '(none)'}`)
console.log('This audit does not validate the financial facts or recovery path of each action.')
if (missing.length > 0 || withoutReceipt.length > 0 || withoutBrowserRequest.length > 0 || withoutBrowserReceipt.length > 0) process.exitCode = 1
