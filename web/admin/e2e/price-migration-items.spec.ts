import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('價格遷移詳情從伺服器分頁，並以全批次衝突數判斷恢復', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    const id = `migration-items-${randomUUID()}`
    await page.route(`**/admin/api/price-migrations/${id}`, async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
        ID: id, Cohort: 'default', TargetPriceVersionID: 'price-v2', Status: 'paused',
        ItemCount: '25', PendingCount: '0', AppliedCount: '24', ConflictedCount: '1', SkippedCount: '0',
      }) })
    })
    const makeItem = (number: number, status: string) => ({
      MigrationID: id, SubscriptionID: `sub-${String(number).padStart(3, '0')}`,
      FromPriceVersionID: 'price-v1', TargetPriceVersionID: 'price-v2',
      SeatQuantity: '1', ExpectedRevision: '1', PriorAmountMinor: '9007199254740993',
      TargetAmountMinor: '9007199254740995', CurrentEntitlementStatus: 'active',
      ProjectedEntitlementRule: 'pro', EffectiveAt: '2026-09-28T00:00:00Z',
      Status: status, ConflictReason: status === 'conflicted' ? 'revision_changed' : '',
    })
    await page.route(`**/admin/api/price-migrations/${id}/items?*`, async (route) => {
      const query = new URL(route.request().url()).searchParams
      const filtered = query.get('status') === 'conflicted'
      const second = query.get('cursor') === 'page-2'
      const items = filtered ? [makeItem(25, 'conflicted')] : second
        ? Array.from({ length: 5 }, (_, index) => makeItem(index + 21, index === 4 ? 'conflicted' : 'applied'))
        : Array.from({ length: 20 }, (_, index) => makeItem(index + 1, 'applied'))
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
        items, next_cursor: filtered || second ? '' : 'page-2', observed_at: '2026-09-28T00:00:00Z',
      }) })
    })

    await page.goto(`${app.baseURL}/admin/price-migrations/${id}`)
    await expect(page.getByText('sub-001')).toBeVisible()
    await expect(page.getByText('sub-021')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '恢復批次' })).toHaveCount(0)
    await page.getByRole('button', { name: '下一頁' }).click()
    await expect(page.getByText('sub-021')).toBeVisible()
    await expect(page.getByText('sub-001')).toHaveCount(0)
    await page.getByRole('button', { name: '重新整理' }).click()
    await expect(page.getByText('sub-001')).toBeVisible()
    await page.getByRole('combobox', { name: '篩選遷移項目狀態' }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('衝突', { exact: true }).click()
    await expect(page.getByText('sub-025')).toBeVisible()
    await expect(page.getByText('sub-021')).toHaveCount(0)
    await expect(page.getByText('訂閱 revision 已變更')).toBeVisible()
  } finally {
    await app.stop()
  }
})
