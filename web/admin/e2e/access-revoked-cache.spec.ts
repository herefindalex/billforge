import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

async function login(page: import('@playwright/test').Page, app: Awaited<ReturnType<typeof startLocalAdmin>>) {
  await page.goto(`${app.baseURL}/admin/login`)
  await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
  await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
  await page.getByRole('button', { name: '登 入' }).click()
  await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
}

test('revoked resource read hides cached rows and open drawer until access returns', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await login(page, app)
    let forbidden = false
    await page.route('**/admin/api/quotes?**', async (route) => {
      await route.fulfill(forbidden
        ? { status: 403, contentType: 'application/json', body: JSON.stringify({ error: { code: 'FORBIDDEN', message: '權限已撤回' } }) }
        : { status: 200, contentType: 'application/json', body: JSON.stringify({ items: [{ ID: 'quote-private-ui', CustomerID: 'customer-private-ui', Accepted: true }], total: 1, next_cursor: '', observed_at: '2026-09-29T00:00:00Z' }) })
    })
    await page.goto(`${app.baseURL}/admin/quotes`)
    const row = page.getByRole('row').filter({ hasText: 'quote-private-ui' })
    await row.getByRole('button', { name: '詳情' }).click()
    await expect(page.getByRole('dialog', { name: '資料詳情' })).toBeVisible()

    forbidden = true
    await page.getByRole('dialog', { name: '資料詳情' }).getByRole('button', { name: '更 新' }).click()
    await expect(page.getByText('資料載入失敗')).toBeVisible()
    await expect(row).toHaveCount(0)
    await expect(page.getByRole('dialog', { name: '資料詳情' })).toHaveCount(0)

    forbidden = false
    await page.getByRole('button', { name: '重 試' }).click()
    await expect(row).toBeVisible()
  } finally {
    await app.stop()
  }
})

test('revoked overview read hides cached counts until access returns', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    let forbidden = false
    await page.route('**/admin/api/overview', async (route) => {
      await route.fulfill(forbidden
        ? { status: 403, contentType: 'application/json', body: JSON.stringify({ error: { code: 'FORBIDDEN', message: '權限已撤回' } }) }
        : { status: 200, contentType: 'application/json', body: JSON.stringify({ observed_at: '2026-09-29T00:00:00Z', counts: { quotes: 777 } }) })
    })
    await login(page, app)
    await expect(page.getByText('777')).toBeVisible()

    forbidden = true
    await page.getByRole('button', { name: '更新資料' }).click()
    await expect(page.getByText('概覽載入失敗')).toBeVisible()
    await expect(page.getByText('777')).toHaveCount(0)

    forbidden = false
    await page.getByRole('button', { name: '重 試' }).click()
    await expect(page.getByText('777')).toBeVisible()
  } finally {
    await app.stop()
  }
})
