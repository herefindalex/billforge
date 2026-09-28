import { execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { startLocalAdmin } from './server'

test('usage adjustment, close and rerate reject previews after their sources change', async ({ page, context }) => {
  const app = await startLocalAdmin()
  const count = (query: string, ...values: string[]) => Number(execFileSync('python3', [
    '-c',
    'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); print(db.execute(sys.argv[2],sys.argv[3:]).fetchone()[0])',
    app.commercePath, query, ...values,
  ], { encoding: 'utf8' }).trim())
  const scalar = (query: string, ...values: string[]) => execFileSync('python3', [
    '-c',
    'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); print(db.execute(sys.argv[2],sys.argv[3:]).fetchone()[0])',
    app.commercePath, query, ...values,
  ], { encoding: 'utf8' }).trim()
  const adjustmentReceipts = () => count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C27' AND c.target_id=''")
  const record = async (activePage: Page, subscriptionID: string, eventID: string, quantity: string, occurredAt: string) => {
    await activePage.goto(`${app.baseURL}/admin/usage-events/new`)
    const subscriptionField = activePage.getByLabel('訂閱 ID', { exact: true })
    await expect(subscriptionField).toBeVisible()
    if (await subscriptionField.isDisabled()) await activePage.getByRole('button', { name: '記錄另一筆事件' }).click()
    for (const [label, value] of [
      ['訂閱 ID', subscriptionID], ['Meter ID', 'tasks'], ['來源', 'browser'],
      ['事件 ID', eventID], ['發生時間（UTC）', occurredAt], ['數量', quantity],
    ]) await activePage.getByLabel(label, { exact: true }).fill(value)
    await activePage.getByRole('button', { name: '檢查並記錄' }).click()
    await activePage.getByRole('dialog').getByRole('button', { name: '確認記錄' }).click()
    await expect(activePage.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
  }

  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const customerID = `stale-close-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: '客戶 ID' }).fill(customerID)
    await page.getByRole('textbox', { name: '方案 ID' }).fill('pro')
    await page.getByRole('textbox', { name: '席次' }).fill('5')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=?', customerID)
    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await page.getByRole('button', { name: '預覽接受' }).click()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const subscriptionID = scalar('SELECT id FROM subscriptions WHERE quote_id=?', quoteID)
    const periodEnd = BigInt(scalar('SELECT period_end FROM billing_periods WHERE subscription_id=? AND period_index=0', subscriptionID))
    const originalEventID = `original-${randomUUID()}`
    await record(page, subscriptionID, originalEventID, '20000', new Date().toISOString())

    const cutoff = new Date(Number((periodEnd + 1_000_000_000n) / 1_000_000n)).toISOString()
    await page.goto(`${app.baseURL}/admin/lab/clock`)
    await page.getByRole('combobox', { name: /模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('固定時間', { exact: true }).click()
    await page.getByRole('textbox', { name: /固定 UTC 時間/ }).fill(cutoff)
    await page.getByRole('button', { name: '確認設定時鐘' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await page.goto(`${app.baseURL}/admin/usage-periods/${subscriptionID}/close`)
    await page.getByLabel('帳期序號', { exact: true }).fill('0')
    await page.getByLabel('接收截止時間（UTC）', { exact: true }).fill(cutoff)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽')).toBeVisible()
    const other = await context.newPage()
    const lateAt = new Date(Number((periodEnd - 60n * 60n * 1_000_000_000n) / 1_000_000n)).toISOString()
    await record(other, subscriptionID, `concurrent-${randomUUID()}`, '10', lateAt)
    await other.close()

    const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認關帳' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認關帳' }).click()
    const rejected = await staleResponse
    expect(rejected.status()).toBe(409)
    expect((await rejected.json()).error.code).toBe('PREVIEW_STALE')
    await expect(page.getByText('原預覽已失效，請檢查新預覽並再次確認')).toBeVisible()
    await expect(page.getByText('原先：20000')).toBeVisible()
    await expect(page.getByText('現在：20010')).toBeVisible()
    expect(count('SELECT COUNT(*) FROM usage_ratings WHERE subscription_id=? AND period_index=0', subscriptionID)).toBe(0)
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C28' AND c.target_id=?", subscriptionID)).toBe(0)

    await page.getByRole('button', { name: '確認關帳' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認關帳' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(scalar('SELECT quantity FROM usage_ratings WHERE subscription_id=? AND period_index=0 AND revision=1', subscriptionID)).toBe('20010')
    expect(scalar('SELECT rounded_minor FROM usage_ratings WHERE subscription_id=? AND period_index=0 AND revision=1', subscriptionID)).toBe('1')
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C28' AND c.target_id=?", subscriptionID)).toBe(1)

    const later = new Date(Number((periodEnd + 24n * 60n * 60n * 1_000_000_000n) / 1_000_000n)).toISOString()
    await page.goto(`${app.baseURL}/admin/lab/clock`)
    await page.getByRole('button', { name: '執行另一個操作' }).click()
    await page.getByRole('combobox', { name: /模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('固定時間', { exact: true }).click()
    await page.getByRole('textbox', { name: /固定 UTC 時間/ }).fill(later)
    await page.getByRole('button', { name: '確認設定時鐘' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await record(page, subscriptionID, `first-late-${randomUUID()}`, '10', lateAt)
    await page.goto(`${app.baseURL}/admin/usage-periods/${subscriptionID}/rerate`)
    await page.getByLabel('帳期序號', { exact: true }).fill('0')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽')).toBeVisible()
    const second = await context.newPage()
    await record(second, subscriptionID, `second-late-${randomUUID()}`, '10', lateAt)
    await second.close()

    const staleRerateResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認重算' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認重算' }).click()
    const rejectedRerate = await staleRerateResponse
    expect(rejectedRerate.status()).toBe(409)
    expect((await rejectedRerate.json()).error.code).toBe('PREVIEW_STALE')
    await expect(page.getByText('原預覽已失效，請檢查新預覽並再次確認')).toBeVisible()
    await expect(page.getByText('原先：20020')).toBeVisible()
    await expect(page.getByText('現在：20030')).toBeVisible()
    expect(count('SELECT COUNT(*) FROM usage_ratings WHERE subscription_id=? AND period_index=0', subscriptionID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C29' AND c.target_id=?", subscriptionID)).toBe(0)

    await page.getByRole('button', { name: '確認重算' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認重算' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(scalar('SELECT quantity FROM usage_ratings WHERE subscription_id=? AND period_index=0 AND revision=2', subscriptionID)).toBe('20030')
    expect(scalar('SELECT rounded_minor FROM usage_ratings WHERE subscription_id=? AND period_index=0 AND revision=2', subscriptionID)).toBe('3')
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C29' AND c.target_id=?", subscriptionID)).toBe(1)

    const smallEventID = `adjust-original-${randomUUID()}`
    await record(page, subscriptionID, smallEventID, '10', lateAt)
    const staleAdjustmentID = `stale-adjustment-${randomUUID()}`
    const adjustmentFields = (eventID: string, quantity: string) => [
      ['新事件來源', 'browser'], ['新事件 ID', eventID], ['訂閱 ID', subscriptionID],
      ['原事件來源', 'browser'], ['原事件 ID', smallEventID], ['反向數量', quantity],
    ]
    await page.goto(`${app.baseURL}/admin/usage-adjustments/new`)
    for (const [label, value] of adjustmentFields(staleAdjustmentID, '7')) {
      await page.getByLabel(label, { exact: true }).fill(value)
    }
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽')).toBeVisible()
    const concurrentAdjustmentID = `concurrent-adjustment-${randomUUID()}`
    const adjustmentPage = await context.newPage()
    await adjustmentPage.goto(`${app.baseURL}/admin/usage-adjustments/new`)
    for (const [label, value] of adjustmentFields(concurrentAdjustmentID, '4')) {
      await adjustmentPage.getByLabel(label, { exact: true }).fill(value)
    }
    await adjustmentPage.getByRole('button', { name: '建立預覽' }).click()
    await adjustmentPage.getByRole('button', { name: '確認反向調整' }).last().click()
    await adjustmentPage.getByRole('dialog').getByRole('button', { name: '確認反向調整' }).click()
    await expect(adjustmentPage.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await adjustmentPage.close()

    const staleAdjustmentResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認反向調整' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認反向調整' }).click()
    const rejectedAdjustment = await staleAdjustmentResponse
    expect(rejectedAdjustment.status()).toBe(409)
    expect((await rejectedAdjustment.json()).error.code).toBe('PREVIEW_STALE')
    await expect(page.getByText('原預覽已失效', { exact: true })).toBeVisible()
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    expect(count('SELECT COUNT(*) FROM usage_events WHERE event_id=?', staleAdjustmentID)).toBe(0)
    expect(adjustmentReceipts()).toBe(1)

    const freshAdjustmentID = `fresh-adjustment-${randomUUID()}`
    await page.getByLabel('新事件 ID', { exact: true }).fill(freshAdjustmentID)
    await page.getByLabel('反向數量', { exact: true }).fill('6')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認反向調整' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認反向調整' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(scalar('SELECT quantity FROM usage_events WHERE event_id=?', concurrentAdjustmentID)).toBe('-4')
    expect(scalar('SELECT quantity FROM usage_events WHERE event_id=?', freshAdjustmentID)).toBe('-6')
    expect(count('SELECT COUNT(*) FROM usage_events WHERE event_id=?', staleAdjustmentID)).toBe(0)
    expect(adjustmentReceipts()).toBe(2)
  } finally {
    await app.stop()
  }
})
