import { expect, test } from '@playwright/test'
import { submitClockControl } from './control-commands'
import { startLocalAdmin } from './server'

test('lab clock confirmation shows a server preview and submits its preview ID', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    let previewPosts = 0
    let submittedPreviewID = ''
    page.on('request', (request) => {
      if (request.method() !== 'POST') return
      if (request.url().endsWith('/admin/api/previews')) previewPosts += 1
      if (request.url().endsWith('/admin/api/commands')) {
        const body = request.postDataJSON() as { action_id?: string; preview_id?: string }
        if (body.action_id === 'C46') submittedPreviewID = body.preview_id ?? ''
      }
    })
    await page.goto(`${app.baseURL}/admin/lab/clock`)
    await page.getByRole('combobox', { name: /模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('固定時間', { exact: true }).click()
    await page.getByRole('textbox', { name: /固定 UTC 時間/ }).fill('2026-10-01T12:00:00Z')
    await page.getByRole('button', { name: '確認設定時鐘' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible()
    await expect(dialog.getByText(/proposed_mode：fixed/)).toBeVisible()
    expect(previewPosts).toBe(1)
    expect(submittedPreviewID).toBe('')
    await dialog.getByRole('button', { name: '確認設定時鐘' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(submittedPreviewID).toMatch(/^prev_/)
    const clock = await (await page.request.get(`${app.baseURL}/admin/api/lab/clock`)).json() as { mode: string; value_utc: string }
    expect(clock.mode).toBe('fixed')
    expect(clock.value_utc).toBe('2026-10-01T12:00:00Z')
  } finally {
    await app.stop()
  }
})

test('stale lab clock preview shows source change before reconfirmation', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const session = await (await page.request.get(`${app.baseURL}/admin/api/session`)).json() as { csrf_token: string }
    await page.goto(`${app.baseURL}/admin/lab/clock`)
    await page.getByRole('combobox', { name: /模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('固定時間', { exact: true }).click()
    await page.getByRole('textbox', { name: /固定 UTC 時間/ }).fill('2026-10-01T12:00:00Z')
    await page.getByRole('button', { name: '確認設定時鐘' }).click()
    await expect(page.getByRole('dialog')).toBeVisible()

    await submitClockControl(page, app.baseURL, session.csrf_token, 'fixed', '2026-10-02T12:00:00Z')
    const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
    expect((await staleResponse).status()).toBe(409)
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(page.getByText('原預覽已失效，請檢查新預覽並再次確認')).toBeVisible()
    await expect(page.getByText('來源 revision')).toBeVisible()
    const unchanged = await (await page.request.get(`${app.baseURL}/admin/api/lab/clock`)).json() as { value_utc: string }
    expect(unchanged.value_utc).toBe('2026-10-02T12:00:00Z')

    await page.getByRole('button', { name: '確認設定時鐘' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const finalClock = await (await page.request.get(`${app.baseURL}/admin/api/lab/clock`)).json() as { value_utc: string }
    expect(finalClock.value_utc).toBe('2026-10-01T12:00:00Z')
  } finally {
    await app.stop()
  }
})
