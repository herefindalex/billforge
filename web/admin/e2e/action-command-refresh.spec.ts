import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('共用操作頁在命令狀態讀取失敗時停用舊狀態操作', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const session = await (await page.request.get(`${app.baseURL}/admin/api/session`)).json() as { actor_id: string }
    await page.evaluate((actorID) => {
      sessionStorage.setItem(`billforge:admin:command:${actorID}:C16:refund-action-ui`, 'cmd-action-ui')
    }, session.actor_id)
    let failRead = false
    let status: 'accepted' | 'waiting_verification' | 'succeeded' = 'accepted'
    await page.route('**/admin/api/commands/cmd-action-ui', async (route) => {
      if (failRead) {
        await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'QUERY_FAILED', message: '暫時無法讀取' } }) })
      } else {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
          id: 'cmd-action-ui', actor_id: session.actor_id, idempotency_key: 'original-key',
          action_id: 'C16', target_id: 'refund-action-ui', status,
          result_refs: status === 'succeeded' ? { target_id: 'refund-action-ui', operation_status: 'succeeded' } : {},
          created_at: '2026-09-28T00:00:00Z', updated_at: '2026-09-28T00:00:00Z',
        }) })
      }
    })

    await page.goto(`${app.baseURL}/admin/refunds/refund-action-ui/dispatch`)
    await expect(page.getByRole('button', { name: '繼續原命令' })).toBeEnabled()
    failRead = true
    await page.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByText('無法更新命令狀態；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText(/上次讀取：/)).toBeVisible()
    await expect(page.getByRole('button', { name: '繼續原命令' })).toBeDisabled()

    failRead = false
    status = 'waiting_verification'
    await page.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByText('無法更新命令狀態；以下是上次成功讀取的資料')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '重新查證' })).toBeEnabled()
    failRead = true
    await page.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByRole('button', { name: '重新查證' })).toBeDisabled()

    failRead = false
    status = 'succeeded'
    await page.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByRole('button', { name: '執行另一個操作' })).toBeEnabled()
    failRead = true
    await page.getByRole('button', { name: /更\s*新/ }).click()
    await expect(page.getByRole('button', { name: '執行另一個操作' })).toBeDisabled()
  } finally {
    await app.stop()
  }
})

test('共用操作頁開始另一命令時清除前一命令的待確認錯誤', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const session = await (await page.request.get(`${app.baseURL}/admin/api/session`)).json() as { actor_id: string }
    await page.route('**/admin/api/commands', async (route) => {
      if (route.request().method() !== 'POST') return route.continue()
      await route.fulfill({
        status: 503,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'COMMAND_PENDING_RETRY', message: '原命令已受理，結果待確認', retryable: true }, command_id: 'cmd-previous-ui' }),
      })
    })
    await page.route('**/admin/api/commands/cmd-previous-ui', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'cmd-previous-ui', actor_id: session.actor_id, idempotency_key: 'original-key',
          action_id: 'C38', target_id: 'account-ui', status: 'succeeded', result_refs: {},
          created_at: '2026-09-28T00:00:00Z', updated_at: '2026-09-28T00:00:00Z',
        }),
      })
    })

    await page.goto(`${app.baseURL}/admin/account-migrations/account-ui/shadow-entitlements`)
    await page.getByRole('textbox', { name: '訂閱 ID' }).fill('subscription-ui')
    await page.getByRole('textbox', { name: '既有權益狀態' }).fill('active')
    await page.getByRole('button', { name: '確認記錄比對' }).click()
    await page.getByRole('button', { name: '確認記錄比對' }).last().click()
    await expect(page.getByText('命令結果尚未確認', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '執行另一個操作' })).toBeVisible()

    await page.getByRole('button', { name: '執行另一個操作' }).click()
    await expect(page.getByText('命令結果尚未確認', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('textbox', { name: '訂閱 ID' })).toBeEnabled()
    await expect(page.getByRole('button', { name: '確認記錄比對' }).first()).toBeEnabled()
  } finally {
    await app.stop()
  }
})

test('共用操作頁修改被拒絕的輸入後不保留舊命令錯誤', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    let submissions = 0
    await page.route('**/admin/api/commands', async (route) => {
      if (route.request().method() !== 'POST') return route.continue()
      submissions += 1
      await route.fulfill({
        status: 422,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'INVALID_PAYLOAD', message: '既有狀態不符' } }),
      })
    })

    await page.goto(`${app.baseURL}/admin/account-migrations/account-ui/shadow-entitlements`)
    await page.getByRole('textbox', { name: '訂閱 ID' }).fill('subscription-ui')
    await page.getByRole('textbox', { name: '既有權益狀態' }).fill('active')
    await page.getByRole('button', { name: '確認記錄比對' }).click()
    await page.getByRole('button', { name: '確認記錄比對' }).last().click()
    await expect(page.getByText('命令未被接受，請檢查輸入')).toBeVisible()
    expect(submissions).toBe(1)

    await page.getByRole('textbox', { name: '既有權益狀態' }).fill('inactive')
    await expect(page.getByText('命令未被接受，請檢查輸入')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '確認記錄比對' }).first()).toBeEnabled()
  } finally {
    await app.stop()
  }
})
