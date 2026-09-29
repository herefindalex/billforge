import { execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { startLocalAdmin } from './server'

test('reverse price migration uses a new batch and restores the old price at renewal', async ({ page }) => {
  const admin = await startLocalAdmin()
  const base = admin.baseURL
  const read = (sql: string, arg: string): string => {
    const script = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); print(db.execute(sys.argv[2],(sys.argv[3],)).fetchone()[0])'
    return execFileSync('python3', ['-c', script, admin.commercePath, sql, arg], { encoding: 'utf8' }).trim()
  }
  const complete = () => expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
  const confirmPreview = async (button: string) => {
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByRole('button', { name: '建立預覽' })).toBeEnabled()
    await page.getByRole('button', { name: button }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: button }).click()
    await complete()
  }
  const fill = async (fields: [string, string][]) => {
    for (const [label, value] of fields) await page.getByLabel(label, { exact: true }).fill(value)
  }
  const submitSelection = async (priceID: string, at: string) => {
    await page.goto(`${base}/admin/catalog-selections/new`)
    if (await page.getByRole('button', { name: '建立預覽' }).isDisabled()) {
      await page.getByRole('button', { name: '執行另一個操作' }).click()
    }
    await fill([['方案 ID', 'pro'], ['Cohort', 'default'], ['生效時間（UTC）', at], ['價格版本 ID', priceID]])
    await confirmPreview('確認選價')
  }
  const plan = async (migrationID: string, priceID: string, subscriptionID: string) => {
    await page.goto(`${base}/admin/price-migrations/new`)
    if (await page.getByRole('button', { name: '建立預覽' }).isDisabled()) {
      await page.getByRole('button', { name: '執行另一個操作' }).click()
    }
    await fill([
      ['遷移批次 ID', migrationID], ['Cohort', 'default'], ['目標價格版本 ID', priceID],
      ['訂閱 ID（逗號或換行分隔）', subscriptionID],
    ])
    await confirmPreview('確認建立遷移批次')
  }
  const renew = async () => {
    await page.goto(`${base}/admin/jobs/renewals`)
    if (await page.getByRole('button', { name: '建立預覽' }).isDisabled()) {
      await page.getByRole('button', { name: '執行另一個操作' }).click()
    }
    await confirmPreview('確認續約批次')
  }
  const pay = async (operationID: string) => {
    await page.goto(`${base}/admin/payments/${operationID}/dispatch`)
    await confirmPreview('確認送出付款')
  }
  const boundary = (nanoseconds: bigint, offset = 0n) =>
    new Date(Number((nanoseconds + offset) / 1_000_000n)).toISOString()

  try {
    await page.goto(`${base}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(admin.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(admin.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
    const session = await (await page.request.get(`${base}/admin/api/session`)).json() as { csrf_token: string }
    const setClock = async (at: string) => {
      const response = await page.request.post(`${base}/admin/api/commands`, {
        headers: { Origin: base, 'X-CSRF-Token': session.csrf_token, 'Idempotency-Key': randomUUID() },
        data: { action_id: 'C46', target_id: '', payload: { mode: 'fixed', value_utc: at } },
      })
      expect(response.ok(), `${response.status()} ${await response.text()}`).toBe(true)
    }

    const customerID = `reverse-migration-${randomUUID()}`
    await page.goto(`${base}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
    await page.getByRole('textbox', { name: /席次/ }).fill('5')
    await page.getByRole('button', { name: '建立報價' }).click()
    await complete()
    const quoteID = read('SELECT id FROM quotes WHERE customer_id=?', customerID)
    await page.goto(`${base}/admin/quotes/${quoteID}/accept`)
    await page.getByRole('button', { name: '預覽接受' }).click()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await complete()
    const subscriptionID = read('SELECT id FROM subscriptions WHERE quote_id=?', quoteID)
    await pay(read('SELECT o.id FROM payment_operations o JOIN invoices i ON i.id=o.invoice_id WHERE i.subscription_id=?', subscriptionID))
    expect(read('SELECT status FROM subscriptions WHERE id=?', subscriptionID)).toBe('active')

    const firstEnd = BigInt(read('SELECT period_end FROM billing_periods WHERE subscription_id=? AND period_index=0', subscriptionID))
    const newPriceID = `pro-forward-${randomUUID()}`
    await page.goto(`${base}/admin/prices/pro/new`)
    await fill([
      ['價格版本 ID', newPriceID], ['版本號', '2'], ['固定金額（最小單位）', '6000'],
      ['每席金額（最小單位）', '1000'], ['包含任務量', '20000'],
      ['超額費率分子', '1'], ['超額費率分母', '10'], ['生效起點（UTC）', boundary(firstEnd)],
    ])
    await confirmPreview('確認發布價格')
    await submitSelection(newPriceID, boundary(firstEnd))
    const forwardID = `forward-${randomUUID()}`
    await plan(forwardID, newPriceID, subscriptionID)
    await setClock(boundary(firstEnd, 1_000_000_000n))
    await renew()
    expect(read('SELECT status FROM price_migrations WHERE id=?', forwardID)).toBe('completed')
    expect(read('SELECT price_version_id FROM pricing_assignments WHERE subscription_id=? AND effective_end IS NULL', subscriptionID)).toBe(newPriceID)
    expect(read('SELECT i.total_minor FROM invoices i JOIN billing_periods p ON p.invoice_id=i.id WHERE p.subscription_id=? AND p.period_index=1', subscriptionID)).toBe('11000')
    await pay(read('SELECT o.id FROM payment_operations o JOIN billing_periods p ON p.invoice_id=o.invoice_id WHERE p.subscription_id=? AND p.period_index=1', subscriptionID))

    const secondEnd = BigInt(read('SELECT period_end FROM billing_periods WHERE subscription_id=? AND period_index=1', subscriptionID))
    await submitSelection('pro-v1', boundary(secondEnd))
    const reverseID = `reverse-${randomUUID()}`
    await plan(reverseID, 'pro-v1', subscriptionID)
    expect(read('SELECT from_price_version_id FROM price_migration_items WHERE migration_id=?', reverseID)).toBe(newPriceID)
    await setClock(boundary(secondEnd, 1_000_000_000n))
    await renew()
    expect(read('SELECT status FROM price_migrations WHERE id=?', reverseID)).toBe('completed')
    expect(read('SELECT price_version_id FROM pricing_assignments WHERE subscription_id=? AND effective_end IS NULL', subscriptionID)).toBe('pro-v1')
    expect(read('SELECT source_migration_id FROM pricing_assignments WHERE subscription_id=? AND effective_end IS NULL', subscriptionID)).toBe(reverseID)
    expect(read('SELECT i.total_minor FROM invoices i JOIN billing_periods p ON p.invoice_id=i.id WHERE p.subscription_id=? AND p.period_index=2', subscriptionID)).toBe('10000')
    expect(read('SELECT COUNT(*) FROM pricing_assignments WHERE subscription_id=?', subscriptionID)).toBe('3')
  } finally {
    await admin.stop()
  }
})
