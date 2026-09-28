import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('批次工作首次讀取失敗及進度更新失敗有不同呈現並可恢復', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const id = `job-refresh-${randomUUID()}`
    const item = (itemID: string, status: string) => ({ id: itemID, target_type: 'subscription', target_id: itemID, period_key: '2026-09', status, updated_at: '2026-09-28T00:00:00Z' })
    const job = (secondStatus: string) => ({
      id,
      command_id: 'cmd-refresh',
      kind: 'entitlement_refresh',
      status: secondStatus === 'succeeded' ? 'succeeded' : 'waiting_verification',
      created_at: '2026-09-28T00:00:00Z',
      updated_at: '2026-09-28T00:00:00Z',
      items: [item('sub-first', 'succeeded'), item('sub-second', secondStatus)],
    })
    let response: 'failure' | 'initial' | 'updated' = 'failure'
    await page.route(`**/admin/api/jobs/${id}`, async (route) => {
      if (response === 'failure') {
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'QUERY_FAILED', message: '暫時無法讀取' } }) })
      } else {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(job(response === 'updated' ? 'succeeded' : 'waiting_verification')) })
      }
    })

    await page.goto(`${app.baseURL}/admin/jobs/${id}`)
    await expect(page.getByText('無法載入工作狀態')).toBeVisible()
    await expect(page.getByText('1 / 2')).toHaveCount(0)
    response = 'initial'
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByText('1 / 2')).toBeVisible()
    await expect(page.getByText('sub-second')).toBeVisible()

    response = 'failure'
    await page.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByText('無法更新工作進度；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText(/上次讀取：/)).toBeVisible()
    await expect(page.getByText('1 / 2')).toBeVisible()
    await expect(page.getByText('sub-second')).toBeVisible()

    response = 'updated'
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByText('無法更新工作進度；以下是上次成功讀取的資料')).toHaveCount(0)
    await expect(page.getByText('2 / 2')).toBeVisible()
  } finally {
    await app.stop()
  }
})
