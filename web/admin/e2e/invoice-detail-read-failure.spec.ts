import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('帳單更新故障保留已標示的舊資料，權限收回則隱藏財務內容', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const id = `invoice-read-${randomUUID()}`
    let failureStatus: number | null = null
    await page.route(`**/admin/api/invoices/${id}`, async (route) => {
      if (failureStatus !== null) {
        await route.fulfill({ status: failureStatus, contentType: 'application/json', body: JSON.stringify({ error: { code: failureStatus === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: failureStatus === 403 ? '沒有權限' : '暫時無法讀取' } }) })
        return
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
        observed_at: '2026-09-28T00:00:00Z',
        invoice: {
          ID: id, SubscriptionID: 'sub-1', FinalizedAt: '2026-09-28T00:00:00Z', Period: null,
          Balance: {
            Currency: 'USD', OriginalMinor: '1000', ReductionsMinor: '0', ObligationMinor: '1000',
            GrossCapturedMinor: '0', ReleasedMinor: '0', CreditAppliedMinor: '0', NetAppliedMinor: '0', OutstandingMinor: '1000',
          },
          Lines: [], Payments: [{ ID: 'payment-1', AmountMinor: '1000', Currency: 'USD', Status: 'created' }], PaymentsTruncated: false,
          Corrections: [], CorrectionsTruncated: false, CreditApplications: [], CreditApplicationsTruncated: false,
          CreditGrants: [{ ID: 'credit-1', CorrectionID: 'correction-1', ReleaseID: 'release-1', SourceOperationID: 'payment-1', AmountMinor: '1000', CreatedAt: '2026-09-28T00:00:00Z' }], CreditGrantsTruncated: false,
          Refunds: [], RefundsTruncated: false,
        },
      }) })
    })

    await page.goto(`${app.baseURL}/admin/invoices/${id}`)
    await expect(page.getByText('尚待支付')).toBeVisible()
    await expect(page.getByRole('button', { name: '建立付款操作' })).toBeEnabled()

    failureStatus = 503
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('無法更新帳單；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText('尚待支付')).toBeVisible()
    await expect(page.getByRole('button', { name: '建立付款操作' })).toBeDisabled()
    await expect(page.getByRole('button', { name: '預留退款' })).toBeDisabled()
    await expect(page.getByRole('button', { name: '送出' })).toBeDisabled()

    failureStatus = null
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByText('無法更新帳單；以下是上次成功讀取的資料')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '建立付款操作' })).toBeEnabled()
    await expect(page.getByRole('button', { name: '送出' })).toBeEnabled()

    failureStatus = 403
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('沒有權限查看帳單')).toBeVisible()
    await expect(page.getByText('尚待支付')).toHaveCount(0)
  } finally {
    await app.stop()
  }
})
