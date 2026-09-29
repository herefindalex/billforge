import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

async function login(page: import('@playwright/test').Page, app: Awaited<ReturnType<typeof startLocalAdmin>>) {
  await page.goto(`${app.baseURL}/admin/login`)
  await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
  await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
  await page.getByRole('button', { name: '登 入' }).click()
  await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
}

test('價格版本快取在權限收回後不再顯示', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await login(page, app)
    await page.goto(`${app.baseURL}/admin/catalog/prices/pro-v1`)
    await expect(page.getByText('版本與生效範圍')).toBeVisible()
    let failureStatus = 503
    await page.route('**/admin/api/prices/pro-v1', async (route) => route.fulfill({ status: failureStatus, contentType: 'application/json', body: JSON.stringify({ error: { code: failureStatus === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: '暫時無法讀取' } }) }))
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('無法更新價格版本，以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText('版本與生效範圍')).toBeVisible()
    failureStatus = 403
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('沒有權限查看價格版本')).toBeVisible()
    await expect(page.getByText('版本與生效範圍')).toHaveCount(0)
  } finally {
    await app.stop()
  }
})

test('合約快取故障時停用新報價，權限收回後隱藏條款', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await login(page, app)
    const id = `contract-read-${randomUUID()}`
    let failureStatus: number | null = null
    await page.route(`**/admin/api/contracts/${id}`, async (route) => {
      if (failureStatus !== null) {
        await route.fulfill({ status: failureStatus, contentType: 'application/json', body: JSON.stringify({ error: { code: failureStatus === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: '暫時無法讀取' } }) })
        return
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
        contract: {
          ID: id, CustomerID: 'customer-1', Version: '1', BasePriceVersionID: 'price-v1', Currency: 'USD',
          FixedMinor: '1000', SeatMinor: '100', PaymentDays: '30', EffectiveFrom: '2026-09-28T00:00:00Z',
          EffectiveTo: '2027-09-28T00:00:00Z', PostContractPriceVersionID: 'price-v2', Checksum: 'fixture',
          PublishedAt: '2026-09-28T00:00:00Z', QuoteCount: '0', SubscriptionCount: '0',
          Quotes: [], QuotesTruncated: false, Subscriptions: [], SubscriptionsTruncated: false,
        },
        observed_at: '2026-09-28T00:00:00Z',
      }) })
    })

    await page.goto(`${app.baseURL}/admin/contracts/${id}`)
    await expect(page.getByText('已發布條款')).toBeVisible()
    await expect(page.getByRole('button', { name: '建立合約報價' })).toBeEnabled()
    failureStatus = 503
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('無法更新合約資料，以下是上次成功讀取的結果')).toBeVisible()
    await expect(page.getByRole('button', { name: '建立合約報價' })).toBeDisabled()
    failureStatus = 403
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('沒有權限查看合約版本')).toBeVisible()
    await expect(page.getByText('已發布條款')).toHaveCount(0)
  } finally {
    await app.stop()
  }
})
