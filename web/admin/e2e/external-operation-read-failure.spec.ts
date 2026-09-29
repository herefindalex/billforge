import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

for (const kind of ['payment', 'refund'] as const) {
  test(`${kind} 讀取故障保留明確標示的舊資料，權限收回則隱藏內容`, async ({ page }) => {
    const app = await startLocalAdmin()
    try {
      await page.goto(`${app.baseURL}/admin/login`)
      await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
      await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
      await page.getByRole('button', { name: '登 入' }).click()
      await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

      const plural = kind === 'payment' ? 'payments' : 'refunds'
      const label = kind === 'payment' ? '付款' : '退款'
      const id = `${kind}-read-${randomUUID()}`
      let failureStatus: number | null = null
      await page.route(`**/admin/api/${plural}/${id}`, async (route) => {
        if (failureStatus !== null) {
          await route.fulfill({ status: failureStatus, contentType: 'application/json', body: JSON.stringify({ error: { code: failureStatus === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: failureStatus === 403 ? '沒有權限' : '暫時無法讀取' } }) })
          return
        }
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
          operation: { id, status: 'created', outbox_status: 'pending', amount_minor: '1000', currency: 'USD', source_id: kind === 'payment' ? 'invoice-1' : 'credit-1' },
          observed_at: '2026-09-28T00:00:00Z',
        }) })
      })

      await page.goto(`${app.baseURL}/admin/${plural}/${id}`)
      await expect(page.getByText('操作與來源')).toBeVisible()
      await expect(page.getByRole('button', { name: `送出${label}` })).toBeEnabled()

      failureStatus = 503
      await page.getByRole('button', { name: '重新整理' }).click()
      await expect(page.getByText('無法更新操作狀態，以下是上次成功讀取的資料')).toBeVisible()
      await expect(page.getByText('操作與來源')).toBeVisible()
      await expect(page.getByRole('button', { name: `送出${label}` })).toBeDisabled()
      await expect(page.getByRole('button', { name: '查證原操作' })).toBeDisabled()

      failureStatus = null
      await page.getByRole('button', { name: /重\s*試/ }).click()
      await expect(page.getByText('無法更新操作狀態，以下是上次成功讀取的資料')).toHaveCount(0)
      await expect(page.getByRole('button', { name: `送出${label}` })).toBeEnabled()

      failureStatus = 403
      await page.getByRole('button', { name: '重新整理' }).click()
      await expect(page.getByText(`沒有權限查看${label}操作詳情`)).toBeVisible()
      await expect(page.getByText('操作與來源')).toHaveCount(0)
    } finally {
      await app.stop()
    }
  })
}
