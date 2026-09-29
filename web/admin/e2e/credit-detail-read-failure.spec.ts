import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('Credit 更新故障保留舊額度但停用支出，權限收回後隱藏內容', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const id = `credit-read-${randomUUID()}`
    let failureStatus: number | null = null
    await page.route(`**/admin/api/credits/${id}`, async (route) => {
      if (failureStatus !== null) {
        await route.fulfill({ status: failureStatus, contentType: 'application/json', body: JSON.stringify({ error: { code: failureStatus === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: failureStatus === 403 ? '沒有權限' : '暫時無法讀取' } }) })
        return
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
        credit: {
          ID: id, ReleaseID: 'release-1', SourceCorrectionID: 'correction-1', SourceOperationID: 'payment-1',
          SourceInvoiceID: 'invoice-1', CreatedAt: '2026-09-28T00:00:00Z',
          Balance: { GrantID: id, Currency: 'USD', GrantedMinor: '1000', AppliedMinor: '0', ReservedMinor: '0', RefundedMinor: '0', AvailableMinor: '1000' },
        },
        observed_at: '2026-09-28T00:00:00Z',
      }) })
    })

    await page.goto(`${app.baseURL}/admin/credits/${id}`)
    await expect(page.getByText('可用額度')).toBeVisible()
    await expect(page.getByRole('button', { name: '抵扣帳單' })).toBeEnabled()

    failureStatus = 503
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('無法更新 Credit；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText('可用額度')).toBeVisible()
    await expect(page.getByRole('button', { name: '抵扣帳單' })).toBeDisabled()
    await expect(page.getByRole('button', { name: '預留退款' })).toBeDisabled()

    failureStatus = null
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByText('無法更新 Credit；以下是上次成功讀取的資料')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '抵扣帳單' })).toBeEnabled()

    failureStatus = 403
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('沒有權限查看 Credit')).toBeVisible()
    await expect(page.getByText('可用額度')).toHaveCount(0)
  } finally {
    await app.stop()
  }
})
