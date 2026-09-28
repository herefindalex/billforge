import { execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { startLocalAdmin } from './server'

test('manual decision shows changed discrepancy and requires another confirmation', async ({ page, context }) => {
  const app = await startLocalAdmin()
  const scalar = (query: string, ...values: string[]) => execFileSync('python3', [
    '-c',
    'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); row=db.execute(sys.argv[2],sys.argv[3:]).fetchone(); print(row[0] if row else "")',
    app.commercePath, query, ...values,
  ], { encoding: 'utf8' }).trim()
  const fillDecision = async (activePage: Page, discrepancyID: string, decision: string, reason: string) => {
    await activePage.goto(`${app.baseURL}/admin/discrepancies/${encodeURIComponent(discrepancyID)}/manual-decisions`)
    await activePage.getByLabel('決議', { exact: true }).fill(decision)
    await activePage.getByLabel('原因', { exact: true }).fill(reason)
    await activePage.getByRole('button', { name: '建立預覽' }).click()
    await expect(activePage.getByText('操作預覽')).toBeVisible()
  }

  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const providerKey = `capture:manual-race-${randomUUID()}`
    execFileSync('python3', [
      '-c',
      'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES(?,100,\'USD\',\'succeeded\')",(sys.argv[2],)); db.commit()',
      app.providerPath, providerKey,
    ])
    await page.goto(`${app.baseURL}/admin/reconciliation-runs/new`)
    await page.getByLabel('核對截止時間（UTC）').fill(new Date().toISOString())
    await page.getByRole('button', { name: '確認執行對帳' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認執行對帳' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const discrepancyID = scalar('SELECT id FROM discrepancies WHERE object_id=?', providerKey)
    expect(discrepancyID).not.toBe('')

    await fillDecision(page, discrepancyID, 'investigate_source', 'Check provider evidence')
    const other = await context.newPage()
    await fillDecision(other, discrepancyID, 'investigate_provider', 'Provider confirms this capture')
    await other.getByRole('button', { name: '確認記錄決議' }).last().click()
    await other.getByRole('dialog').getByRole('button', { name: '確認記錄決議' }).click()
    await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await other.close()

    const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認記錄決議' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認記錄決議' }).click()
    const rejected = await staleResponse
    expect(rejected.status()).toBe(409)
    expect((await rejected.json()).error.code).toBe('PREVIEW_STALE')
    await expect(page.getByText('原預覽已失效，請檢查新預覽並再次確認')).toBeVisible()
    await expect(page.getByText('open → investigating')).toBeVisible()
    expect(scalar('SELECT COUNT(*) FROM manual_decisions WHERE discrepancy_id=?', discrepancyID)).toBe('1')
    expect(scalar("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C35' AND c.target_id=?", discrepancyID)).toBe('1')

    await page.getByRole('button', { name: '確認記錄決議' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認記錄決議' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(scalar('SELECT COUNT(*) FROM manual_decisions WHERE discrepancy_id=?', discrepancyID)).toBe('2')
    expect(scalar("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C35' AND c.target_id=?", discrepancyID)).toBe('2')
  } finally {
    await app.stop()
  }
})
