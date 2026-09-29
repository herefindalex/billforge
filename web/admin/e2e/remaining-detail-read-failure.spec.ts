import { execFileSync } from 'node:child_process'
import { expect, test } from '@playwright/test'
import { startLocalAdmin, type LocalAdmin } from './server'

test.describe.serial('remaining financial detail read failures', () => {
  let app: LocalAdmin
  test.beforeAll(async () => { app = await startLocalAdmin({ seedDemo: true }) })
  test.afterAll(async () => { await app?.stop() })
  test.beforeEach(async ({ page }) => {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
  })

  test('customer detail marks stale data and disables quote entry until refreshed', async ({ page }) => {
    const customerID = execFileSync('python3', ['-c', 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); print(db.execute("SELECT customer_id FROM subscriptions LIMIT 1").fetchone()[0])', app.commercePath], { encoding: 'utf8' }).trim()
    let failure: number | null = null
    await page.route((url) => url.pathname === `/admin/api/customers/${encodeURIComponent(customerID)}`, async (route) => {
      if (failure === null) return route.continue()
      await route.fulfill({ status: failure, contentType: 'application/json', body: JSON.stringify({ error: { code: failure === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: '讀取失敗' } }) })
    })
    await page.goto(`${app.baseURL}/admin/customers/${encodeURIComponent(customerID)}`)
    await expect(page.getByText(customerID, { exact: true }).first()).toBeVisible()
    const customer = await (await page.request.get(`${app.baseURL}/admin/api/customers/${encodeURIComponent(customerID)}`)).json() as { Quotes: Array<{ ID: string }> }
    const quoteID = customer.Quotes[0].ID
    await expect(page.getByRole('button', { name: quoteID })).toBeEnabled()
    await expect(page.getByRole('button', { name: '為此客戶建立報價' })).toBeEnabled()
    failure = 503
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('無法更新客戶；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText(customerID, { exact: true }).first()).toBeVisible()
    await expect(page.getByRole('button', { name: quoteID })).toBeDisabled()
    await expect(page.getByRole('button', { name: '為此客戶建立報價' })).toBeDisabled()
    failure = null
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByRole('button', { name: quoteID })).toBeEnabled()
    await expect(page.getByRole('button', { name: '為此客戶建立報價' })).toBeEnabled()
    failure = 403
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('沒有權限查看客戶')).toBeVisible()
    await expect(page.getByText(customerID, { exact: true })).toHaveCount(0)
  })

  test('invoice history retains exact money and blocks stale pagination', async ({ page }) => {
    const invoiceID = 'invoice-history-read-probe'
    let failure: number | null = null
    await page.route((url) => url.pathname === `/admin/api/invoices/${invoiceID}/history/corrections`, async (route) => {
      if (failure !== null) {
        await route.fulfill({ status: failure, contentType: 'application/json', body: JSON.stringify({ error: { code: failure === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: '讀取失敗' } }) })
        return
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [{ ID: 'correction-read-probe', ReductionMinor: '500', OriginKind: 'admin_command', OriginID: 'cmd-read-probe', CreatedAt: '2026-09-28T00:00:00Z' }], currency: 'USD', next_cursor: 'next-page' }) })
    })
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/history/corrections`)
    await expect(page.getByText('correction-read-probe')).toBeVisible()
    await expect(page.getByText('USD 5.00')).toBeVisible()
    await expect(page.getByRole('button', { name: '下一頁' })).toBeEnabled()
    failure = 503
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('無法更新帳單歷史；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText('USD 5.00')).toBeVisible()
    await expect(page.getByRole('button', { name: '下一頁' })).toBeDisabled()
    failure = null
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByRole('button', { name: '下一頁' })).toBeEnabled()
    failure = 403
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('沒有權限查看帳單歷史')).toBeVisible()
    await expect(page.getByText('USD 5.00')).toHaveCount(0)
  })

  test('migration histories mark stale evidence and disable provenance resolution', async ({ page }) => {
    const id = 'legacy:history-read-probe'
    const encoded = encodeURIComponent(id)
    let shadowFailure: number | null = null
    let provenanceFailure: number | null = null
    await page.route((url) => url.pathname === `/admin/api/account-migrations/${encoded}/shadows`, async (route) => {
      if (shadowFailure !== null) return route.fulfill({ status: shadowFailure, contentType: 'application/json', body: JSON.stringify({ error: { code: shadowFailure === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: '讀取失敗' } }) })
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [{ ID: 'shadow-read-probe', Kind: 'quote', ObjectID: 'quote-1', Matched: false, LatencyMillis: '10', ObservedAt: '2026-09-28T00:00:00Z' }], next_cursor: 'next-page' }) })
    })
    await page.route((url) => url.pathname === `/admin/api/account-migrations/${encoded}/provenance`, async (route) => {
      if (provenanceFailure !== null) return route.fulfill({ status: provenanceFailure, contentType: 'application/json', body: JSON.stringify({ error: { code: provenanceFailure === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: '讀取失敗' } }) })
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [{ LegacyInvoiceID: 'legacy-invoice-1', LegacySubscriptionID: 'legacy-sub-1', CommerceSubscriptionID: 'sub-1', CommerceInvoiceID: 'invoice-1', PriceVersionID: 'pro-v1', Status: 'manual_review', Evidence: 'needs review' }], next_cursor: 'next-page' }) })
    })

    await page.goto(`${app.baseURL}/admin/account-migrations/${encoded}/shadow-history`)
    await expect(page.getByText('shadow-read-probe')).toBeVisible()
    await expect(page.getByRole('button', { name: '下一頁' })).toBeEnabled()
    shadowFailure = 503
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('無法更新 Shadow 歷史；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText('shadow-read-probe')).toBeVisible()
    await expect(page.getByRole('button', { name: '下一頁' })).toBeDisabled()
    shadowFailure = null
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByRole('button', { name: '下一頁' })).toBeEnabled()
    shadowFailure = 403
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('沒有權限查看 Shadow 歷史')).toBeVisible()
    await expect(page.getByText('shadow-read-probe')).toHaveCount(0)

    await page.goto(`${app.baseURL}/admin/account-migrations/${encoded}/provenance-history`)
    await expect(page.getByText('legacy-invoice-1')).toBeVisible()
    await expect(page.getByRole('button', { name: '處理來源' })).toBeEnabled()
    await expect(page.getByRole('button', { name: '下一頁' })).toBeEnabled()
    provenanceFailure = 503
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('無法更新來源歷史；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText('legacy-invoice-1')).toBeVisible()
    await expect(page.getByRole('button', { name: '處理來源' })).toBeDisabled()
    await expect(page.getByRole('button', { name: '下一頁' })).toBeDisabled()
    provenanceFailure = null
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByRole('button', { name: '處理來源' })).toBeEnabled()
    await expect(page.getByRole('button', { name: '下一頁' })).toBeEnabled()
    provenanceFailure = 403
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('沒有權限查看來源歷史')).toBeVisible()
    await expect(page.getByText('legacy-invoice-1')).toHaveCount(0)
  })
})
