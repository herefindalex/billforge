import { execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('entitlement preview shows that a hundred item batch leaves more subscriptions', async ({ page }) => {
  const admin = await startLocalAdmin()
  try {
    const prefix = `backlog-${randomUUID().replaceAll('-', '')}`
    const seed = `
import sqlite3,sys,time
db=sqlite3.connect(sys.argv[1])
prefix=sys.argv[2]
expiry=time.time_ns()+3600_000_000_000
with db:
  db.execute("WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<101) INSERT INTO quotes(id,customer_id,price_version_id,amount_minor,currency,expires_at,fingerprint) SELECT ?||'-quote-'||n,?||'-customer-'||n,'basic-v1',2000,'USD',?,?||'-fingerprint-'||n FROM seq",(prefix,prefix,expiry,prefix))
  db.execute("WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<101) INSERT INTO subscriptions(id,quote_id,customer_id,price_version_id,status,created_at) SELECT ?||'-sub-'||n,?||'-quote-'||n,?||'-customer-'||n,'basic-v1','active',? FROM seq",(prefix,prefix,prefix,time.time_ns()))
`
    execFileSync('python3', ['-c', seed, admin.commercePath, prefix])

    await page.goto(`${admin.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(admin.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(admin.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
    await page.goto(`${admin.baseURL}/admin/jobs/entitlement-refresh`)
    const previewResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '建立預覽' }).click()
    const response = await previewResponse
    expect(response.ok()).toBe(true)
    const preview = await response.json()
    expect(preview.impact.subscription_count).toBe('100')
    expect(preview.impact.has_more_candidates).toBe('true')
    expect(preview.impact.items).toHaveLength(100)
    await expect(page.getByText('本批之外仍有候選項目')).toBeVisible()
    await expect(page.getByText('本次僅處理預覽列出的項目。完成後請建立新的預覽與批次，繼續處理其餘項目。')).toBeVisible()
  } finally {
    admin.stop()
  }
})
