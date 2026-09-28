import { execFileSync } from 'node:child_process'
import { expect, test } from '@playwright/test'
import { startLocalAdmin } from './server'

function insertJobs(databasePath: string, first: number, last: number) {
  execFileSync('python3', ['-c', `
import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
with db:
    for number in range(int(sys.argv[2]), int(sys.argv[3]) + 1):
        command_id = f'job-list-command-{number}'
        job_id = f'job-list-{number}'
        stamp = '2026-09-28T12:00:00Z'
        db.execute('INSERT INTO admin_commands(id,actor_id,idempotency_key,action_id,target_id,payload_json,payload_hash,status,business_time,created_at,updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)',
            (command_id, 'local-admin', command_id, 'C44', '', '{}', 'fixture', 'succeeded', stamp, stamp, stamp))
        db.execute('INSERT INTO admin_jobs(id,command_id,kind,status,created_at,updated_at) VALUES(?, ?, ?, ?, ?, ?)',
            (job_id, command_id, 'C44', 'partial' if number == 22 else 'succeeded', stamp, stamp))
        statuses = ('succeeded', 'conflicted') if number == 22 else ('succeeded',)
        for index, status in enumerate(statuses):
            db.execute('INSERT INTO admin_job_items(id,job_id,target_type,target_id,payload_hash,status,updated_at) VALUES(?, ?, ?, ?, ?, ?, ?)',
                (f'{job_id}-item-{index}', job_id, 'subscription', f'subscription-{number}-{index}', 'fixture', status, stamp))
`, databasePath, String(first), String(last)])
}

test('job list finds old work without its ID and keeps pagination stable across insertion', async ({ page }) => {
  const app = await startLocalAdmin()
  try {
    insertJobs(app.commercePath, 1, 22)
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    await page.goto(`${app.baseURL}/admin/jobs`)
    await expect(page.locator('.ant-card-head-title').getByText('批次工作', { exact: true })).toBeVisible()
    const recent = page.getByRole('row', { name: /job-list-22/ })
    await expect(recent).toContainText('1 / 2')
    await expect(recent).toContainText('partial')
    await expect(page.getByRole('row', { name: /job-list-2(?:\D|$)/ })).toHaveCount(0)

    insertJobs(app.commercePath, 23, 23)
    await page.getByRole('button', { name: '下一頁' }).click()
    await expect(page.getByRole('row', { name: /job-list-2(?:\D|$)/ })).toBeVisible()
    await expect(page.getByRole('row', { name: /job-list-1(?:\D|$)/ })).toBeVisible()
    await expect(page.getByRole('row', { name: /job-list-23/ })).toHaveCount(0)

    await page.reload()
    await expect(page.getByRole('row', { name: /job-list-1(?:\D|$)/ })).toBeVisible()
    await page.getByRole('row', { name: /job-list-1(?:\D|$)/ }).getByRole('button', { name: '查看逐項進度' }).click()
    await expect(page).toHaveURL(/\/admin\/jobs\/job-list-1$/)
    await expect(page.getByText('job-list-1', { exact: true })).toBeVisible()
  } finally {
    await app.stop()
  }
})
