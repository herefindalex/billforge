import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('付款專用頁顯示預覽阻擋原因，確認框到期後不提交舊金額', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    let previews = 0
    let submissions = 0
    await page.route('**/admin/api/previews', async (route) => {
      if (route.request().method() !== 'POST') return route.continue()
      previews += 1
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          preview_id: `payment-preview-${previews}`, action_id: 'C07', target_id: 'invoice-ui',
          expires_at: previews === 2 ? new Date(Date.now() + 2500).toISOString() : '2099-01-01T00:00:00Z',
          source_versions: { invoice: '1' },
          impact: { amount_minor: '500', outstanding_before_minor: '1000', currency: 'USD' },
          blocking_reasons: previews === 1 ? ['原收款操作仍待查證'] : [],
        }),
      })
    })
    await page.route('**/admin/api/commands', async (route) => {
      if (route.request().method() !== 'POST') return route.continue()
      submissions += 1
      await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: { code: 'UNEXPECTED_SUBMIT' } }) })
    })

    await page.goto(`${app.baseURL}/admin/invoices/invoice-ui/payments/new`)
    await page.getByRole('textbox', { name: '付款金額（最小貨幣單位）' }).fill('500')
    await page.getByRole('button', { name: '預覽付款' }).click()
    await expect(page.getByText('原收款操作仍待查證')).toBeVisible()
    await expect(page.getByRole('button', { name: '確認建立付款' })).toBeDisabled()
    expect(submissions).toBe(0)

    await page.getByRole('button', { name: '預覽付款' }).click()
    await expect(page.getByText('原收款操作仍待查證')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '確認建立付款' })).toBeEnabled()
    await page.getByRole('button', { name: '確認建立付款' }).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await expect(page.getByText('預覽已過期，請重新建立預覽')).toBeVisible()
    await page.getByRole('dialog').getByRole('button', { name: '確認建立' }).click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '確認建立付款' })).toBeDisabled()
    expect(submissions).toBe(0)

    await page.getByRole('button', { name: '預覽付款' }).click()
    await expect(page.getByText('預覽已過期，請重新建立預覽')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '確認建立付款' })).toBeEnabled()
    expect(previews).toBe(3)
  } finally {
    await app.stop()
  }
})
