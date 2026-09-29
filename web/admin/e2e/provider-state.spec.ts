import { execFileSync } from 'node:child_process'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

function seedProvider(databasePath: string, first: number, last: number) {
  execFileSync('python3', ['-c', `
import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
with db:
    for number in range(int(sys.argv[2]), int(sys.argv[3]) + 1):
        amount = 9007199254740993 if number == 1 else 1000
        db.execute('INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES(?,?,?,?)',
            (f'provider-capture-{number}', amount, 'USD', 'succeeded'))
`, databasePath, String(first), String(last)])
}

test('lab control shows separate provider status and paginated capture and refund facts', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    seedProvider(app.providerPath, 1, 21)
    execFileSync('python3', ['-c', `
import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
with db:
    db.execute("INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES('failed-capture',300,'USD','definitively_failed')")
    db.execute("INSERT INTO refunds(provider_key,source_capture_key,amount_minor,currency,status) VALUES('provider-refund-1','provider-capture-2',200,'USD','succeeded')")
`, app.providerPath])

    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
    await page.goto(`${app.baseURL}/admin/lab/controls`)
    const statusCard = page.locator('.ant-card').filter({ hasText: 'Fake provider 狀態' })
    await expect(statusCard.getByText('22', { exact: true })).toBeVisible()
    await statusCard.getByRole('button', { name: '查看收款結果' }).click()

    await expect(page.getByRole('row', { name: /failed-capture/ })).toBeVisible()
    await expect(page.getByRole('row', { name: /provider-capture-1(?:\D|$)/ })).toHaveCount(0)
    seedProvider(app.providerPath, 22, 22)
    await page.getByRole('button', { name: '下一頁' }).click()
    const oldCapture = page.getByRole('row', { name: /provider-capture-1(?:\D|$)/ })
    await expect(oldCapture).toContainText('USD 90,071,992,547,409.93')
    await expect(page.getByRole('row', { name: /provider-capture-22/ })).toHaveCount(0)

    await page.getByRole('combobox', { name: '提供者狀態' }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('確定失敗', { exact: true }).click()
    await expect(page.getByRole('row', { name: /failed-capture/ })).toBeVisible()
    await expect(page.getByRole('row', { name: /provider-capture-1(?:\D|$)/ })).toHaveCount(0)

    await page.goto(`${app.baseURL}/admin/lab/controls`)
    await page.getByRole('button', { name: '查看退款結果' }).click()
    const refund = page.getByRole('row', { name: /provider-refund-1/ })
    await expect(refund).toContainText('provider-capture-2')
    await expect(refund).toContainText('USD 2.00')
    await page.route('**/admin/api/lab/provider-refunds?*', async (route) => route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ error: { code: 'FORBIDDEN', message: '沒有權限' } }) }))
    await page.getByRole('button', { name: '更新資料' }).click()
    await expect(page.getByText('無法讀取模擬提供者資料')).toBeVisible()
    await expect(refund).toHaveCount(0)
  } finally {
    await app.stop()
  }
})

test('read-only administrator cannot view provider diagnostics', async ({ page }) => {
  const app = await startLocalAdmin({ capabilities: 'read' })
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
    await page.goto(`${app.baseURL}/admin/lab/controls`)
    await expect(page.getByText('目前業務時鐘', { exact: true })).toBeVisible()
    await expect(page.getByText('Fake provider 狀態', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '查看收款結果' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '設定時鐘' })).toBeDisabled()
    expect((await page.request.get(`${app.baseURL}/admin/api/lab/status`)).status()).toBe(403)
    expect((await page.request.get(`${app.baseURL}/admin/api/lab/provider-captures`)).status()).toBe(403)
  } finally {
    await app.stop()
  }
})
