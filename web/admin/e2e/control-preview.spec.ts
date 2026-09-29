import { execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
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

test('changed provider decision rejects an old preview before admitting a second command', async ({ page }) => {
  const app = await startLocalAdmin()
  const scalar = (statement: string, arg: string) => execFileSync('python3', [
    '-c',
    'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); row=db.execute(sys.argv[2],(sys.argv[3],)).fetchone(); print(row[0] if row else "")',
    app.commercePath,
    statement,
    arg,
  ], { encoding: 'utf8' }).trim()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const customerID = `control-decision-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    await page.getByRole('textbox', { name: /席次/ }).fill('0')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=? ORDER BY rowid DESC LIMIT 1', customerID)
    expect(quoteID).toBeTruthy()

    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await page.getByRole('button', { name: '預覽接受' }).click()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const operationID = scalar('SELECT p.id FROM payment_operations p JOIN invoices i ON p.invoice_id=i.id JOIN subscriptions s ON i.subscription_id=s.id WHERE s.quote_id=?', quoteID)
    expect(operationID).toBeTruthy()

    await page.goto(`${app.baseURL}/admin/lab/payment-decisions/${operationID}`)
    await page.getByRole('combobox', { name: /結果/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('成功', { exact: true }).click()
    await page.getByRole('button', { name: '確認付款結果' }).click()
    await expect(page.getByRole('dialog')).toBeVisible()

    const session = await (await page.request.get(`${app.baseURL}/admin/api/session`)).json() as { csrf_token: string }
    const headers = { Origin: app.baseURL, 'X-CSRF-Token': session.csrf_token }
    const payload = { status: 'definitively_failed' }
    const previewResponse = await page.request.post(`${app.baseURL}/admin/api/previews`, {
      headers,
      data: { action_id: 'C47', target_id: operationID, payload },
    })
    expect(previewResponse.status()).toBe(200)
    const competingPreview = (await previewResponse.json()) as { preview_id: string }
    const competingCommand = await page.request.post(`${app.baseURL}/admin/api/commands`, {
      headers: { ...headers, 'Idempotency-Key': randomUUID() },
      data: { action_id: 'C47', target_id: operationID, payload, preview_id: competingPreview.preview_id },
    })
    expect(competingCommand.status()).toBe(202)
    expect(((await competingCommand.json()) as { status: string }).status).toBe('succeeded')

    const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('dialog').getByRole('button', { name: '確認付款結果' }).click()
    const rejected = await staleResponse
    expect(rejected.status()).toBe(409)
    expect((await rejected.json()).error.code).toBe('PREVIEW_STALE')
    await expect(page.getByText('原預覽已失效')).toBeVisible()
    await expect(page.getByText('原預覽（已失效）')).toBeVisible()
    await expect(page.getByText('proposed_decision', { exact: true })).toBeVisible()
    expect(scalar("SELECT COUNT(*) FROM admin_commands WHERE action_id='C47' AND target_id=?", operationID)).toBe('1')
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
