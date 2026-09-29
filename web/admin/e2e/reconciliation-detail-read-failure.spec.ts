import { execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('reconciliation details retain marked evidence on transient read failure and hide it on denial', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const providerKey = `capture:orphan-read-${randomUUID()}`
    execFileSync('python3', ['-c', `import sqlite3,sys
db=sqlite3.connect(sys.argv[1])
db.executemany("INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES(?,100,'USD','succeeded')",[(sys.argv[2] if i == 0 else f'{sys.argv[2]}-{i}',) for i in range(21)])
db.commit()`, app.providerPath, providerKey])

    await page.goto(`${app.baseURL}/admin/reconciliation-runs/new`)
    await page.getByLabel('核對截止時間（UTC）').fill(new Date().toISOString())
    await page.getByRole('button', { name: '確認執行對帳' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認執行對帳' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const discrepancyID = execFileSync('python3', ['-c', 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); print(db.execute("SELECT id FROM discrepancies WHERE object_id=?",(sys.argv[2],)).fetchone()[0])', app.commercePath, providerKey], { encoding: 'utf8' }).trim()
    const detailResponse = await page.request.get(`${app.baseURL}/admin/api/discrepancies/${encodeURIComponent(discrepancyID)}`)
    expect(detailResponse.status()).toBe(200)
    const detail = await detailResponse.json() as { discrepancy: { Runs: Array<{ ID: string }> } }
    const runID = detail.discrepancy.Runs[0].ID

    let runFailure: number | null = null
    await page.route('**/admin/api/reconciliation-runs/**', async (route) => {
      if (runFailure === null) return route.continue()
      await route.fulfill({ status: runFailure, contentType: 'application/json', body: JSON.stringify({ error: { code: runFailure === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: '讀取失敗' } }) })
    })
    await page.goto(`${app.baseURL}/admin/reconciliation-runs/${encodeURIComponent(runID)}`)
    await expect(page.getByText(runID)).toBeVisible()
    await expect(page.getByRole('cell', { name: 'unknown_provider_capture', exact: true })).toHaveCount(20)
    await expect(page.getByRole('button', { name: '下一頁' })).toBeEnabled()
    runFailure = 503
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('無法更新對帳執行；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByRole('cell', { name: 'unknown_provider_capture', exact: true })).toHaveCount(20)
    await expect(page.getByRole('button', { name: '下一頁' })).toBeDisabled()
    runFailure = null
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByText('無法更新對帳執行；以下是上次成功讀取的資料')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '下一頁' })).toBeEnabled()
    runFailure = 403
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('沒有權限查看對帳執行')).toBeVisible()
    await expect(page.getByRole('cell', { name: 'unknown_provider_capture', exact: true })).toHaveCount(0)

    let discrepancyFailure: number | null = null
    await page.route(`**/admin/api/discrepancies/${encodeURIComponent(discrepancyID)}`, async (route) => {
      if (discrepancyFailure === null) return route.continue()
      await route.fulfill({ status: discrepancyFailure, contentType: 'application/json', body: JSON.stringify({ error: { code: discrepancyFailure === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: '讀取失敗' } }) })
    })
    await page.goto(`${app.baseURL}/admin/discrepancies/${encodeURIComponent(discrepancyID)}`)
    await expect(page.getByText('known payment operation')).toBeVisible()
    await expect(page.getByText('100 USD succeeded')).toBeVisible()
    await expect(page.getByRole('button', { name: '規劃修復' })).toBeEnabled()
    discrepancyFailure = 503
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('無法更新對帳差異；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText('known payment operation')).toBeVisible()
    await expect(page.getByText('100 USD succeeded')).toBeVisible()
    await expect(page.getByRole('button', { name: '規劃修復' })).toBeDisabled()
    await expect(page.getByRole('button', { name: '記錄人工決議' })).toBeDisabled()
    discrepancyFailure = null
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByText('無法更新對帳差異；以下是上次成功讀取的資料')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '規劃修復' })).toBeEnabled()
    discrepancyFailure = 403
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('沒有權限查看對帳差異')).toBeVisible()
    await expect(page.getByText('known payment operation')).toHaveCount(0)
    await expect(page.getByText('100 USD succeeded')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '規劃修復' })).toHaveCount(0)
  } finally {
    await app.stop()
  }
})
