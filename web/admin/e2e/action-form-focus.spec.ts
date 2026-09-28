import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('cancelling an action confirmation restores keyboard focus to its trigger', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    await page.goto(`${app.baseURL}/admin/lab/faults/focus-only-operation`)
    const trigger = page.getByRole('main').getByRole('button', { name: '確認故障票據' })
    await trigger.focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(page.getByText('請輸入操作種類')).toBeVisible()
    const operationKind = page.getByRole('combobox', { name: '操作種類' })
    await expect(operationKind).toHaveAttribute('aria-invalid', 'true')
    const descriptionID = await operationKind.getAttribute('aria-describedby')
    expect(descriptionID).toBeTruthy()
    await expect(page.locator(`#${descriptionID}`)).toContainText('請輸入操作種類')

    await page.getByRole('combobox', { name: '操作種類' }).click()
    await page.getByText('付款', { exact: true }).last().click()
    await page.getByRole('combobox', { name: '故障模式' }).click()
    await page.getByText('回應遺失', { exact: true }).last().click()

    await trigger.focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(trigger).toBeFocused()
  } finally {
    await app.stop()
  }
})
