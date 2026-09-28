import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('資源抽屜跟隨最新查詢並標示失敗時的觀測時間', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    let status = 'created'
    let observedAt = '2026-09-28T00:00:00Z'
    let fail = false
    let present = true
    await page.route('**/admin/api/payments?**', async (route) => {
      if (fail) {
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'QUERY_FAILED', message: '暫時無法讀取' } }) })
        return
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
        items: present ? [{ ID: 'payment-drawer-ui', InvoiceID: 'invoice-ui', Status: status, AmountMinor: '1000', Currency: 'USD' }] : [],
        total: present ? 1 : 0,
        next_cursor: '',
        observed_at: observedAt,
      }) })
    })

    await page.goto(`${app.baseURL}/admin/payments`)
    await page.getByRole('row').filter({ hasText: 'payment-drawer-ui' }).getByRole('button', { name: '詳情' }).click()
    const drawer = page.locator('.ant-drawer').filter({ hasText: '資料詳情' })
    await expect(drawer.getByText('created', { exact: true })).toBeVisible()

    status = 'definitively_failed'
    observedAt = '2026-09-28T00:01:00Z'
    await drawer.getByRole('button', { name: /更\s*新/ }).click()
    await expect(drawer.getByText('definitively_failed', { exact: true })).toBeVisible()
    await expect(drawer.getByText(/觀測：/)).toBeVisible()

    fail = true
    await drawer.getByRole('button', { name: /更\s*新/ }).click()
    await expect(drawer.getByText('資料更新失敗，以下是上次讀取的結果')).toBeVisible()
    await expect(drawer.getByText('definitively_failed', { exact: true })).toBeVisible()

    fail = false
    status = 'succeeded'
    observedAt = '2026-09-28T00:02:00Z'
    await drawer.getByRole('button', { name: /更\s*新/ }).click()
    await expect(drawer.getByText('資料更新失敗，以下是上次讀取的結果')).toHaveCount(0)
    await expect(drawer.getByText('succeeded', { exact: true })).toBeVisible()

    present = false
    await drawer.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByRole('dialog', { name: '資料詳情' })).toHaveCount(0)
  } finally {
    await app.stop()
  }
})
