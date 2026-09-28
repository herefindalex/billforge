import { execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { startLocalAdmin } from './server'

test('historical provenance cannot be overwritten by an older backfill or review preview', async ({ page, context }) => {
  const app = await startLocalAdmin()
  const scalar = (query: string, ...values: string[]) => execFileSync('python3', [
    '-c',
    'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); row=db.execute(sys.argv[2],sys.argv[3:]).fetchone(); print(row[0] if row else "")',
    app.commercePath, query, ...values,
  ], { encoding: 'utf8' }).trim()
  const count = (query: string, ...values: string[]) => Number(scalar(query, ...values))
  const fillBackfill = async (activePage: Page, legacyID: string, legacyInvoiceID: string, subscriptionID: string, invoiceID: string, priceID: string) => {
    await activePage.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/provenance`)
    for (const [label, value] of [
      ['舊帳單 ID', legacyInvoiceID], ['舊訂閱 ID', 'legacy-subscription'],
      ['Commerce 訂閱 ID', subscriptionID], ['Commerce 帳單 ID', invoiceID],
      ['價格版本 ID', priceID],
    ]) await activePage.getByLabel(label, { exact: true }).fill(value)
    await activePage.getByRole('button', { name: '建立預覽' }).click()
    await expect(activePage.getByText('操作預覽')).toBeVisible()
  }
  const fillReview = async (activePage: Page, legacyID: string, legacyInvoiceID: string, subscriptionID: string, invoiceID: string, decision: string) => {
    await activePage.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/provenance/${encodeURIComponent(legacyInvoiceID)}/resolve`)
    for (const [label, value] of [
      ['Commerce 訂閱 ID', subscriptionID], ['Commerce 帳單 ID', invoiceID],
      ['價格版本 ID', 'basic-v1'], ['審核決議', decision],
    ]) await activePage.getByLabel(label, { exact: true }).fill(value)
    await activePage.getByRole('button', { name: '建立預覽' }).click()
    await expect(activePage.getByText('操作預覽')).toBeVisible()
  }

  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const customerID = `provenance-customer-${randomUUID()}`
    const legacyID = `legacy:provenance-${randomUUID()}`
    const legacyInvoiceID = `legacy-invoice-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: '客戶 ID' }).fill(customerID)
    await page.getByRole('textbox', { name: '方案 ID' }).fill('basic')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=?', customerID)
    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await page.getByRole('button', { name: '預覽接受' }).click()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const subscriptionID = scalar('SELECT id FROM subscriptions WHERE quote_id=?', quoteID)
    const invoiceID = scalar('SELECT id FROM invoices WHERE subscription_id=?', subscriptionID)

    await page.goto(`${app.baseURL}/admin/account-migrations/new`)
    for (const [label, value] of [
      ['既有帳戶 ID', legacyID], ['Commerce 客戶 ID', customerID],
      ['受益人 ID', customerID], ['價格 Cohort', 'default'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('combobox', { name: '是否有歷史資料' }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('有', { exact: true }).click()
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認連結帳戶' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認連結帳戶' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await fillBackfill(page, legacyID, legacyInvoiceID, subscriptionID, invoiceID, 'basic-v1')
    const other = await context.newPage()
    await fillBackfill(other, legacyID, legacyInvoiceID, subscriptionID, invoiceID, 'pro-v1')
    await other.getByRole('button', { name: '確認回填' }).last().click()
    await other.getByRole('dialog').getByRole('button', { name: '確認回填' }).click()
    await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(scalar('SELECT status FROM legacy_provenance WHERE legacy_invoice_id=?', legacyInvoiceID)).toBe('manual_review')

    const staleBackfillResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認回填' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認回填' }).click()
    const rejectedBackfill = await staleBackfillResponse
    expect(rejectedBackfill.status()).toBe(409)
    expect((await rejectedBackfill.json()).error.code).toBe('PREVIEW_STALE')
    await expect(page.getByText('原預覽已失效', { exact: true })).toBeVisible()
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    expect(scalar('SELECT price_version_id FROM legacy_provenance WHERE legacy_invoice_id=?', legacyInvoiceID)).toBe('pro-v1')
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C39' AND c.target_id=?", legacyID)).toBe(1)

    await fillReview(page, legacyID, legacyInvoiceID, subscriptionID, invoiceID, 'verify_original')
    await fillReview(other, legacyID, legacyInvoiceID, subscriptionID, invoiceID, 'verify_provider')
    await other.getByRole('button', { name: '確認來源映射' }).last().click()
    await other.getByRole('dialog').getByRole('button', { name: '確認來源映射' }).click()
    await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await other.close()

    const staleReviewResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認來源映射' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認來源映射' }).click()
    const rejectedReview = await staleReviewResponse
    expect(rejectedReview.status()).toBe(409)
    expect((await rejectedReview.json()).error.code).toBe('PREVIEW_STALE')
    await expect(page.getByText('原預覽已失效', { exact: true })).toBeVisible()
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    expect(scalar('SELECT status FROM legacy_provenance WHERE legacy_invoice_id=?', legacyInvoiceID)).toBe('complete')
    expect(scalar('SELECT price_version_id FROM legacy_provenance WHERE legacy_invoice_id=?', legacyInvoiceID)).toBe('basic-v1')
    expect(count("SELECT COUNT(*) FROM account_migration_events WHERE legacy_account_id=? AND kind='provenance_resolved'", legacyID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C40' AND c.target_id=?", legacyInvoiceID)).toBe(1)
  } finally {
    await app.stop()
  }
})
