import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('published price detail shows exact components and its catalog selection scope', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    await page.goto(`${app.baseURL}/admin/catalog/prices?id_prefix=pro-v1`)
    await page.getByRole('button', { name: '開啟版本' }).click()
    await expect(page).toHaveURL(`${app.baseURL}/admin/catalog/prices/pro-v1`)
    await expect(page.getByRole('heading', { name: '價格版本詳情' })).toBeVisible()
    await expect(page.getByText('published', { exact: true })).toBeVisible()
    await expect(page.getByText('USD 50.00')).toBeVisible()
    await expect(page.getByText('1 / 10')).toBeVisible()
    await expect(page.getByText('20000', { exact: true })).toBeVisible()
    await expect(page.getByRole('row', { name: /選價紀錄數/ }).getByText('1', { exact: true })).toBeVisible()
    await page.reload()
    await expect(page.getByRole('heading', { name: '價格版本詳情' })).toBeVisible()

    await page.setViewportSize({ width: 390, height: 844 })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)

    await page.getByRole('button', { name: '查看此版本選價' }).click()
    await expect(page).toHaveURL(`${app.baseURL}/admin/catalog-selections?price_version_id=pro-v1`)
    await expect(page.getByRole('row').filter({ hasText: 'pro-v1' })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: 'basic-v1' })).toHaveCount(0)
  } finally {
    await app.stop()
  }
})
