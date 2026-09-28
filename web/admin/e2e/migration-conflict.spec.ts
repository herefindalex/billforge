import { execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('migration detail explains a revision conflict without undoing an applied item', async ({ page }) => {
  const admin = await startLocalAdmin()
  const base = admin.baseURL
  const sql = (statement: string, ...args: string[]) => {
    const script = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); row=db.execute(sys.argv[2],sys.argv[3:]).fetchone(); print(row[0] if row else "")'
    return execFileSync('python3', ['-c', script, admin.commercePath, statement, ...args], { encoding: 'utf8' }).trim()
  }
  const complete = () => expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
  const confirmPreview = async (button: string) => {
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: button }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: button }).click()
    await complete()
  }
  const fill = async (fields: [string, string][]) => {
    for (const [label, value] of fields) await page.getByLabel(label, { exact: true }).fill(value)
  }

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
    await setClock(new Date(Math.floor(Date.now() / 1000) * 1000).toISOString())

    const subscriptions: string[] = []
    for (let index = 0; index < 2; index++) {
      const customerID = `conflict-migration-${index}-${randomUUID()}`
      await page.goto(`${base}/admin/quotes/new`)
      const customerField = page.getByRole('textbox', { name: /客戶 ID/ })
      await expect(customerField).toBeVisible()
      if (await customerField.isDisabled()) await page.getByRole('button', { name: '建立另一筆報價' }).click()
      await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
      await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
      await page.getByRole('textbox', { name: /席次/ }).fill('5')
      await page.getByRole('button', { name: '建立報價' }).click()
      await complete()
      const quoteID = sql('SELECT id FROM quotes WHERE customer_id=?', customerID)
      await page.goto(`${base}/admin/quotes/${quoteID}/accept`)
      await page.getByRole('button', { name: '預覽接受' }).click()
      await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
      await complete()
      const subscriptionID = sql('SELECT id FROM subscriptions WHERE quote_id=?', quoteID)
      subscriptions.push(subscriptionID)
      const operationID = sql('SELECT o.id FROM payment_operations o JOIN invoices i ON i.id=o.invoice_id WHERE i.subscription_id=?', subscriptionID)
      await page.goto(`${base}/admin/payments/${operationID}/dispatch`)
      await confirmPreview('確認送出付款')
    }
    subscriptions.sort()
    const [appliedID, conflictedID] = subscriptions
    const firstEnd = BigInt(sql('SELECT period_end FROM billing_periods WHERE subscription_id=? AND period_index=0', appliedID))
    expect(sql('SELECT period_end FROM billing_periods WHERE subscription_id=? AND period_index=0', conflictedID)).toBe(firstEnd.toString())
    const effective = new Date(Number(firstEnd / 1_000_000n)).toISOString()
    const priceID = `pro-conflict-${randomUUID()}`
    await page.goto(`${base}/admin/prices/pro/new`)
    await fill([
      ['價格版本 ID', priceID], ['版本號', '2'], ['固定金額（最小單位）', '6000'],
      ['每席金額（最小單位）', '1000'], ['包含任務量', '20000'],
      ['超額費率分子', '1'], ['超額費率分母', '10'], ['生效起點（UTC）', effective],
    ])
    await confirmPreview('確認發布價格')
    await page.goto(`${base}/admin/catalog-selections/new`)
    await fill([['方案 ID', 'pro'], ['Cohort', 'default'], ['生效時間（UTC）', effective], ['價格版本 ID', priceID]])
    await confirmPreview('確認選價')
    const migrationID = `conflict-batch-${randomUUID()}`
    await page.goto(`${base}/admin/price-migrations/new`)
    await fill([
      ['遷移批次 ID', migrationID], ['Cohort', 'default'], ['目標價格版本 ID', priceID],
      ['訂閱 ID（逗號或換行分隔）', subscriptions.join(',')],
    ])
    await confirmPreview('確認建立遷移批次')

    await page.goto(`${base}/admin/subscriptions/${conflictedID}/cancel`)
    const rejectedCancelPreview = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '預覽取消' }).click()
    expect((await rejectedCancelPreview).status()).toBe(409)
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    await expect(page.getByRole('button', { name: '確認排程取消' })).toHaveCount(0)
    expect(sql("SELECT COUNT(*) FROM admin_previews WHERE action_id='C05' AND target_id=?", conflictedID)).toBe('0')
    expect(sql('SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=?', conflictedID)).toBe('0')

    const conflictedCustomer = sql('SELECT customer_id FROM subscriptions WHERE id=?', conflictedID)
    const currentRevision = sql('SELECT revision FROM subscriptions WHERE id=?', conflictedID)
    await page.goto(`${base}/admin/quotes/new`)
    await page.getByRole('button', { name: '建立另一筆報價' }).click()
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(conflictedCustomer)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    await page.getByRole('checkbox', { name: '這是現有訂閱的變更報價' }).check()
    await page.getByRole('textbox', { name: /訂閱 ID/ }).fill(conflictedID)
    await page.getByRole('combobox', { name: /變更方式/ }).click()
    await page.getByText('下期變更', { exact: true }).click()
    await page.getByRole('textbox', { name: /目前 Revision/ }).fill(currentRevision)
    await page.getByRole('button', { name: '建立報價' }).click()
    await complete()
    await page.getByRole('button', { name: '前往排程下期變更' }).click()
    const rejectedPlanPreview = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '預覽下期變更' }).click()
    expect((await rejectedPlanPreview).status()).toBe(409)
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    await expect(page.getByText('變更預覽', { exact: true })).toHaveCount(0)
    expect(sql("SELECT COUNT(*) FROM admin_previews WHERE action_id='C03' AND target_id=?", conflictedID)).toBe('0')
    expect(sql('SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=?', conflictedID)).toBe('0')

    const expectedRevision = sql('SELECT expected_revision FROM price_migration_items WHERE migration_id=? AND subscription_id=?', migrationID, conflictedID)
    const changeRevision = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("UPDATE subscriptions SET revision=revision+1 WHERE id=?",(sys.argv[2],)); db.commit()'
    execFileSync('python3', ['-c', changeRevision, admin.commercePath, conflictedID])

    await setClock(new Date(Number((firstEnd + 1_000_000_000n) / 1_000_000n)).toISOString())
    await page.goto(`${base}/admin/jobs/renewals`)
    await confirmPreview('確認續約批次')
    expect(sql('SELECT status FROM price_migrations WHERE id=?', migrationID)).toBe('paused')
    expect(sql('SELECT status FROM price_migration_items WHERE migration_id=? AND subscription_id=?', migrationID, appliedID)).toBe('applied')
    expect(sql('SELECT conflict_reason FROM price_migration_items WHERE migration_id=? AND subscription_id=?', migrationID, conflictedID)).toBe('revision_changed')

    await page.goto(`${base}/admin/subscriptions/${conflictedID}/cancel`)
    const conflictedCancelPreview = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '預覽取消' }).click()
    expect((await conflictedCancelPreview).status()).toBe(409)
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    expect(sql('SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=?', conflictedID)).toBe('0')
    expect(sql('SELECT price_version_id FROM pricing_assignments WHERE subscription_id=? AND effective_end IS NULL', appliedID)).toBe(priceID)
    expect(sql('SELECT price_version_id FROM pricing_assignments WHERE subscription_id=? AND effective_end IS NULL', conflictedID)).toBe('pro-v1')

    await page.goto(`${base}/admin/price-migrations`)
    await page.getByRole('row').filter({ hasText: migrationID }).getByRole('button', { name: '詳情' }).click()
    await expect(page).toHaveURL(new RegExp(`/admin/price-migrations/${migrationID}$`))
    await expect(page.getByRole('heading', { name: '價格遷移批次' })).toBeVisible()
    await expect(page.getByText('1 筆訂閱與遷移預覽衝突')).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: conflictedID }).getByText('訂閱 revision 已變更')).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: conflictedID }).getByText(expectedRevision, { exact: true })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: appliedID }).getByText('applied', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '略過項目' }).click()
    await page.getByLabel('訂閱 ID', { exact: true }).fill(conflictedID)
    await page.getByLabel('略過理由', { exact: true }).fill('revision changed after preview')
    await confirmPreview('確認略過')
    expect(sql('SELECT status FROM price_migration_items WHERE migration_id=? AND subscription_id=?', migrationID, conflictedID)).toBe('skipped')
    expect(sql('SELECT status FROM price_migration_items WHERE migration_id=? AND subscription_id=?', migrationID, appliedID)).toBe('applied')
    expect(sql('SELECT i.total_minor FROM invoices i JOIN billing_periods p ON p.invoice_id=i.id WHERE p.subscription_id=? AND p.period_index=1', appliedID)).toBe('11000')
    await page.goto(`${base}/admin/jobs/renewals`)
    await page.getByRole('button', { name: '執行另一個操作' }).click()
    await confirmPreview('確認續約批次')
    expect(sql('SELECT i.total_minor FROM invoices i JOIN billing_periods p ON p.invoice_id=i.id WHERE p.subscription_id=? AND p.period_index=1', conflictedID)).toBe('10000')
    expect(sql('SELECT COUNT(*) FROM pricing_assignments WHERE subscription_id=?', appliedID)).toBe('2')
    expect(sql('SELECT COUNT(*) FROM pricing_assignments WHERE subscription_id=?', conflictedID)).toBe('1')
  } finally {
    await admin.stop()
  }
})

test('migration detail shows source price seat and schedule conflicts without renewal facts', async ({ page }) => {
  const admin = await startLocalAdmin()
  const base = admin.baseURL
  const sql = (statement: string, ...args: string[]) => {
    const script = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); row=db.execute(sys.argv[2],sys.argv[3:]).fetchone(); print(row[0] if row else "")'
    return execFileSync('python3', ['-c', script, admin.commercePath, statement, ...args], { encoding: 'utf8' }).trim()
  }
  const mutate = (statement: string, ...args: string[]) => {
    const script = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute(sys.argv[2],sys.argv[3:]); db.commit()'
    execFileSync('python3', ['-c', script, admin.commercePath, statement, ...args])
  }
  try {
    await page.goto(`${base}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(admin.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(admin.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
    const session = await (await page.request.get(`${base}/admin/api/session`)).json() as { csrf_token: string }
    async function setClock(valueUTC: string) {
      const response = await page.request.post(`${base}/admin/api/commands`, {
        headers: { Origin: base, 'X-CSRF-Token': session.csrf_token, 'Idempotency-Key': randomUUID() },
        data: { action_id: 'C46', target_id: '', payload: { mode: 'fixed', value_utc: valueUTC } },
      })
      expect(response.ok(), `${response.status()} ${await response.text()}`).toBe(true)
    }
    async function fill(fields: Array<[string, string]>) {
      for (const [label, value] of fields) await page.getByLabel(label, { exact: true }).fill(value)
    }
    async function confirmPreview(label: string) {
      await page.getByRole('button', { name: '建立預覽' }).click()
      await page.getByRole('button', { name: label }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: label }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    }
    await setClock(new Date(Math.floor(Date.now() / 1000) * 1000).toISOString())
    const cases = [
      { kind: 'source_price', reason: 'source_price_changed', label: '目前價格已不同於預覽來源' },
      { kind: 'seat_quantity', reason: 'seat_quantity_changed', label: '席位數已變更' },
      { kind: 'scheduled_change', reason: 'scheduled_change', label: '已有下期變更排程' },
    ]
    const subscriptions: string[] = []
    for (const testCase of cases) {
      const customerID = `migration-${testCase.kind}-${randomUUID()}`
      await page.goto(`${base}/admin/quotes/new`)
      const customerField = page.getByRole('textbox', { name: /客戶 ID/ })
      await expect(customerField).toBeVisible()
      if (await customerField.isDisabled()) await page.getByRole('button', { name: '建立另一筆報價' }).click()
      await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
      await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
      await page.getByRole('textbox', { name: /席次/ }).fill('5')
      await page.getByRole('button', { name: '建立報價' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      const quoteID = sql('SELECT id FROM quotes WHERE customer_id=? ORDER BY rowid DESC LIMIT 1', customerID)
      await page.goto(`${base}/admin/quotes/${quoteID}/accept`)
      await page.getByRole('button', { name: '預覽接受' }).click()
      await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      const subscriptionID = sql('SELECT id FROM subscriptions WHERE quote_id=?', quoteID)
      subscriptions.push(subscriptionID)
      const operationID = sql('SELECT o.id FROM payment_operations o JOIN invoices i ON i.id=o.invoice_id WHERE i.subscription_id=?', subscriptionID)
      await page.goto(`${base}/admin/payments/${operationID}/dispatch`)
      await confirmPreview('確認送出付款')
    }
    const boundary = BigInt(sql('SELECT period_end FROM billing_periods WHERE subscription_id=? AND period_index=0', subscriptions[0]))
    for (const subscriptionID of subscriptions.slice(1)) {
      expect(sql('SELECT period_end FROM billing_periods WHERE subscription_id=? AND period_index=0', subscriptionID)).toBe(boundary.toString())
    }
    const effective = new Date(Number(boundary / 1_000_000n)).toISOString()
    const priceID = `pro-matrix-${randomUUID()}`
    await page.goto(`${base}/admin/prices/pro/new`)
    await fill([
      ['價格版本 ID', priceID], ['版本號', '2'],
      ['固定金額（最小單位）', '6000'], ['每席金額（最小單位）', '1000'],
      ['包含任務量', '20000'], ['超額費率分子', '1'],
      ['超額費率分母', '10'], ['生效起點（UTC）', effective],
    ])
    await confirmPreview('確認發布價格')
    await page.goto(`${base}/admin/catalog-selections/new`)
    await fill([['方案 ID', 'pro'], ['Cohort', 'default'], ['生效時間（UTC）', effective], ['價格版本 ID', priceID]])
    await confirmPreview('確認選價')
    const migrationIDs: string[] = []
    for (const subscriptionID of subscriptions) {
      const migrationID = `migration-matrix-${randomUUID()}`
      migrationIDs.push(migrationID)
      await page.goto(`${base}/admin/price-migrations/new`)
      const startAnother = page.getByRole('button', { name: '執行另一個操作' })
      if (await startAnother.isVisible()) await startAnother.click()
      await fill([
        ['遷移批次 ID', migrationID], ['Cohort', 'default'],
        ['目標價格版本 ID', priceID], ['訂閱 ID（逗號或換行分隔）', subscriptionID],
      ])
      await confirmPreview('確認建立遷移批次')
      expect(sql('SELECT status FROM price_migration_items WHERE migration_id=?', migrationID)).toBe('pending')
    }
    mutate(`UPDATE subscriptions SET price_version_id='basic-v1' WHERE id=?`, subscriptions[0])
    mutate(`UPDATE subscriptions SET seat_quantity=6 WHERE id=?`, subscriptions[1])
    const scheduleID = `schedule-matrix-${randomUUID()}`
    mutate(`INSERT INTO subscription_schedules(id,subscription_id,kind,target_price_version_id,seat_quantity,effective_at,status,request_key,created_revision,created_at) VALUES(?,?,'change','basic-v1',0,?,'scheduled',?,1,?)`, scheduleID, subscriptions[2], boundary.toString(), scheduleID, boundary.toString())
    await setClock(new Date(Number((boundary + 1_000_000_000n) / 1_000_000n)).toISOString())
    await page.goto(`${base}/admin/jobs/renewals`)
    await confirmPreview('確認續約批次')
    for (const [index, testCase] of cases.entries()) {
      const subscriptionID = subscriptions[index]
      const migrationID = migrationIDs[index]
      expect(sql('SELECT status FROM price_migrations WHERE id=?', migrationID)).toBe('paused')
      expect(sql('SELECT conflict_reason FROM price_migration_items WHERE migration_id=?', migrationID)).toBe(testCase.reason)
      expect(sql('SELECT COUNT(*) FROM billing_periods WHERE subscription_id=?', subscriptionID)).toBe('1')
      expect(sql('SELECT COUNT(*) FROM invoices WHERE subscription_id=?', subscriptionID)).toBe('1')
      expect(sql('SELECT COUNT(*) FROM pricing_assignments WHERE subscription_id=?', subscriptionID)).toBe('1')
      await page.goto(`${base}/admin/price-migrations/${migrationID}`)
      await expect(page.getByRole('row').filter({ hasText: subscriptionID }).getByText(testCase.label)).toBeVisible()
    }
  } finally {
    await admin.stop()
  }
})
