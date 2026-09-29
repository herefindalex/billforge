import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('價格遷移暫停頁讀取失敗時不允許依舊狀態發出命令', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const id = `migration-refresh-${randomUUID()}`
    let failRead = true
    await page.route(`**/admin/api/price-migrations/${id}`, async (route) => {
      if (failRead) {
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'QUERY_FAILED', message: '暫時無法讀取' } }) })
      } else {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ID: id, Cohort: 'default', TargetPriceVersionID: 'price-v2', Status: 'active', ItemCount: '0', PendingCount: '0', AppliedCount: '0', ConflictedCount: '0', SkippedCount: '0' }) })
      }
    })

    await page.goto(`${app.baseURL}/admin/price-migrations/${id}/pause`)
    await expect(page.getByText('遷移批次無法載入')).toBeVisible()
    failRead = false
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByRole('button', { name: '暫停未完成項目' })).toBeEnabled()
    await expect(page.getByText(id)).toBeVisible()

    failRead = true
    await page.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByText('無法更新遷移批次；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText(/上次讀取：/)).toBeVisible()
    await expect(page.getByText(id)).toBeVisible()
    await expect(page.getByRole('button', { name: '暫停未完成項目' })).toBeDisabled()

    failRead = false
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByText('無法更新遷移批次；以下是上次成功讀取的資料')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '暫停未完成項目' })).toBeEnabled()
  } finally {
    await app.stop()
  }
})
