import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('訂閱更新故障標示舊狀態並停用變更，權限收回後隱藏內容', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const id = `subscription-read-${randomUUID()}`
    let failureStatus: number | null = null
    await page.route(`**/admin/api/subscriptions/${id}/**`, async (route) => {
      const entitlement = route.request().url().endsWith('/entitlement')
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(entitlement
        ? { entitlement: null, observed_at: '2026-09-28T00:00:00Z' }
        : { items: [], next_cursor: '', observed_at: '2026-09-28T00:00:00Z' }) })
    })
    await page.route(`**/admin/api/subscriptions/${id}`, async (route) => {
      if (failureStatus !== null) {
        await route.fulfill({ status: failureStatus, contentType: 'application/json', body: JSON.stringify({ error: { code: failureStatus === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: failureStatus === 403 ? '沒有權限' : '暫時無法讀取' } }) })
        return
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
        ID: id, CustomerID: 'customer-1', PriceVersionID: 'price-v1', SeatQuantity: '2', Revision: '1',
        Status: 'active', EntitlementStatus: 'active', EntitlementReason: '', EntitlementSourceRevision: '1',
        QuoteID: 'quote-1', ContractVersionID: '', PricePlanID: 'pro', Currency: 'USD',
        ActualFixedMinor: '1000', ActualSeatMinor: '500', CurrentPeriod: null, ScheduledChange: null,
        ScheduledCancel: null, HoldReason: '',
      }) })
    })

    await page.goto(`${app.baseURL}/admin/subscriptions/${id}`)
    await expect(page.getByText('實際價格版本')).toBeVisible()
    await expect(page.getByRole('button', { name: '立即升級' })).toBeEnabled()

    failureStatus = 503
    await page.getByRole('button', { name: '更新訂閱' }).click()
    await expect(page.getByText('無法更新訂閱；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText('實際價格版本')).toBeVisible()
    await expect(page.getByRole('button', { name: '立即升級' })).toBeDisabled()
    await expect(page.getByRole('button', { name: '排程下期變更' })).toBeDisabled()

    failureStatus = null
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByText('無法更新訂閱；以下是上次成功讀取的資料')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '立即升級' })).toBeEnabled()

    failureStatus = 403
    await page.getByRole('button', { name: '更新訂閱' }).click()
    await expect(page.getByText('沒有權限查看訂閱')).toBeVisible()
    await expect(page.getByText('實際價格版本')).toHaveCount(0)
  } finally {
    await app.stop()
  }
})
