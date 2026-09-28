import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('來源讀取失敗後仍可用原鍵查詢各專用表單的命令', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    let commandID = ''
    let failNextReplay = false
    await page.route('**/admin/api/commands', async (route) => {
      if (route.request().method() !== 'POST') return route.continue()
      if (failNextReplay) {
        failNextReplay = false
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'COMMAND_PENDING_RETRY', message: '原命令暫時無法查詢', retryable: true } }) })
        return
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ id: commandID }) })
    })

    for (const actionID of ['C03', 'C04', 'C05', 'C06', 'C23'] as const) {
      const id = `source-read-${actionID}-${randomUUID()}`
      const key = `original-${randomUUID()}`
      commandID = `cmd-${id}`
      failNextReplay = actionID === 'C03'
      const migration = actionID === 'C23'
      const cancel = actionID === 'C05' || actionID === 'C06'
      const path = migration ? `/price-migrations/${id}/pause` : cancel ? `/subscriptions/${id}/cancel` : actionID === 'C04' ? `/subscriptions/${id}/upgrade` : `/subscriptions/${id}/schedule-plan`
      const source = migration ? `price-migrations/${id}` : `subscriptions/${id}`
      const storageKey = migration ? `billforge:admin:pause-migration:${id}` : cancel ? `billforge:admin:cancel:${id}` : `billforge:admin:${actionID}:${id}`
      const storageValue = migration ? key : JSON.stringify(cancel ? { key, previewID: `preview-${id}`, revision: '1', actionID } : { key, previewID: `preview-${id}`, payload: { quote_id: 'quote-existing', fingerprint: 'existing-fingerprint', revision: '1' } })
      await page.evaluate(([name, value]) => sessionStorage.setItem(name, value), [storageKey, storageValue])
      await page.route(`**/admin/api/${source}`, async (route) => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'QUERY_FAILED', message: '暫時無法讀取來源' } }) }))

      await page.goto(`${app.baseURL}/admin${path}`)
      await expect(page.getByText(migration ? '遷移批次無法載入' : '訂閱無法載入')).toBeVisible()
      await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
      const replay = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST')
      await page.getByRole('button', { name: '查詢原命令' }).click()
      const request = await replay
      expect(request.headers()['idempotency-key']).toBe(key)
      expect((request.postDataJSON() as { action_id: string }).action_id).toBe(actionID)
      if (actionID === 'C03') {
        await expect(page.getByText('原命令查詢未成功')).toBeVisible()
        await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
        const secondReplay = page.waitForRequest((next) => next.url().endsWith('/admin/api/commands') && next.method() === 'POST')
        await page.getByRole('button', { name: '查詢原命令' }).click()
        expect((await secondReplay).headers()['idempotency-key']).toBe(key)
      }
      await expect(page.getByRole('link', { name: '開啟命令頁面' })).toHaveAttribute('href', `/admin/commands/${commandID}`)
      await page.unroute(`**/admin/api/${source}`)
    }
  } finally {
    await app.stop()
  }
})
