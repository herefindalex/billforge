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

    await page.goto(`${app.baseURL}/admin/lab/clock`)
    const trigger = page.getByRole('main').locator('button[type="submit"]')
    await trigger.focus()
    await page.keyboard.press('Enter')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(page.getByText('請輸入模式')).toBeVisible()
    const clockMode = page.getByRole('combobox', { name: '模式' })
    await expect(clockMode).toHaveAttribute('aria-invalid', 'true')
    const descriptionID = await clockMode.getAttribute('aria-describedby')
    expect(descriptionID).toBeTruthy()
    await expect(page.locator(`#${descriptionID}`)).toContainText('請輸入模式')

    await clockMode.click()
    await page.getByText('實際時間', { exact: true }).last().click()

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
