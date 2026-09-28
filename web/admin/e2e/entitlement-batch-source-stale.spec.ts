import { execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { expect, test, type Page } from '@playwright/test'
import { startLocalAdmin } from './server'

test('entitlement batch reports changed member separately while refreshing the other', async ({ page, context }) => {
  const app = await startLocalAdmin()
  const scalar = (query: string, ...values: string[]) => execFileSync('python3', [
    '-c',
    'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); row=db.execute(sys.argv[2],sys.argv[3:]).fetchone(); print(row[0] if row else "")',
    app.commercePath, query, ...values,
  ], { encoding: 'utf8' }).trim()
  const createPaid = async (activePage: Page, customerID: string) => {
    await activePage.goto(`${app.baseURL}/admin/quotes/new`)
    const customerField = activePage.getByRole('textbox', { name: '客戶 ID' })
    await expect(customerField).toBeVisible()
    if (await customerField.isDisabled()) await activePage.getByRole('button', { name: '建立另一筆報價' }).click()
    await activePage.getByRole('textbox', { name: '客戶 ID' }).fill(customerID)
    await activePage.getByRole('textbox', { name: '方案 ID' }).fill('basic')
    await activePage.getByRole('textbox', { name: '席次' }).fill('0')
    await activePage.getByRole('button', { name: '建立報價' }).click()
    await expect(activePage.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=?', customerID)
    await activePage.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await activePage.getByRole('button', { name: '預覽接受' }).click()
    await activePage.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await activePage.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(activePage.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const subscriptionID = scalar('SELECT id FROM subscriptions WHERE quote_id=?', quoteID)
    const operationID = scalar('SELECT o.id FROM payment_operations o JOIN invoices i ON i.id=o.invoice_id WHERE i.subscription_id=?', subscriptionID)
    await activePage.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
    await activePage.getByRole('button', { name: '建立預覽' }).click()
    await activePage.getByRole('button', { name: '確認送出付款' }).last().click()
    await activePage.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(activePage.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await activePage.goto(`${app.baseURL}/admin/payments/${operationID}/reconcile`)
    await activePage.getByRole('button', { name: '確認查證' }).click()
    await activePage.getByRole('dialog').getByRole('button', { name: '確認查證' }).click()
    await expect(activePage.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    return subscriptionID
  }

  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const changedID = await createPaid(page, `batch-changed-${randomUUID()}`)
    const stableID = await createPaid(page, `batch-stable-${randomUUID()}`)
    execFileSync('python3', [
      '-c',
      'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("DELETE FROM entitlements WHERE subscription_id IN (?,?)",sys.argv[2:]); db.commit()',
      app.commercePath, changedID, stableID,
    ])
    await page.goto(`${app.baseURL}/admin/jobs/entitlement-refresh`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽')).toBeVisible()

    const other = await context.newPage()
    await other.goto(`${app.baseURL}/admin/subscriptions/${changedID}/cancel`)
    await other.getByRole('button', { name: '預覽取消' }).click()
    await other.getByRole('button', { name: '確認排程取消' }).click()
    await other.getByRole('dialog').getByRole('button', { name: '確認排程' }).click()
    await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await other.close()

    await page.getByRole('button', { name: '確認刷新權益' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認刷新權益' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const commandID = scalar("SELECT id FROM admin_commands WHERE action_id='C45' ORDER BY rowid DESC LIMIT 1")
    await page.goto(`${app.baseURL}/admin/jobs/${encodeURIComponent(`job:${commandID}`)}`)
    await expect(page.getByRole('row').filter({ hasText: changedID }).getByText('conflicted', { exact: true })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: stableID }).getByText('succeeded', { exact: true })).toBeVisible()
    expect(scalar('SELECT error_code FROM admin_job_items WHERE job_id=? AND target_id=?', `job:${commandID}`, changedID)).toBe('SOURCE_CHANGED')
    expect(scalar('SELECT COUNT(*) FROM entitlements WHERE subscription_id=?', changedID)).toBe('0')
    expect(scalar("SELECT COUNT(*) FROM entitlements WHERE subscription_id=? AND status='active'", stableID)).toBe('1')
    expect(scalar('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe('1')
  } finally {
    await app.stop()
  }
})
