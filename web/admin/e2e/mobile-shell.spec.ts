import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

test('手機寬度下登入、導覽、查詢與操作頁不造成整頁水平捲動', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    await page.setViewportSize({ width: 390, height: 844 })
    const assertPageWidth = async (pageName: string) => {
      const { width, viewport } = await page.evaluate(() => ({ width: document.documentElement.scrollWidth, viewport: window.innerWidth }))
      if (width > viewport + 1) {
        const offenders = await page.evaluate(() => Array.from(document.querySelectorAll('body *')).map((element) => {
          const box = element.getBoundingClientRect()
          return { tag: element.tagName, className: typeof element.className === 'string' ? element.className : '', left: Math.round(box.left), right: Math.round(box.right), scroll: element.scrollWidth, client: element.clientWidth }
        }).filter((item) => item.right > window.innerWidth + 1 && item.left < window.innerWidth).slice(0, 25))
        console.log(`mobile overflow ${pageName}: ${JSON.stringify(offenders)}`)
      }
      expect(width, `${pageName} 的整頁寬度`).toBeLessThanOrEqual(viewport + 1)
    }

    await page.goto(`${app.baseURL}/admin/login`)
    await expect(page.getByRole('textbox', { name: /帳號/ })).toBeVisible()
    await assertPageWidth('登入')
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
    await assertPageWidth('概覽')

    const menuTrigger = page.getByRole('button', { name: '開啟選單' })
    await menuTrigger.focus()
    await page.keyboard.press('Enter')
    await expect(page.locator('.ant-drawer')).toBeVisible()
    await assertPageWidth('選單')
    await page.locator('.ant-drawer').focus()
    await page.keyboard.press('Escape')
    await expect(page.locator('.ant-drawer').getByRole('menuitem', { name: '建立報價' })).toHaveCount(0)
    await expect(menuTrigger).toBeFocused()
    await menuTrigger.click()
    await expect(page.locator('.ant-drawer')).toBeVisible()
    await page.locator('.ant-drawer').getByRole('menuitem', { name: '建立報價' }).click()
    await expect(page.getByRole('heading', { name: '建立報價' })).toBeVisible()
    await assertPageWidth('建立報價')

    await page.goto(`${app.baseURL}/admin/customers`)
    await expect(page.locator('.ant-table')).toBeVisible()
    await assertPageWidth('客戶查詢')
    await page.goto(`${app.baseURL}/admin/commands`)
    await expect(page.locator('.ant-table')).toBeVisible()
    await assertPageWidth('命令查詢')

    await menuTrigger.click()
    const menuItems = await page.locator('.ant-drawer').getByRole('menuitem').allTextContents()
    expect(menuItems.length).toBeGreaterThan(20)
    await page.keyboard.press('Escape')
    await expect(page.locator('.ant-drawer').getByRole('menuitem')).toHaveCount(0)
    for (let index = 0; index < menuItems.length; index += 1) {
      await menuTrigger.click()
      await page.locator('.ant-drawer').getByRole('menuitem').nth(index).click()
      await expect(page.locator('.ant-drawer').getByRole('menuitem')).toHaveCount(0)
      await assertPageWidth(`導覽項目 ${menuItems[index]}`)
    }

    await page.setViewportSize({ width: 320, height: 720 })
    for (const path of ['/admin/', '/admin/subscriptions', '/admin/quotes/new', '/admin/commands']) {
      await page.goto(`${app.baseURL}${path}`)
      await expect(page.locator('.app-content')).toBeVisible()
      await assertPageWidth(`320px ${path}`)
    }
  } finally {
    await app.stop()
  }
})
