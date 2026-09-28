import { execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('pending fault tickets remain visible across cursor pages', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    const prefix = `fault-page-${randomUUID()}`
    const seed = `
import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
prefix = sys.argv[2]
for i in range(21):
    identity = f'{prefix}-{i:02d}'
    db.execute('INSERT INTO admin_fault_tickets(id,operation_kind,operation_id,mode,created_at) VALUES(?,?,?,?,?)',
               (identity, 'payment', identity, 'lost_response', f'2026-01-01T00:00:{i:02d}Z'))
db.commit()
`
    execFileSync('python3', ['-c', seed, app.commercePath, prefix])

    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
    await page.goto(`${app.baseURL}/admin/lab/controls`)
    await expect(page.getByText(`${prefix}-20`, { exact: true })).toBeVisible()
    await expect(page.getByText(`${prefix}-00`, { exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: '下一頁故障票據' }).click()
    await expect(page.getByText(`${prefix}-00`, { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '下一頁故障票據' })).toBeDisabled()
    await page.getByRole('button', { name: '上一頁故障票據' }).click()
    await expect(page.getByText(`${prefix}-20`, { exact: true })).toBeVisible()
    await expect(page.getByText(`${prefix}-00`, { exact: true })).toHaveCount(0)
  } finally {
    await app.stop()
  }
})
