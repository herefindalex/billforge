import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('migration detail marks stale owners, blocks cutover actions, and hides data on denial', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const legacyID = `legacy:stale-${randomUUID()}`
    const customerID = `customer-stale-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/account-migrations/new`)
    for (const [label, value] of [
      ['既有帳戶 ID', legacyID], ['Commerce 客戶 ID', customerID],
      ['受益人 ID', customerID], ['價格 Cohort', 'default'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('combobox', { name: '是否有歷史資料' }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('沒有', { exact: true }).click()
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認連結帳戶' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認連結帳戶' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    let failureStatus: number | null = null
    await page.route(`**/admin/api/account-migrations/${encodeURIComponent(legacyID)}`, async (route) => {
      if (failureStatus === null) return route.continue()
      await route.fulfill({ status: failureStatus, contentType: 'application/json', body: JSON.stringify({ error: { code: failureStatus === 403 ? 'FORBIDDEN' : 'QUERY_FAILED', message: '讀取失敗' } }) })
    })
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}`)
    await expect(page.getByText(customerID, { exact: true }).first()).toBeVisible()
    await expect(page.getByText('legacy', { exact: true }).first()).toBeVisible()
    await expect(page.getByRole('button', { name: '切換讀取' })).toBeEnabled()
    await expect(page.getByRole('button', { name: '停止遷移' })).toBeEnabled()
    await page.getByLabel('報價 P95 上限（毫秒）').fill('5000')
    await page.getByLabel('未知付款上限').fill('0')
    await page.getByLabel('未結對帳差異上限').fill('0')
    await page.getByRole('button', { name: '計算 Readiness' }).click()
    await expect(page.getByText('可切換', { exact: true })).toBeVisible()

    failureStatus = 503
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('無法更新帳戶遷移；以下是上次成功讀取的資料')).toBeVisible()
    await expect(page.getByText(customerID, { exact: true }).first()).toBeVisible()
    await expect(page.getByText('legacy', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('可切換', { exact: true })).toHaveCount(0)
    for (const name of ['計算 Readiness', '查詢權益', '比對報價', '比對權益', '回填來源', '切換讀取', '切換寫入', '停止遷移']) {
      await expect(page.getByRole('button', { name })).toBeDisabled()
    }

    failureStatus = null
    await page.getByRole('button', { name: /重\s*試/ }).click()
    await expect(page.getByText('無法更新帳戶遷移；以下是上次成功讀取的資料')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '切換讀取' })).toBeEnabled()
    await expect(page.getByRole('button', { name: '停止遷移' })).toBeEnabled()

    failureStatus = 403
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('沒有權限查看帳戶遷移')).toBeVisible()
    await expect(page.getByText(customerID, { exact: true })).toHaveCount(0)
    await expect(page.getByText('legacy', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '切換讀取' })).toHaveCount(0)
  } finally {
    await app.stop()
  }
})
