import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('預覽列出不可執行原因並禁止提交，原因解除後可確認', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    let blocked = true
    let submissions = 0
    await page.route('**/admin/api/previews', async (route) => {
      if (route.request().method() !== 'POST') return route.continue()
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          preview_id: blocked ? 'preview-blocked' : 'preview-ready',
          action_id: 'C16', target_id: 'refund-ui', expires_at: '2099-01-01T00:00:00Z',
          source_versions: { refund: '1' }, impact: { currency: 'USD', amount_minor: '500' },
          blocking_reasons: blocked ? ['原付款仍待查證', '退款額度不足'] : [],
        }),
      })
    })
    await page.route('**/admin/api/commands', async (route) => {
      if (route.request().method() !== 'POST') return route.continue()
      submissions += 1
      await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: { code: 'UNEXPECTED_SUBMIT' } }) })
    })

    await page.goto(`${app.baseURL}/admin/refunds/refund-ui/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('目前無法確認此操作')).toBeVisible()
    await expect(page.getByRole('listitem').filter({ hasText: '原付款仍待查證' })).toBeVisible()
    await expect(page.getByRole('listitem').filter({ hasText: '退款額度不足' })).toBeVisible()
    await expect(page.getByRole('button', { name: '確認送出退款' })).toBeDisabled()
    expect(submissions).toBe(0)

    blocked = false
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('目前無法確認此操作')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '確認送出退款' })).toBeEnabled()
    expect(submissions).toBe(0)
  } finally {
    await app.stop()
  }
})

test('預覽在畫面停留期間到期會停用確認並可重新建立', async ({ page }) => {
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
          preview_id: `preview-${previews}`, action_id: 'C16', target_id: 'refund-ui',
          expires_at: previews === 1 ? new Date(Date.now() + 2500).toISOString() : '2099-01-01T00:00:00Z',
          source_versions: { refund: '1' }, impact: { currency: 'USD', amount_minor: '500' }, blocking_reasons: [],
        }),
      })
    })
    await page.route('**/admin/api/commands', async (route) => {
      if (route.request().method() !== 'POST') return route.continue()
      submissions += 1
      await route.fulfill({ status: 500, contentType: 'application/json', body: JSON.stringify({ error: { code: 'UNEXPECTED_SUBMIT' } }) })
    })

    await page.goto(`${app.baseURL}/admin/refunds/refund-ui/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByRole('button', { name: '確認送出退款' })).toBeEnabled()
    await page.getByRole('button', { name: '確認送出退款' }).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await expect(page.getByText('預覽已過期，請重新建立預覽')).toBeVisible()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出退款' }).click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '確認送出退款' })).toBeDisabled()
    expect(submissions).toBe(0)

    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('預覽已過期，請重新建立預覽')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '確認送出退款' })).toBeEnabled()
    expect(previews).toBe(2)
    expect(submissions).toBe(0)
  } finally {
    await app.stop()
  }
})
