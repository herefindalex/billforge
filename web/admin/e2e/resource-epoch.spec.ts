import { execFileSync } from 'node:child_process'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('resource views distinguish epoch time from draft unpublished time', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    const insertDraft = `
import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
db.execute("INSERT INTO price_versions(id,plan_id,version,currency,fixed_amount_minor,published_at,checksum,publication_state,effective_from) VALUES('draft-epoch','epoch-plan',1,'USD',100,0,'','draft',0)")
db.commit()
`
    execFileSync('python3', ['-c', insertDraft, app.commercePath])
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    await page.goto(`${app.baseURL}/admin/data/catalog-selections?plan_id=pro&cohort=default`)
    await expect(page.getByRole('row').filter({ hasText: 'pro-v1' }).getByText('1970-01-01T00:00:00Z')).toBeVisible()

    await page.goto(`${app.baseURL}/admin/data/prices?id_prefix=basic-v1`)
    await page.getByRole('row').filter({ hasText: 'basic-v1' }).getByRole('button', { name: '詳情' }).click()
    let detail = page.getByRole('dialog', { name: '資料詳情' })
    await expect(detail.getByRole('row', { name: /PublishedAt/ }).getByText('未知', { exact: true })).toBeVisible()

    await page.goto(`${app.baseURL}/admin/data/prices?id_prefix=draft-epoch`)
    await page.getByRole('row').filter({ hasText: 'draft-epoch' }).getByRole('button', { name: '詳情' }).click()
    detail = page.getByRole('dialog', { name: '資料詳情' })
    await expect(detail.getByRole('row', { name: /PublishedAt/ }).getByText('未知', { exact: true })).toBeVisible()
    await expect(detail.getByRole('row', { name: /EffectiveFrom/ }).getByText('1970-01-01T00:00:00Z')).toBeVisible()
  } finally {
    await app.stop()
  }
})
