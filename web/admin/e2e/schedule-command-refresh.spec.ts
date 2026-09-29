import { execFileSync } from 'node:child_process'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('方案排程命令暫時讀取失敗可顯示舊資料，權限遭拒時隱藏舊結果', async ({ page }) => {
  const app = await startLocalAdmin({ seedDemo: true })
  try {
    const subscriptionID = execFileSync('python3', ['-c', 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); print(db.execute("SELECT id FROM subscriptions LIMIT 1").fetchone()[0])', app.commercePath], { encoding: 'utf8' }).trim()
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const session = await (await page.request.get(`${app.baseURL}/admin/api/session`)).json() as { actor_id: string }
    await page.evaluate(([actorID, id]) => {
      sessionStorage.setItem(`billforge:admin:command:${actorID}:C03:${id}`, 'cmd-schedule-read-ui')
    }, [session.actor_id, subscriptionID])

    let failureStatus: 503 | 403 | null = null
    await page.route('**/admin/api/commands/cmd-schedule-read-ui', async (route) => {
      if (failureStatus !== null) {
        await route.fulfill({ status: failureStatus, contentType: 'application/json', body: JSON.stringify({ error: { code: failureStatus === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: '無法讀取' } }) })
        return
      }
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
        id: 'cmd-schedule-read-ui', actor_id: session.actor_id, idempotency_key: 'original-key',
        action_id: 'C03', target_id: subscriptionID, status: 'accepted',
        result_refs: { schedule_id: 'schedule-cached-ui' },
        created_at: '2026-09-28T00:00:00Z', updated_at: '2026-09-28T00:00:00Z',
      }) })
    })

    await page.goto(`${app.baseURL}/admin/subscriptions/${encodeURIComponent(subscriptionID)}/schedule-plan`)
    await expect(page.getByText('schedule-cached-ui')).toBeVisible()
    failureStatus = 503
    await expect(page.getByText('無法更新命令狀態；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText('schedule-cached-ui')).toBeVisible()

    failureStatus = 403
    await expect(page.getByText('命令狀態無法載入')).toBeVisible()
    await expect(page.getByText('schedule-cached-ui')).toHaveCount(0)

    failureStatus = null
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByText('schedule-cached-ui')).toBeVisible()
  } finally {
    await app.stop()
  }
})
