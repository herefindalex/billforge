import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('stale resource data disables state-changing links until a successful refresh', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    let fail = false
    await page.route('**/admin/api/subscriptions?**', async (route) => {
      if (fail) {
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'QUERY_FAILED', message: '暫時無法讀取' } }) })
        return
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
        items: [{ ID: 'sub-stale-ui', Status: 'active', CustomerID: 'customer-ui' }],
        total: 1,
        next_cursor: '',
        observed_at: '2026-09-29T00:00:00Z',
      }) })
    })
    await page.goto(`${app.baseURL}/admin/subscriptions`)
    const row = page.getByRole('row').filter({ hasText: 'sub-stale-ui' })
    const cancel = row.getByRole('button', { name: '管理取消' })
    const detail = row.getByRole('button', { name: '詳情' })
    await expect(cancel).toBeEnabled()
    await expect(detail).toBeEnabled()

    fail = true
    await page.getByRole('button', { name: '更新資料' }).click()
    await expect(page.getByText('資料更新失敗，顯示上次讀取結果')).toBeVisible()
    await expect(cancel).toBeDisabled()
    await expect(detail).toBeEnabled()

    fail = false
    await page.getByRole('button', { name: '更新資料' }).click()
    await expect(page.getByText('資料更新失敗，顯示上次讀取結果')).toHaveCount(0)
    await expect(cancel).toBeEnabled()
  } finally {
    await app.stop()
  }
})
