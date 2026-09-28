import { execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { startLocalAdmin } from './server'

test('writer cutover rejects an older preview when shadow readiness is lost', async ({ page, context }) => {
  const app = await startLocalAdmin()
  const scalar = (query: string, ...values: string[]) => execFileSync('python3', [
    '-c',
    'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); row=db.execute(sys.argv[2],sys.argv[3:]).fetchone(); print(row[0] if row else "")',
    app.commercePath, query, ...values,
  ], { encoding: 'utf8' }).trim()
  const recordShadowQuote = async (activePage: Page, legacyID: string, amount: string) => {
    await activePage.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/shadow-quotes`)
    for (const [label, value] of [
      ['方案 ID', 'basic'], ['席位數', '0'], ['既有報價金額', amount], ['幣別', 'USD'],
    ]) await activePage.getByLabel(label, { exact: true }).fill(value)
    await activePage.getByRole('button', { name: '確認記錄比對' }).click()
    await activePage.getByRole('dialog').getByRole('button', { name: '確認記錄比對' }).click()
    await expect(activePage.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
  }
  const fillCutover = async (activePage: Page, legacyID: string, action: 'switch-read' | 'switch-writer') => {
    await activePage.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/${action}`)
    for (const [label, value] of [
      ['報價 P95 上限（毫秒）', '5000'], ['未知付款上限', '0'], ['未結對帳差異上限', '0'],
    ]) await activePage.getByLabel(label, { exact: true }).fill(value)
    await activePage.getByRole('button', { name: '建立預覽' }).click()
    await expect(activePage.getByText('操作預覽')).toBeVisible()
  }

  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const customerID = `cutover-customer-${randomUUID()}`
    const legacyID = `legacy:cutover-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/account-migrations/new`)
    for (const [label, value] of [
      ['既有帳戶 ID', legacyID], ['Commerce 客戶 ID', customerID],
      ['受益人 ID', customerID], ['價格 Cohort', 'default'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('combobox', { name: '是否有歷史資料' }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('沒有', { exact: true }).click()
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認連結帳戶' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認連結帳戶' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await recordShadowQuote(page, legacyID, '2000')
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/shadow-entitlements`)
    await page.getByLabel('訂閱 ID', { exact: true }).fill('future-subscription')
    await page.getByLabel('既有權益狀態', { exact: true }).fill('missing')
    await page.getByRole('button', { name: '確認記錄比對' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認記錄比對' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/reconciliation-runs/new`)
    await page.getByLabel('核對截止時間（UTC）').fill(new Date().toISOString())
    await page.getByRole('button', { name: '確認執行對帳' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認執行對帳' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await fillCutover(page, legacyID, 'switch-read')
    await page.getByRole('button', { name: '確認切換讀取' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認切換讀取' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await fillCutover(page, legacyID, 'switch-writer')
    const other = await context.newPage()
    await recordShadowQuote(other, legacyID, '2500')
    await other.close()
    expect(scalar('SELECT COUNT(*) FROM migration_shadows WHERE legacy_account_id=? AND kind=\'quote\' AND matched=0', legacyID)).toBe('1')

    const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認切換寫入' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認切換寫入' }).click()
    const rejected = await staleResponse
    expect(rejected.status()).toBe(409)
    expect((await rejected.json()).error.code).toBe('PREVIEW_STALE')
    await expect(page.getByText('原預覽已失效', { exact: true })).toBeVisible()
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    expect(scalar('SELECT read_owner FROM account_links WHERE legacy_account_id=?', legacyID)).toBe('commerce')
    expect(scalar('SELECT writer_owner FROM account_links WHERE legacy_account_id=?', legacyID)).toBe('legacy')
    expect(scalar("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C42' AND c.target_id=?", legacyID)).toBe('0')
    expect(scalar("SELECT COUNT(*) FROM account_migration_events WHERE legacy_account_id=? AND kind='writer_cutover'", legacyID)).toBe('0')
  } finally {
    await app.stop()
  }
})
