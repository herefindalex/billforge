import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

const readFailure = {
  status: 503,
  contentType: 'application/json',
  body: JSON.stringify({ error: { code: 'QUERY_FAILED', message: '暫時無法讀取' } }),
}

test('命令列表、抽屜與詳情讀取失敗時保留上次資料並可恢復', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(`refresh-${randomUUID()}`)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    const submitted = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '建立報價' }).click()
    const command = await (await submitted).json() as { id: string; [key: string]: unknown }
    expect(command.id).toBeTruthy()

    await page.goto(`${app.baseURL}/admin/commands`)
    const row = page.getByRole('row').filter({ hasText: command.id })
    await expect(row).toBeVisible()
    await page.route('**/admin/api/commands?**', async (route) => {
      if (route.request().method() === 'GET') await route.fulfill(readFailure)
      else await route.continue()
    })
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('無法更新命令列表；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText(/上次讀取：/)).toBeVisible()
    await expect(row).toBeVisible()
    await page.unroute('**/admin/api/commands?**')
    await expect(page.getByText('無法更新命令列表；以下是上次成功讀取的資料')).toHaveCount(0, { timeout: 10_000 })

    await page.route(`**/admin/api/commands/${command.id}`, async (route) => route.fulfill(readFailure))
    await row.getByRole('button', { name: '詳情' }).click()
    const drawer = page.locator('.ant-drawer').filter({ hasText: '命令詳情' })
    await expect(drawer.getByText('無法更新命令詳情；以下是上次成功讀取的資料')).toBeVisible()
    await expect(drawer.getByText(/上次讀取：/)).toBeVisible()
    await expect(drawer.getByText(command.id).first()).toBeVisible()
    await page.unroute(`**/admin/api/commands/${command.id}`)
    await drawer.getByRole('button', { name: /重\s*試/ }).click()
    await expect(drawer.getByText('無法更新命令詳情；以下是上次成功讀取的資料')).toHaveCount(0)
    await page.route(`**/admin/api/commands/${command.id}`, async (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...command, status: 'waiting_verification', error_code: '' }) }))
    await drawer.getByRole('button', { name: /更\s*新/ }).click()
    await expect(drawer.getByRole('button', { name: '重新查證' })).toBeEnabled()
    await page.unroute(`**/admin/api/commands/${command.id}`)
    await page.route(`**/admin/api/commands/${command.id}`, async (route) => route.fulfill(readFailure))
    await drawer.getByRole('button', { name: /更\s*新/ }).click()
    await expect(drawer.getByText('無法更新命令詳情；以下是上次成功讀取的資料')).toBeVisible()
    await expect(drawer.getByText(/上次讀取：/)).toBeVisible()
    await expect(drawer.getByText(command.id).first()).toBeVisible()
    await expect(drawer.getByRole('button', { name: '重新查證' })).toBeDisabled()
    await page.unroute(`**/admin/api/commands/${command.id}`)
    await drawer.getByRole('button', { name: /重\s*試/ }).click()
    await expect(drawer.getByText('無法更新命令詳情；以下是上次成功讀取的資料')).toHaveCount(0)

    await page.goto(`${app.baseURL}/admin/commands/${command.id}`)
    await expect(page.getByRole('main').getByText(command.id).first()).toBeVisible()
    await page.route(`**/admin/api/commands/${command.id}`, async (route) => route.fulfill(readFailure))
    await page.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByText('無法更新命令狀態；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText(/上次讀取：/)).toBeVisible()
    await expect(page.getByRole('main').getByText(command.id).first()).toBeVisible()
    await page.unroute(`**/admin/api/commands/${command.id}`)
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByText('無法更新命令狀態；以下是上次成功讀取的資料')).toHaveCount(0)

    await page.route(`**/admin/api/commands/${command.id}`, async (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...command, status: 'accepted', error_code: '' }) }))
    await page.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByRole('button', { name: '繼續原命令' })).toBeEnabled()
    await page.unroute(`**/admin/api/commands/${command.id}`)
    await page.route(`**/admin/api/commands/${command.id}`, async (route) => route.fulfill(readFailure))
    await page.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByText('無法更新命令狀態；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByRole('button', { name: '繼續原命令' })).toBeDisabled()

    await page.unroute(`**/admin/api/commands/${command.id}`)
    await page.route(`**/admin/api/commands/${command.id}`, async (route) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...command, status: 'accepted', error_code: 'PERMISSION_REVOKED_REVIEW' }) }))
    await page.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByRole('button', { name: '檢查既有收據' })).toBeEnabled()
    await page.unroute(`**/admin/api/commands/${command.id}`)
    await page.route(`**/admin/api/commands/${command.id}`, async (route) => route.fulfill(readFailure))
    await page.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByRole('button', { name: '檢查既有收據' })).toBeDisabled()
  } finally {
    await app.stop()
  }
})
