import { expect, test, type Page } from '@playwright/test'
import { startLocalAdmin } from './server'

async function login(page: Page, app: Awaited<ReturnType<typeof startLocalAdmin>>) {
  await page.goto(`${app.baseURL}/admin/login`)
  await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
  await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
  await page.getByRole('button', { name: '登 入' }).click()
  await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
  return (await (await page.request.get(`${app.baseURL}/admin/api/session`)).json()) as { actor_id: string }
}

test('共用操作頁只在原命令確定不存在時允許清除本頁記錄', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    const session = await login(page, app)
    const commandID = 'missing-refund-command-ui'
    const key = `billforge:admin:command:${session.actor_id}:C16:refund-action-ui`
    expect((await page.request.get(`${app.baseURL}/admin/api/commands/${commandID}`)).status()).toBe(404)
    await page.evaluate(([storageKey, id]) => sessionStorage.setItem(storageKey, id), [key, commandID])

    await page.goto(`${app.baseURL}/admin/refunds/refund-action-ui/dispatch`)
    await expect(page.getByText('命令狀態無法載入')).toBeVisible()
    await expect(page.getByRole('button', { name: '建立預覽' })).toBeDisabled()
    await page.getByRole('button', { name: '清除本頁無效命令記錄' }).click()
    await expect(page.getByRole('button', { name: '建立預覽' })).toBeEnabled()
    expect(await page.evaluate((storageKey) => sessionStorage.getItem(storageKey), key)).toBeNull()
    await page.reload()
    await expect(page.getByRole('button', { name: '建立預覽' })).toBeEnabled()
  } finally {
    await app.stop()
  }
})

test('建立報價頁可清除 404 命令，暫時讀取失敗仍保留原命令', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    const session = await login(page, app)
    const key = `billforge:admin:command:${session.actor_id}:C01:`
    const missingID = 'missing-quote-command-ui'
    expect((await page.request.get(`${app.baseURL}/admin/api/commands/${missingID}`)).status()).toBe(404)
    await page.evaluate(([storageKey, id]) => sessionStorage.setItem(storageKey, id), [key, missingID])
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await expect(page.getByRole('button', { name: '清除本頁無效命令記錄' })).toBeVisible()
    await expect(page.getByRole('textbox', { name: '客戶 ID' })).toBeDisabled()
    await page.getByRole('button', { name: '清除本頁無效命令記錄' }).click()
    await expect(page.getByRole('textbox', { name: '客戶 ID' })).toBeEnabled()

    const unavailableID = 'unavailable-quote-command-ui'
    await page.route(`**/admin/api/commands/${unavailableID}`, (route) => route.fulfill({
      status: 503,
      contentType: 'application/json',
      body: JSON.stringify({ error: { code: 'QUERY_FAILED', message: '暫時無法讀取' } }),
    }))
    await page.evaluate(([storageKey, id]) => sessionStorage.setItem(storageKey, id), [key, unavailableID])
    await page.reload()
    await expect(page.getByText('命令狀態無法載入')).toBeVisible()
    await expect(page.getByRole('button', { name: '清除本頁無效命令記錄' })).toHaveCount(0)
    await expect(page.getByRole('textbox', { name: '客戶 ID' })).toBeDisabled()
    expect(await page.evaluate((storageKey) => sessionStorage.getItem(storageKey), key)).toBe(unavailableID)
  } finally {
    await app.stop()
  }
})

test('曾讀到的命令隨後回 404 時仍可清除，但舊狀態不能啟動新操作', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    const session = await login(page, app)
    const commandID = 'cached-refund-command-ui'
    const key = `billforge:admin:command:${session.actor_id}:C16:refund-action-ui`
    await page.evaluate(([storageKey, id]) => sessionStorage.setItem(storageKey, id), [key, commandID])
    let missing = false
    await page.route(`**/admin/api/commands/${commandID}`, (route) => route.fulfill(missing ? {
      status: 404,
      contentType: 'application/json',
      body: JSON.stringify({ error: { code: 'NOT_FOUND', message: '找不到命令' } }),
    } : {
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        id: commandID,
        actor_id: session.actor_id,
        action_id: 'C16',
        target_id: 'refund-action-ui',
        status: 'succeeded',
        result_refs: {},
        created_at: '2026-09-28T00:00:00Z',
        updated_at: '2026-09-28T00:00:00Z',
      }),
    }))
    await page.goto(`${app.baseURL}/admin/refunds/refund-action-ui/dispatch`)
    await expect(page.getByRole('button', { name: '執行另一個操作' })).toBeEnabled()
    missing = true
    await page.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByRole('button', { name: '清除本頁無效命令記錄' })).toBeVisible()
    await expect(page.getByRole('button', { name: '執行另一個操作' })).toHaveCount(0)
    await page.getByRole('button', { name: '清除本頁無效命令記錄' }).click()
    await expect(page.getByRole('button', { name: '建立預覽' })).toBeEnabled()
    expect(await page.evaluate((storageKey) => sessionStorage.getItem(storageKey), key)).toBeNull()
  } finally {
    await app.stop()
  }
})
