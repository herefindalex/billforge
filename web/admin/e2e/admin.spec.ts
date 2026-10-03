import { execFileSync } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { appendFileSync, readFileSync, readdirSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect, test, type Page, type Request, type Route } from '@playwright/test'
import { submitClockControl } from './control-commands'
import { startLocalAdmin, type LocalAdmin } from './server'

test.describe.serial('local Web Admin with real SQLite and fake provider', () => {
  let app: LocalAdmin
  let lastAuditedCommandRowID = 0
  const browserActions = new Set<string>()
  const browserCommandKeys = new Set<string>()
  const browserCommandBodies = new Map<string, string[]>()
  const replayedActionCases = new Set<string>()

  test.beforeAll(async () => { app = await startLocalAdmin() })
  test.afterAll(async () => { await app?.stop() })
  test.beforeEach(async ({ page, context }) => {
    if (!process.env.BILLFORGE_E2E_ACTION_CASE_AUDIT) return
    browserActions.clear()
    browserCommandKeys.clear()
    browserCommandBodies.clear()
    const observe = (request: Request) => {
      if (request.method() !== 'POST' || new URL(request.url()).pathname !== '/admin/api/commands') return
      try {
        const payload = request.postDataJSON() as { action_id?: unknown }
        if (typeof payload.action_id === 'string' && /^C\d{2}$/.test(payload.action_id)) {
          browserActions.add(payload.action_id)
          const key = request.headers()['idempotency-key']
          if (key) {
            const actionKey = `${payload.action_id}\u0000${key}`
            browserCommandKeys.add(actionKey)
            const body = request.postData()
            if (body !== null) {
              const candidates = browserCommandBodies.get(actionKey) ?? []
              if (!candidates.includes(body)) candidates.push(body)
              browserCommandBodies.set(actionKey, candidates)
            }
          }
        }
      } catch { /* malformed request; the command response test covers its rejection */ }
    }
    page.on('request', observe)
    context.on('page', (newPage) => newPage.on('request', observe))
  })
  test.afterEach(async ({ browser }, testInfo) => {
    const auditPath = process.env.BILLFORGE_E2E_ACTION_CASE_AUDIT
    if (!auditPath) return
    const output = execFileSync('python3', ['-c', `import json, sqlite3, sys
db = sqlite3.connect('file:' + sys.argv[1] + '?mode=ro', uri=True)
rows = list(db.execute('SELECT c.rowid,c.id,c.idempotency_key,c.action_id,c.status,(SELECT COUNT(*) FROM admin_command_receipts r WHERE r.command_id=c.id) FROM admin_commands c WHERE c.rowid>? ORDER BY c.rowid', (int(sys.argv[2]),)))
print(json.dumps(rows))`, app.commercePath, String(lastAuditedCommandRowID)], { encoding: 'utf8' })
    const rows = JSON.parse(output) as [number, string, string, string, string, number][]
    if (rows.length > 0) lastAuditedCommandRowID = rows[rows.length - 1][0]
    const browserReceiptRows = rows.filter(([, , key, action, status, receipt]) =>
      status === 'succeeded' && receipt === 1 && browserCommandKeys.has(`${action}\u0000${key}`),
    )
    const browserReceiptActions = new Set(browserReceiptRows.map(([, , , action]) => action))
    const browserReplayActions: string[] = []
    const browserConflictActions: string[] = []
    const replayCandidates = browserReceiptRows.filter(([, , , action]) => !replayedActionCases.has(action))
    if (replayCandidates.length > 0 && testInfo.status === 'passed') {
      const auditContext = await browser.newContext()
      try {
        const auditPage = await auditContext.newPage()
        await signIn(auditPage)
        const sessionResponse = await auditPage.request.get(`${app.baseURL}/admin/api/session`)
        expect(sessionResponse.status()).toBe(200)
        const session = await sessionResponse.json() as { csrf_token: string }
        for (const [, commandID, key, action] of replayCandidates) {
          if (replayedActionCases.has(action)) continue
          const bodies = browserCommandBodies.get(`${action}\u0000${key}`) ?? []
          if (bodies.length === 0) throw new Error(`No captured browser body for ${action} command ${commandID}`)
          const headers = {
            Origin: app.baseURL,
            'Content-Type': 'application/json',
            'X-CSRF-Token': session.csrf_token,
            'Idempotency-Key': key,
          }
          let matchedBody: string | null = null
          for (const body of bodies) {
            const replay = await auditPage.request.post(`${app.baseURL}/admin/api/commands`, {
              headers,
              data: body,
            })
            if (replay.status() === 409) continue
            expect(replay.status(), `${action} replay HTTP status`).toBe(200)
            expect((await replay.json()).id, `${action} replay command ID`).toBe(commandID)
            matchedBody = body
            break
          }
          if (matchedBody === null) throw new Error(`No original browser body replayed ${action} command ${commandID}`)
          const conflict = await auditPage.request.post(`${app.baseURL}/admin/api/commands`, {
            headers,
            data: divergentCommandBody(matchedBody),
          })
          expect(conflict.status(), `${action} divergent replay HTTP status`).toBe(409)
          expect((await conflict.json()).error?.code, `${action} divergent replay code`).toBe('IDEMPOTENCY_CONFLICT')
          expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID), `${action} receipt count`).toBe(1)
          replayedActionCases.add(action)
          browserReplayActions.push(action)
          browserConflictActions.push(action)
        }
        expect(count('SELECT COUNT(*) FROM admin_commands WHERE rowid>?', String(lastAuditedCommandRowID)), 'replay created a new command').toBe(0)
      } finally {
        await auditContext.close()
      }
    }
    appendFileSync(auditPath, JSON.stringify({
      test: testInfo.title,
      browser_actions: [...browserActions].sort(),
      browser_receipt_actions: [...browserReceiptActions].sort(),
      browser_replay_actions: browserReplayActions.sort(),
      browser_conflict_actions: browserConflictActions.sort(),
      commands: rows.map(([, , , action, status, receipt]) => ({ action, status, receipt: receipt === 1 })),
    }) + '\n')
  })

  async function signIn(page: Page) {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page).toHaveURL(/\/admin\/?$/)
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
  }

  function divergentCommandBody(body: string): string {
    const command = JSON.parse(body) as { action_id?: unknown; target_id?: unknown; preview_id?: unknown; payload?: unknown }
    if (typeof command.preview_id === 'string' && command.preview_id !== '') {
      command.preview_id += '-audit-conflict'
    } else if (typeof command.target_id === 'string' && command.target_id !== '') {
      command.target_id += '-audit-conflict'
  } else if ((command.action_id === 'C01' || command.action_id === 'C26') && command.payload && typeof command.payload === 'object' && !Array.isArray(command.payload)) {
    const payload = command.payload as Record<string, unknown>
    const field = command.action_id === 'C01' ? 'customer_id' : 'event_id'
    if (typeof payload[field] !== 'string' || payload[field] === '') throw new Error(`${command.action_id} browser body has no ${field}`)
    payload[field] += '-audit-conflict'
  } else if (command.action_id === 'C33' && command.payload && typeof command.payload === 'object' && !Array.isArray(command.payload)) {
    const payload = command.payload as Record<string, unknown>
    if (typeof payload.as_of !== 'string' || Number.isNaN(Date.parse(payload.as_of))) {
      throw new Error('C33 browser body has no valid as_of')
    }
    payload.as_of = new Date(Date.parse(payload.as_of) + 1000).toISOString()
  } else {
      throw new Error(`No valid divergent request field for ${String(command.action_id)}`)
    }
    return JSON.stringify(command)
  }

  async function commitThenDropResponse(page: Page, route: Route) {
    const request = route.request()
    const original = request.headers()
    const headers: Record<string, string> = { origin: original.origin ?? new URL(request.url()).origin }
    for (const name of ['content-type', 'x-csrf-token', 'idempotency-key', 'x-request-id']) {
      if (original[name]) headers[name] = original[name]
    }
    // Use a separate connection so committing the request cannot consume the intercepted route.
    try {
      return await page.request.post(request.url(), { headers, data: request.postData() ?? '' })
    } finally {
      await route.abort('failed')
    }
  }

  test('process credentials override conflicting env file credentials', async ({ page }) => {
    const secondary = await startLocalAdmin({
      processCredentials: { username: 'admin-override', password: 'process #= password 123456' },
    })
    try {
      await page.goto(`${secondary.baseURL}/admin/login`)
      await page.getByRole('textbox', { name: /帳號/ }).fill(secondary.fileUsername)
      await page.getByRole('textbox', { name: /密碼/ }).fill(secondary.filePassword)
      await page.getByRole('button', { name: '登 入' }).click()
      await expect(page.getByText('帳號或密碼無法驗證。')).toBeVisible()
      await page.getByRole('textbox', { name: /帳號/ }).fill(secondary.username)
      await page.getByRole('textbox', { name: /密碼/ }).fill(secondary.password)
      await page.getByRole('button', { name: '登 入' }).click()
      await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
    } finally {
      await secondary.stop()
    }
  })

  test('missing admin password refuses server startup', async () => {
    await expect(startLocalAdmin({ omitFilePassword: true })).rejects.toThrow('admin password must be 12–72 UTF-8 bytes')
  })

  test('admin listener rejects a foreign Host for API and assets', async ({ page }) => {
    const headers = { Host: 'attacker.example:8080' }
    const api = await page.request.get(`${app.baseURL}/admin/api/session/csrf`, { headers })
    const asset = await page.request.get(`${app.baseURL}/admin/login`, { headers })
    expect(api.status()).toBe(403)
    expect(asset.status()).toBe(403)
    const local = await page.request.get(`${app.baseURL}/admin/api/session/csrf`)
    expect(local.status()).toBe(200)
  })

  test('admin listener does not expose the v1 write API', async ({ page }) => {
    await signIn(page)
    const customerID = `v1-blocked-${randomUUID()}`
    const response = await page.request.post(`${app.baseURL}/v1/quotes`, {
      data: { customer_id: customerID, plan_id: 'basic' },
    })
    expect(response.status()).toBe(404)
    expect(count('SELECT COUNT(*) FROM quotes WHERE customer_id=?', customerID)).toBe(0)
  })

  test('admin password and internal token stay out of assets responses URLs and logs', async ({ page }) => {
    const assetsDir = resolve(dirname(fileURLToPath(import.meta.url)), '../dist/assets')
    const assets = readdirSync(assetsDir)
    expect(assets.length).toBeGreaterThan(0)
    const secrets = [app.password, app.internalToken]
    for (const asset of assets) {
      const bytes = readFileSync(join(assetsDir, asset))
      for (const secret of secrets) expect(bytes.includes(Buffer.from(secret))).toBe(false)
    }
    const index = await page.request.get(`${app.baseURL}/admin/`)
    expect(index.status()).toBe(200)
    const indexBody = await index.text()
    for (const secret of secrets) expect(indexBody).not.toContain(secret)
    const unauthenticated = await page.request.get(`${app.baseURL}/admin/api/overview`, { headers: { 'X-Request-ID': 'req_client_forged' } })
    expect(unauthenticated.status()).toBe(401)
    const errorRequestID = unauthenticated.headers()['x-request-id']
    expect(errorRequestID).toMatch(/^req_[A-Za-z0-9_-]+$/)
    expect(errorRequestID).not.toBe('req_client_forged')
    expect((await unauthenticated.json()).request_id).toBe(errorRequestID)
    await signIn(page)
    for (const path of ['/admin/api/session', '/admin/api/overview']) {
      const response = await page.request.get(`${app.baseURL}${path}`)
      expect(response.status()).toBe(200)
      const body = await response.text()
      for (const secret of secrets) expect(body).not.toContain(secret)
    }
    const sessionResponse = await page.request.get(`${app.baseURL}/admin/api/session`)
    const session = await sessionResponse.json() as { csrf_token: string }
    const sessionCookie = (await page.context().cookies(`${app.baseURL}/admin/`)).find((cookie) => cookie.name.includes('session'))
    expect(sessionCookie?.value).toBeTruthy()
    const customerID = `audit-secret-check-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    const submitted = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '建立報價' }).click()
    const submittedRequestID = (await submitted).headers()['x-request-id']
    expect(submittedRequestID).toMatch(/^req_[A-Za-z0-9_-]+$/)
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const auditCommandID = scalar("SELECT id FROM admin_commands WHERE action_id='C01' AND json_extract(payload_json,'$.customer_id')=?", customerID)
    expect(scalar('SELECT request_id FROM admin_commands WHERE id=?', auditCommandID)).toBe(submittedRequestID)
    expect(count('SELECT COUNT(*) FROM admin_audit WHERE command_id=? AND request_id=?', auditCommandID, submittedRequestID)).toBe(2)
    const auditEvidence = scalar(`SELECT json_group_array(json_object('actor',actor_id,'action',action_id,'target',target_id,
      'reason',reason,'before',before_json,'after',after_json)) FROM admin_audit WHERE command_id=?`, auditCommandID)
    expect(count('SELECT COUNT(*) FROM admin_audit WHERE command_id=?', auditCommandID)).toBe(2)
    expect(auditEvidence).toContain(scalar('SELECT id FROM quotes WHERE customer_id=?', customerID))
    await page.goto(`${app.baseURL}/admin/commands/${auditCommandID}`)
    await expect(page.getByText('受理請求 ID')).toBeVisible()
    await expect(page.getByText(submittedRequestID)).toBeVisible()
    for (const secret of [...secrets, session.csrf_token, sessionCookie!.value]) expect(auditEvidence).not.toContain(secret)
    for (const secret of secrets) {
      expect(page.url()).not.toContain(encodeURIComponent(secret))
      expect(app.logOutput()).not.toContain(secret)
    }
  })

  test('failed refresh labels retained overview and resource data as stale', async ({ page }) => {
    await signIn(page)
    await expect(page.getByText(/資料觀測時間：/)).toBeVisible()
    await page.route('**/admin/api/overview', async (route) => {
      await route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":{"code":"QUERY_FAILED","message":"Read temporarily unavailable"}}' })
    })
    await page.getByRole('button', { name: '更新資料' }).click()
    await expect(page.getByText('資料更新失敗，顯示上次讀取結果')).toBeVisible({ timeout: 20_000 })
    await expect(page.getByText(/資料觀測時間：/)).toBeVisible()
    await page.unroute('**/admin/api/overview')
    await page.getByRole('button', { name: '更新資料' }).click()
    await expect(page.getByText('資料更新失敗，顯示上次讀取結果')).toHaveCount(0)

    await page.goto(`${app.baseURL}/admin/payments`)
    await expect(page.getByRole('table')).toBeVisible()
    await expect(page.getByText(/觀測：/)).toBeVisible()
    await page.route('**/admin/api/payments?**', async (route) => {
      await route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":{"code":"QUERY_FAILED","message":"Read temporarily unavailable"}}' })
    })
    await page.getByRole('button', { name: '更新資料' }).click()
    await expect(page.getByText('資料更新失敗，顯示上次讀取結果')).toBeVisible({ timeout: 20_000 })
    await expect(page.getByRole('table')).toBeVisible()
    await expect(page.getByText(/觀測：/)).toBeVisible()
    await page.unroute('**/admin/api/payments?**')
    await page.getByRole('button', { name: '更新資料' }).click()
    await expect(page.getByText('資料更新失敗，顯示上次讀取結果')).toHaveCount(0)
  })

  test('a timed-out resource read labels its retained page as stale', async ({ page }) => {
    await signIn(page)
    await page.goto(`${app.baseURL}/admin/payments`)
    await expect(page.getByRole('table')).toBeVisible()
    let release!: () => void
    let started!: () => void
    const gate = new Promise<void>((resolve) => { release = resolve })
    const requestStarted = new Promise<void>((resolve) => { started = resolve })
    await page.route('**/admin/api/payments?**', async (route) => {
      started()
      await gate
      try { await route.continue() } catch { /* the browser aborted the timed-out read */ }
    })
    try {
      await page.getByRole('button', { name: '更新資料' }).click()
      await requestStarted
      await expect(page.getByText('資料更新失敗，顯示上次讀取結果')).toBeVisible({ timeout: 15_000 })
      await expect(page.getByText('讀取逾時，請重新讀取')).toBeVisible()
      await expect(page.getByRole('table')).toBeVisible()
    } finally {
      release()
      await page.unroute('**/admin/api/payments?**')
    }
    await page.getByRole('button', { name: '更新資料' }).click()
    await expect(page.getByText('資料更新失敗，顯示上次讀取結果')).toHaveCount(0)
  })

  test('held command explains restricted verification and clears after original receipt', async ({ page }) => {
    // UI state probe; lab and API tests establish the verification guard.
    await signIn(page)
    let verified = false
    let verificationRequests = 0
    const command = () => ({
      id: 'held-ui', actor_id: 'local-admin', idempotency_key: 'held-ui-key',
      action_id: 'C47', target_id: 'payment-held-ui',
      status: verified ? 'succeeded' : 'accepted',
      result_refs: verified ? { target_id: 'payment-held-ui' } : {},
      error_code: verified ? '' : 'PERMISSION_REVOKED_REVIEW',
      created_at: '2026-09-26T00:00:00Z', updated_at: '2026-09-26T00:00:00Z',
    })
    await page.route('**/admin/api/commands/held-ui', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(command()) })
    })
    await page.route('**/admin/api/commands/held-ui/resume', async (route) => {
      verificationRequests += 1
      verified = true
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(command()) })
    })
    await page.goto(`${app.baseURL}/admin/commands/held-ui`)
    await expect(page.getByText('權限已變更，命令暫停待查證')).toBeVisible()
    await page.getByRole('button', { name: '檢查既有收據' }).click()
    await expect(page.getByText('權限已變更，命令暫停待查證')).toHaveCount(0)
    await expect(page.getByText('succeeded')).toBeVisible()
    expect(verificationRequests).toBe(1)
  })

  test('retryable command response opens the original command after reload', async ({ page }) => {
    await signIn(page)
    const commandID = `provider-pending-${randomUUID()}`
    const operationID = `payment-pending-${randomUUID()}`
    let status = 'accepted'
    let submissions = 0
    let resumes = 0
    const command = () => ({
      id: commandID, actor_id: 'local-admin', idempotency_key: 'original-key',
      action_id: 'C10', target_id: operationID, status,
      created_at: '2026-09-27T00:00:00Z', updated_at: '2026-09-27T00:00:00Z',
    })
    await page.route('**/admin/api/commands**', async (route) => {
      const path = new URL(route.request().url()).pathname
      if (path === '/admin/api/commands' && route.request().method() === 'POST') {
        submissions += 1
        await route.fulfill({
          status: 503, contentType: 'application/json',
          body: JSON.stringify({ error: { code: 'COMMAND_PENDING_RETRY', message: '命令已記錄，請查證', retryable: true }, command_id: commandID }),
        })
      } else if (path === `/admin/api/commands/${commandID}` && route.request().method() === 'GET') {
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(command()) })
      } else if (path === `/admin/api/commands/${commandID}/resume` && route.request().method() === 'POST') {
        resumes += 1
        status = 'succeeded'
        await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(command()) })
      } else {
        await route.continue()
      }
    })
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/reconcile`)
    await page.getByRole('button', { name: '確認查證' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認查證' }).click()
    await expect(page.getByText(commandID)).toBeVisible()
    await expect(page.getByRole('button', { name: '繼續原命令' })).toBeVisible()
    expect(submissions).toBe(1)
    await page.reload()
    await expect(page.getByRole('button', { name: '繼續原命令' })).toBeVisible()
    await page.getByRole('button', { name: '開啟命令頁面' }).click()
    await expect(page).toHaveURL(new RegExp(`/admin/commands/${commandID}$`))
    await page.getByRole('button', { name: '繼續原命令' }).click()
    await expect(page.getByText('succeeded')).toBeVisible()
    expect(submissions).toBe(1)
    expect(resumes).toBe(1)
  })

  test('command polling stops after leaving its detail page', async ({ page }) => {
    await signIn(page)
    let reads = 0
    await page.route('**/admin/api/commands/polling-ui', async (route) => {
      reads += 1
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({
        id: 'polling-ui', actor_id: 'local-admin', idempotency_key: 'polling-ui-key',
        action_id: 'C47', target_id: 'payment-polling-ui', status: 'accepted',
        result_refs: {}, error_code: '',
        created_at: '2026-09-26T00:00:00Z', updated_at: '2026-09-26T00:00:00Z',
      }) })
    })
    await page.goto(`${app.baseURL}/admin/commands/polling-ui`)
    await expect(page.getByText('命令詳情')).toBeVisible()
    await expect.poll(() => reads, { timeout: 5_000 }).toBeGreaterThanOrEqual(2)
    await page.getByRole('menuitem', { name: '營運概覽' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
    await page.waitForTimeout(100)
    const readsAfterLeaving = reads
    await page.waitForTimeout(2_000)
    expect(reads).toBe(readsAfterLeaving)
  })

  test('UTC action field rejects invalid dates and precision before admission', async ({ page }) => {
    await signIn(page)
    const before = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C33')
    await page.goto(`${app.baseURL}/admin/reconciliation-runs/new`)
    const cutoff = page.getByRole('textbox', { name: '核對截止時間（UTC）' })
    for (const invalid of [
      '2026-02-30T12:00:00Z',
      '2026-09-26T12:00:00.1234567891Z',
      '2262-04-11T23:47:16.854775808Z',
    ]) {
      await cutoff.fill(invalid)
      await page.getByRole('button', { name: '確認執行對帳' }).click()
      await expect(page.getByText(/必須是有效 UTC 時間/)).toBeVisible()
    }
    await cutoff.fill('2026-09-26T12:00:00.123456789Z')
    await cutoff.press('Tab')
    await expect(page.getByText(/必須是有效 UTC 時間/)).toHaveCount(0)
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C33')).toBe(before)

    const usageBefore = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C26')
    await page.goto(`${app.baseURL}/admin/usage-events/new`)
    await page.getByRole('textbox', { name: '訂閱 ID' }).fill('invalid-time-probe')
    await page.getByRole('textbox', { name: 'Meter ID' }).fill('api_calls')
    await page.getByRole('textbox', { name: '來源' }).fill('api')
    await page.getByRole('textbox', { name: '事件 ID' }).fill('invalid-time-probe')
    await page.getByRole('textbox', { name: '發生時間（UTC）' }).fill('2026-09-26T12:00:00.1234567891Z')
    await page.getByRole('textbox', { name: '數量' }).fill('1')
    await page.getByRole('button', { name: '檢查並記錄' }).click()
    await expect(page.getByText(/請輸入有效的 UTC 時間/)).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C26')).toBe(usageBefore)

    const quoteBefore = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C01')
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: '客戶 ID' }).fill('int64-form-probe')
    await page.getByRole('textbox', { name: '方案 ID' }).fill('basic')
    const seats = page.getByRole('textbox', { name: '席次' })
    await seats.fill('9223372036854775808')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByText('席次不可超過 int64 上限')).toBeVisible()
    await seats.fill('9007199254740993')
    await seats.press('Tab')
    await expect(page.getByText('席次不可超過 int64 上限')).toHaveCount(0)
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C01')).toBe(quoteBefore)

    const priceBefore = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C18')
    await page.goto(`${app.baseURL}/admin/prices/pro/new`)
    await page.getByRole('textbox', { name: '價格版本 ID' }).fill('a10-denominator-probe')
    await page.getByRole('textbox', { name: '版本號' }).fill('99')
    await page.getByRole('textbox', { name: '固定金額（最小單位）' }).fill('100')
    await page.getByRole('textbox', { name: '每席金額（最小單位）' }).fill('1')
    await page.getByRole('textbox', { name: '包含任務量' }).fill('0')
    await page.getByRole('textbox', { name: '超額費率分子' }).fill('1')
    await page.getByRole('textbox', { name: '生效起點（UTC）' }).fill('2026-09-26T12:00:00Z')
    const denominator = page.getByRole('textbox', { name: '超額費率分母' })
    await denominator.fill('9223372036854775808')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('超額費率分母不可超過 int64 上限')).toBeVisible()
    await denominator.fill('9007199254740993')
    await denominator.press('Tab')
    await expect(page.getByText('超額費率分母不可超過 int64 上限')).toHaveCount(0)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('9007199254740993', { exact: true })).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C18')).toBe(priceBefore)

    for (const field of [
      { action: 'C07', route: 'invoices/missing-invoice/payments/new', label: '付款金額（最小貨幣單位）', button: '預覽付款', error: '付款金額不可超過 int64 上限', extra: null },
      { action: 'C11', route: 'invoices/missing-invoice/reductions/new', label: '減額（最小貨幣單位）', button: '建立預覽', error: '減額（最小貨幣單位）不可超過 int64 上限', extra: { label: '減額理由', value: 'precision probe' } },
      { action: 'C12', route: 'credits/missing-grant/apply', label: '抵扣金額（最小貨幣單位）', button: '建立預覽', error: '抵扣金額（最小貨幣單位）不可超過 int64 上限', extra: { label: '目標帳單 ID', value: 'missing-invoice' } },
      { action: 'C15', route: 'credits/missing-grant/refunds/new', label: '退款金額（最小貨幣單位）', button: '建立預覽', error: '退款金額（最小貨幣單位）不可超過 int64 上限', extra: null },
    ]) {
      const before = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', field.action)
      await page.goto(`${app.baseURL}/admin/${field.route}`)
      if (field.extra) await page.getByRole('textbox', { name: field.extra.label }).fill(field.extra.value)
      const amount = page.getByRole('textbox', { name: field.label })
      await amount.fill('9223372036854775808')
      await page.getByRole('button', { name: field.button }).click()
      await expect(page.getByText(field.error)).toBeVisible()
      await amount.fill('9007199254740993')
      await amount.press('Tab')
      await expect(page.getByText(field.error)).toHaveCount(0)
      expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', field.action)).toBe(before)
    }
  })

  test('editing a price after preview requires a fresh preview', async ({ page }) => {
    await signIn(page)
    const before = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C18')
    await page.goto(`${app.baseURL}/admin/prices/pro/new`)
    await page.getByRole('textbox', { name: '價格版本 ID' }).fill(`preview-change-${randomUUID()}`)
    await page.getByRole('textbox', { name: '版本號' }).fill('99')
    const fixedMinor = page.getByRole('textbox', { name: '固定金額（最小單位）' })
    await fixedMinor.fill('100')
    await page.getByRole('textbox', { name: '每席金額（最小單位）' }).fill('1')
    await page.getByRole('textbox', { name: '包含任務量' }).fill('0')
    await page.getByRole('textbox', { name: '超額費率分子' }).fill('1')
    await page.getByRole('textbox', { name: '超額費率分母' }).fill('1')
    await page.getByRole('textbox', { name: '生效起點（UTC）' }).fill(new Date(Date.now() - 60_000).toISOString().replace(/\.\d{3}Z$/, 'Z'))
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()
    await fixedMinor.fill('200')
    await expect(page.getByText('操作預覽', { exact: true })).toHaveCount(0)
    await expect(page.getByText('輸入已變更，請重新建立預覽')).toBeVisible()
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()
    await expect(page.getByText('USD 2.00', { exact: true })).toBeVisible()

    let releasePreview!: () => void
    let previewRequested!: () => void
    const gate = new Promise<void>((resolve) => { releasePreview = resolve })
    const requested = new Promise<void>((resolve) => { previewRequested = resolve })
    await page.route('**/admin/api/previews', async (route) => {
      previewRequested()
      await gate
      await route.continue()
    })
    await fixedMinor.fill('300')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await requested
    await fixedMinor.fill('400')
    const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews'))
    releasePreview()
    await staleResponse
    await expect(page.getByText('操作預覽', { exact: true })).toHaveCount(0)
    await expect(page.getByText('輸入已變更，請重新建立預覽')).toBeVisible()
    await page.unroute('**/admin/api/previews')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('USD 4.00', { exact: true })).toBeVisible()
    const confirmPrice = page.getByRole('button', { name: '確認發布價格' }).first()
    await confirmPrice.click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(confirmPrice).toBeFocused()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C18')).toBe(before)
  })

  test('an uncertain quote response keeps the original intent until it is recovered', async ({ page }) => {
    await signIn(page)
    const before = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C01')
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    const customer = page.getByRole('textbox', { name: '客戶 ID' })
    await customer.fill(`uncertain-quote-${randomUUID()}`)
    await page.getByRole('textbox', { name: '方案 ID' }).fill('basic')
    let dropped = false
    await page.route('**/admin/api/commands', async (route) => {
      if (!dropped && route.request().method() === 'POST') {
        dropped = true
        await commitThenDropResponse(page, route)
      } else {
        await route.continue()
      }
    })
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByText('有一筆送出結果尚未確認')).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C01')).toBe(before + 1)
    await page.reload()
    await expect(page.getByText('有一筆送出結果尚未確認')).toBeVisible()
    await expect(customer).toBeDisabled()
    await expect(page.getByRole('button', { name: '建立報價' })).toBeDisabled()
    await page.getByRole('button', { name: '查詢原操作' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C01')).toBe(before + 1)
    await page.getByRole('button', { name: '建立另一筆報價' }).click()
    await expect(customer).toBeEnabled()
  })

  test('a timed-out quote response recovers the original command after reload', async ({ page }) => {
    await signIn(page)
    const before = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C01')
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: '客戶 ID' }).fill(`timeout-quote-${randomUUID()}`)
    await page.getByRole('textbox', { name: '方案 ID' }).fill('basic')
    let release!: () => void
    let admitted!: () => void
    const gate = new Promise<void>((resolve) => { release = resolve })
    const commandAdmitted = new Promise<void>((resolve) => { admitted = resolve })
    await page.route('**/admin/api/commands', async (route) => {
      if (route.request().method() !== 'POST') return route.continue()
      const response = await route.fetch()
      admitted()
      await gate
      try { await route.fulfill({ response }) } catch { /* the browser aborted the timed-out response */ }
    })
    try {
      await page.getByRole('button', { name: '建立報價' }).click()
      await commandAdmitted
      expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C01')).toBe(before + 1)
      await expect(page.getByText('回應逾時，請查詢原操作狀態')).toBeVisible({ timeout: 25_000 })
      await expect(page.getByRole('button', { name: '建立報價' })).toBeDisabled()
    } finally {
      release()
      await page.unroute('**/admin/api/commands')
    }
    await page.reload()
    await expect(page.getByText('有一筆送出結果尚未確認')).toBeVisible()
    await page.getByRole('button', { name: '查詢原操作' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C01')).toBe(before + 1)
  })

  test('uncertain admission error keeps the original quote key until a successful retry', async ({ page }) => {
    await signIn(page)
    const customerID = `admission-unknown-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: '客戶 ID' }).fill(customerID)
    await page.getByRole('textbox', { name: '方案 ID' }).fill('basic')

    let firstKey = ''
    let firstPayload = ''
    let failed = false
    await page.route('**/admin/api/commands', async (route) => {
      if (route.request().method() !== 'POST') {
        await route.continue()
        return
      }
      if (!failed) {
        failed = true
        firstKey = route.request().headers()['idempotency-key']
        firstPayload = route.request().postData() ?? ''
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: { code: 'COMMAND_ADMISSION_UNKNOWN', message: 'Command acceptance is unconfirmed; retry with the same request key', retryable: true } }),
        })
        return
      }
      await route.continue()
    })

    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByText('有一筆送出結果尚未確認')).toBeVisible()
    expect(firstKey).toBeTruthy()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C01' AND json_extract(payload_json,'$.customer_id')=?", customerID)).toBe(0)

    await page.reload()
    await expect(page.getByText('有一筆送出結果尚未確認')).toBeVisible()
    const retry = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST')
    await page.getByRole('button', { name: '查詢原操作' }).click()
    const retriedRequest = await retry
    expect(retriedRequest.headers()['idempotency-key']).toBe(firstKey)
    expect(retriedRequest.postData()).toBe(firstPayload)
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const commandID = scalar("SELECT id FROM admin_commands WHERE action_id='C01' AND json_extract(payload_json,'$.customer_id')=?", customerID)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C01' AND json_extract(payload_json,'$.customer_id')=?", customerID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM quotes WHERE customer_id=?', customerID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
  })

  test('definitively rejected quote and usage inputs unlock forms without creating commands', async ({ page }) => {
    await signIn(page)
    const quotesBefore = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C01')
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    const customer = page.getByRole('textbox', { name: '客戶 ID' })
    await customer.fill('   ')
    await page.getByRole('textbox', { name: '方案 ID' }).fill('basic')
    const quoteRejected = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '建立報價' }).click()
    expect((await quoteRejected).status()).toBe(422)
    await expect(page.getByText('輸入未被接受，請修改後重試')).toBeVisible()
    await expect(page.locator('#quote-input-rejection')).toBeFocused()
    await expect(customer).toBeEnabled()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C01')).toBe(quotesBefore)

    const usageBefore = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C26')
    await page.goto(`${app.baseURL}/admin/usage-events/new`)
    await page.getByRole('textbox', { name: '訂閱 ID' }).fill('not-admitted')
    await page.getByRole('textbox', { name: 'Meter ID' }).fill('tasks')
    const source = page.getByLabel('來源', { exact: true })
    await source.fill('   ')
    await page.getByRole('textbox', { name: '事件 ID' }).fill(`rejected-${randomUUID()}`)
    await page.getByRole('textbox', { name: '發生時間（UTC）' }).fill(new Date().toISOString())
    await page.getByRole('textbox', { name: '數量' }).fill('1')
    await page.getByRole('button', { name: '檢查並記錄' }).click()
    const usageRejected = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('dialog').getByRole('button', { name: '確認記錄' }).click()
    expect((await usageRejected).status()).toBe(422)
    await expect(page.getByText('輸入未被接受，請修改後重試')).toBeVisible()
    await expect(page.locator('#usage-input-rejection')).toBeFocused()
    await expect(source).toBeEnabled()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C26')).toBe(usageBefore)
  })

  test('rejected financial action restores an editable form and does not retain its intent', async ({ page }) => {
    await signIn(page)
    const { invoiceID } = await createPaidSubscription(page, `rejected-reduction-${randomUUID()}`, 'basic')
    const commandsBefore = count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C11' AND target_id=?", invoiceID)
    const correctionsBefore = count('SELECT COUNT(*) FROM corrections WHERE invoice_id=?', invoiceID)
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/reductions/new`)
    const amount = page.getByRole('textbox', { name: '減額（最小貨幣單位）' })
    await amount.fill('500')
    await page.getByRole('textbox', { name: '減額理由' }).fill('rejected input recovery')
    await page.getByRole('button', { name: '建立預覽' }).click()
    const confirm = page.getByRole('main').getByRole('button', { name: '確認減額' })
    await expect(confirm).toBeVisible()
    await page.route('**/admin/api/commands', async (route) => {
      if (route.request().method() === 'POST' && (route.request().postData() ?? '').includes('"action_id":"C11"')) {
        await route.fulfill({ status: 422, contentType: 'application/json', body: '{"error":{"code":"INVALID_COMMAND","message":"Rejected for test"}}' })
      } else {
        await route.continue()
      }
    })
    await confirm.click()
    await page.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
    await expect(page.getByText('命令未被接受，請檢查輸入')).toBeVisible()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(confirm).toBeFocused()
    await expect(amount).toBeEnabled()
    await amount.fill('400')
    await expect(page.getByText('操作預覽', { exact: true })).toHaveCount(0)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C11' AND target_id=?", invoiceID)).toBe(commandsBefore)
    expect(count('SELECT COUNT(*) FROM corrections WHERE invoice_id=?', invoiceID)).toBe(correctionsBefore)
    await page.reload()
    await expect(amount).toBeEnabled()
    await expect(page.getByText('原命令的結果尚未確認')).toHaveCount(0)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(amount).toHaveAttribute('aria-invalid', 'true')
    const describedBy = await amount.getAttribute('aria-describedby')
    expect(describedBy).toBeTruthy()
    await expect(page.locator(`[id="${describedBy}"]`)).toContainText('請輸入減額（最小貨幣單位）')
  })

  test('lost reduction response recovers its original correction and credit', async ({ page }) => {
    await signIn(page)
    const { invoiceID } = await createPaidSubscription(page, `reduction-lost-${randomUUID()}`, 'basic')
    const originalInvoice = await page.request.get(`${app.baseURL}/admin/api/invoices/${invoiceID}`)
    expect(originalInvoice.status()).toBe(200)
    const originalLines = (await originalInvoice.json()).invoice.Lines
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/reductions/new`)
    await page.getByRole('textbox', { name: '減額（最小貨幣單位）' }).fill('500')
    await page.getByRole('textbox', { name: '減額理由' }).fill('lost response recovery')
    await page.getByRole('button', { name: '建立預覽' }).click()
    let originalKey = ''
    let dropped = false
    await page.route('**/admin/api/commands', async (route) => {
      if (!dropped && route.request().method() === 'POST' && (route.request().postData() ?? '').includes('"action_id":"C11"')) {
        dropped = true
        originalKey = route.request().headers()['idempotency-key']
        await commitThenDropResponse(page, route)
      } else {
        await route.continue()
      }
    })
    await page.getByRole('main').getByRole('button', { name: '確認減額' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    expect(originalKey).toBeTruthy()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C11' AND target_id=?", invoiceID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM corrections WHERE invoice_id=?', invoiceID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?', invoiceID)).toBe(1)
    await page.reload()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    const replay = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST' && (request.postData() ?? '').includes('"action_id":"C11"'))
    await page.getByRole('button', { name: '用原 request key 查詢' }).click()
    expect((await replay).headers()['idempotency-key']).toBe(originalKey)
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C11' AND target_id=?", invoiceID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM corrections WHERE invoice_id=?', invoiceID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?', invoiceID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C11' AND c.target_id=?", invoiceID)).toBe(1)
    const recoveredInvoice = await page.request.get(`${app.baseURL}/admin/api/invoices/${invoiceID}`)
    expect(recoveredInvoice.status()).toBe(200)
    expect((await recoveredInvoice.json()).invoice.Lines).toEqual(originalLines)
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}`)
    for (const [label, amount] of [
      ['原始金額', 'USD 20.00'],
      ['目前應收', 'USD 15.00'],
      ['已確認收款', 'USD 20.00'],
      ['尚待支付', 'USD 0.00'],
    ]) {
      await expect(page.locator('.ant-descriptions-row').filter({ has: page.getByText(label, { exact: true }) }).getByText(amount, { exact: true })).toBeVisible()
    }
  })

  test('a lost quote acceptance response recovers one subscription and invoice', async ({ page }) => {
    await signIn(page)
    const customerID = `accept-recovery-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=? ORDER BY rowid DESC LIMIT 1', customerID)
    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await page.getByRole('button', { name: '預覽接受' }).click()
    let originalKey = ''
    let dropped = false
    await page.route('**/admin/api/commands', async (route) => {
      if (!dropped && route.request().method() === 'POST') {
        dropped = true
        originalKey = route.request().headers()['idempotency-key']
        await commitThenDropResponse(page, route)
      } else {
        await route.continue()
      }
    })
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    expect(originalKey).toBeTruthy()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C02' AND target_id=?", quoteID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM subscriptions WHERE quote_id=?', quoteID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM invoices WHERE subscription_id=(SELECT id FROM subscriptions WHERE quote_id=?)', quoteID)).toBe(1)

    await page.route(`**/admin/api/quotes/${quoteID}`, async (route) => {
      await route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":{"code":"QUERY_FAILED","message":"暫時無法讀取報價"}}' })
    })
    await page.reload()
    await expect(page.getByText('報價無法載入')).toBeVisible()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    const replay = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST')
    await page.getByRole('button', { name: '查詢原命令' }).click()
    expect((await replay).headers()['idempotency-key']).toBe(originalKey)
    await page.getByRole('link', { name: '開啟命令頁面' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C02' AND target_id=?", quoteID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM subscriptions WHERE quote_id=?', quoteID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM invoices WHERE subscription_id=(SELECT id FROM subscriptions WHERE quote_id=?)', quoteID)).toBe(1)
  })

  test('a lost payment command response recovers one operation across sessions and reload', async ({ page, browser }) => {
    await signIn(page)
    const { invoiceID } = await createAcceptedSubscription(page, `payment-recovery-${randomUUID()}`, 'basic')
    const beforeCommands = count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C07' AND target_id=?", invoiceID)
    const beforeOperations = count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/payments/new`)
    await page.getByRole('textbox', { name: '付款金額（最小貨幣單位）' }).fill('1000')
    await page.getByRole('button', { name: '預覽付款' }).click()
    await expect(page.getByRole('button', { name: '確認建立付款' })).toBeVisible()
    let originalKey = ''
    let originalBody = ''
    let dropped = false
    await page.route('**/admin/api/commands', async (route) => {
      if (!dropped && route.request().method() === 'POST') {
        dropped = true
        originalKey = route.request().headers()['idempotency-key']
        originalBody = route.request().postData() ?? ''
        await commitThenDropResponse(page, route)
      } else {
        await route.continue()
      }
    })
    await page.getByRole('button', { name: '確認建立付款' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認建立' }).click()
    await expect(page.getByText('原付款命令的結果尚未確認')).toBeVisible()
    expect(originalKey).toBeTruthy()
    expect(originalBody).toBeTruthy()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C07' AND target_id=?", invoiceID)).toBe(beforeCommands + 1)
    expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(beforeOperations + 1)

    const commandID = scalar("SELECT id FROM admin_commands WHERE action_id='C07' AND target_id=?", invoiceID)
    const freshContext = await browser.newContext()
    try {
      const freshPage = await freshContext.newPage()
      expect((await freshPage.request.get(`${app.baseURL}/admin/api/session`)).status()).toBe(401)
      await signIn(freshPage)
      const session = await (await freshPage.request.get(`${app.baseURL}/admin/api/session`)).json() as { csrf_token: string }
      const replay = await freshPage.request.post(`${app.baseURL}/admin/api/commands`, {
        headers: {
          Origin: app.baseURL,
          'Content-Type': 'application/json',
          'X-CSRF-Token': session.csrf_token,
          'Idempotency-Key': originalKey,
        },
        data: originalBody,
      })
      expect(replay.status()).toBe(200)
      expect((await replay.json()).id).toBe(commandID)
      await freshPage.goto(`${app.baseURL}/admin/commands/${commandID}`)
      await expect(freshPage.getByRole('main').getByText(commandID, { exact: true }).first()).toBeVisible()
      await expect(freshPage.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    } finally {
      await freshContext.close()
    }
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C07' AND target_id=?", invoiceID)).toBe(beforeCommands + 1)
    expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(beforeOperations + 1)

    await page.reload()
    await expect(page.getByText('原付款命令的結果尚未確認')).toBeVisible()
    const replay = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST')
    await page.getByRole('button', { name: '用原 request key 查詢' }).click()
    expect((await replay).headers()['idempotency-key']).toBe(originalKey)
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C07' AND target_id=?", invoiceID)).toBe(beforeCommands + 1)
    expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(beforeOperations + 1)
    expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=? AND amount_minor=1000', invoiceID)).toBe(1)
  })

  test('editing a payment amount invalidates its preview', async ({ page }) => {
    await signIn(page)
    const { invoiceID } = await createAcceptedSubscription(page, `payment-preview-change-${randomUUID()}`, 'basic')
    const before = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C07')
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/payments/new`)
    const amount = page.getByRole('textbox', { name: '付款金額（最小貨幣單位）' })
    await amount.fill('1000')
    await page.getByRole('button', { name: '預覽付款' }).click()
    await expect(page.getByText('付款預覽', { exact: true })).toBeVisible()
    await amount.fill('1500')
    await expect(page.getByText('付款預覽', { exact: true })).toHaveCount(0)
    await expect(page.getByText('付款金額已變更，請重新預覽')).toBeVisible()
    await page.getByRole('button', { name: '預覽付款' }).click()
    await expect(page.getByText('USD 15.00', { exact: true })).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C07')).toBe(before)
  })

  test('editing an overpayment clears the rejected preview before another request', async ({ page }) => {
    await signIn(page)
    const { invoiceID, operationID } = await createAcceptedSubscription(page, `overpayment-edit-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/payments/new`)
    const amount = page.getByRole('textbox', { name: '付款金額（最小貨幣單位）' })
    await amount.fill('2001')
    const rejected = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '預覽付款' }).click()
    expect((await rejected).status()).toBe(409)
    await expect(page.getByText('無法建立預覽', { exact: true })).toBeVisible()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C07' AND target_id=?", invoiceID)).toBe(0)
    expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(1)
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('created')

    await amount.fill('1000')
    await expect(page.getByText('無法建立預覽', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '確認建立付款' })).toHaveCount(0)
    await page.getByRole('button', { name: '預覽付款' }).click()
    await expect(page.getByText('付款預覽', { exact: true })).toBeVisible()
    await expect(page.getByRole('cell', { name: 'USD 10.00', exact: true })).toBeVisible()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C07' AND target_id=?", invoiceID)).toBe(0)
  })

  test('a rejected payment preview releases its unadmitted intent for a fresh preview', async ({ page }) => {
    await signIn(page)
    const { invoiceID } = await createAcceptedSubscription(page, `payment-stale-${randomUUID()}`, 'basic')
    const before = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C07')
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/payments/new`)
    const amount = page.getByRole('textbox', { name: '付款金額（最小貨幣單位）' })
    await amount.fill('1000')
    await page.getByRole('button', { name: '預覽付款' }).click()
    await expect(page.getByText('付款預覽', { exact: true })).toBeVisible()
    await page.route('**/admin/api/commands', async (route) => {
      await route.fulfill({ status: 409, contentType: 'application/json', body: '{"error":{"code":"PREVIEW_STALE","message":"Preview has expired"}}' })
    })
    await page.getByRole('button', { name: '確認建立付款' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認建立' }).click()
    await expect(page.getByText('原預覽已失效，請重新預覽')).toBeVisible()
    await expect(amount).toBeEnabled()
    await expect(page.getByText('原預覽已失效，請檢查新預覽並再次確認')).toBeVisible()
    await expect(page.getByText('付款預覽', { exact: true })).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C07')).toBe(before)
    await page.unroute('**/admin/api/commands')
  })

  test('payment preview shows changed balance after another operator reduces invoice', async ({ page }) => {
    await signIn(page)
    const { invoiceID } = await createAcceptedSubscription(page, `payment-balance-stale-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/payments/new`)
    await page.getByRole('textbox', { name: '付款金額（最小貨幣單位）' }).fill('300')
    await page.getByRole('button', { name: '預覽付款' }).click()
    await expect(page.getByText('付款預覽', { exact: true })).toBeVisible()
    const beforeOperations = count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)

    const other = await page.context().newPage()
    try {
      await other.goto(`${app.baseURL}/admin/invoices/${invoiceID}/reductions/new`)
      await other.getByLabel('減額（最小貨幣單位）', { exact: true }).fill('500')
      await other.getByLabel('減額理由', { exact: true }).fill('other operator reduction')
      await other.getByRole('button', { name: '建立預覽' }).click()
      await other.getByRole('button', { name: '確認減額' }).last().click()
      await other.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

      const response = page.waitForResponse((value) => value.url().endsWith('/admin/api/commands') && value.request().method() === 'POST')
      await page.getByRole('button', { name: '確認建立付款' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認建立' }).click()
      expect((await response).status()).toBe(409)
      const difference = page.locator('.ant-alert').filter({ hasText: '原預覽已失效，請檢查新預覽並再次確認' })
      await expect(difference).toBeVisible()
      await expect(difference.getByText('原先：USD 20.00')).toBeVisible()
      await expect(difference.getByText('現在：USD 15.00')).toBeVisible()
      await expect(page.getByRole('button', { name: '確認建立付款' })).toBeFocused()
      expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(beforeOperations)
      await page.getByRole('button', { name: '確認建立付款' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認建立' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(beforeOperations + 1)
    } finally {
      await other.close()
    }
  })

  test('rejected payment creation restores focus and permits one new payment intent', async ({ page }) => {
    await signIn(page)
    const { invoiceID } = await createAcceptedSubscription(page, `payment-rejected-${randomUUID()}`, 'basic')
    const beforeOperations = count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/payments/new`)
    await page.getByRole('textbox', { name: '付款金額（最小貨幣單位）' }).fill('300')
    await page.getByRole('button', { name: '預覽付款' }).click()
    await expect(page.getByText('付款預覽', { exact: true })).toBeVisible()

    await page.route('**/admin/api/commands', async (route) => {
      if (route.request().method() === 'POST' && (route.request().postData() ?? '').includes('"action_id":"C07"')) {
        await route.fulfill({ status: 422, contentType: 'application/json', body: '{"error":{"code":"INVALID_PAYLOAD","message":"Injected rejection"}}' })
      } else {
        await route.continue()
      }
    }, { times: 1 })
    await page.getByRole('button', { name: '確認建立付款' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認建立' }).click()
    await expect(page.getByText('命令未被接受，請檢查輸入')).toBeVisible()
    await expect(page.getByRole('button', { name: '預覽付款' })).toBeFocused()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C07' AND target_id=?", invoiceID)).toBe(0)
    expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(beforeOperations)

    await page.getByRole('textbox', { name: '付款金額（最小貨幣單位）' }).fill('400')
    await expect(page.getByText('命令未被接受，請檢查輸入', { exact: true })).toHaveCount(0)
    await page.unroute('**/admin/api/commands')
    await page.getByRole('button', { name: '預覽付款' }).click()
    await page.getByRole('button', { name: '確認建立付款' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認建立' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C07' AND target_id=?", invoiceID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(beforeOperations + 1)
    expect(count("SELECT amount_minor FROM payment_operations WHERE invoice_id=? AND status='created'", invoiceID)).toBe(400)
  })

  test('competing payment previews collect only the winning replacement amount', async ({ page }) => {
    await signIn(page)
    const { invoiceID, operationID } = await createAcceptedSubscription(page, `competing-payment-${randomUUID()}`, 'basic')
    const oldProviderKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', operationID)
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/payments/new`)
    await page.getByRole('textbox', { name: '付款金額（最小貨幣單位）' }).fill('1000')
    await page.getByRole('button', { name: '預覽付款' }).click()
    await expect(page.getByText('付款預覽', { exact: true })).toBeVisible()
    const other = await page.context().newPage()
    try {
      await other.goto(`${app.baseURL}/admin/invoices/${invoiceID}/payments/new`)
      await other.getByRole('textbox', { name: '付款金額（最小貨幣單位）' }).fill('1500')
      await other.getByRole('button', { name: '預覽付款' }).click()
      await expect(other.getByText('付款預覽', { exact: true })).toBeVisible()
      await other.getByRole('button', { name: '確認建立付款' }).click()
      await other.getByRole('dialog').getByRole('button', { name: '確認建立' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

      const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
      await page.getByRole('button', { name: '確認建立付款' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認建立' }).click()
      expect((await staleResponse).status()).toBe(409)
      await expect(page.getByText('原預覽已失效，請重新預覽')).toBeVisible()
      await expect(page.getByText('無法建立預覽', { exact: false })).toBeVisible()
      expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(2)
      expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('cancelled')
      const replacementID = scalar("SELECT id FROM payment_operations WHERE invoice_id=? AND status='created'", invoiceID)
      expect(count('SELECT amount_minor FROM payment_operations WHERE id=?', replacementID)).toBe(1500)
      expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C07' AND target_id=? AND status='succeeded'", invoiceID)).toBe(1)
      expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C07' AND target_id=? AND status='failed' AND error_code='PREVIEW_STALE'", invoiceID)).toBe(1)
      const newProviderKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', replacementID)
      expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', oldProviderKey, app.providerPath))).toBe(0)
      expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', newProviderKey, app.providerPath))).toBe(0)

      await other.goto(`${app.baseURL}/admin/payments/${replacementID}/dispatch`)
      await other.getByRole('button', { name: '建立預覽' }).click()
      await other.getByRole('button', { name: '確認送出付款' }).last().click()
      await other.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(scalar('SELECT status FROM payment_operations WHERE id=?', replacementID)).toBe('succeeded')
      expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('cancelled')
      expect(scalar('SELECT amount_minor FROM captures WHERE provider_key=?', newProviderKey, app.providerPath)).toBe('1500')
      expect(scalar('SELECT currency FROM captures WHERE provider_key=?', newProviderKey, app.providerPath)).toBe('USD')
      expect(scalar('SELECT status FROM captures WHERE provider_key=?', newProviderKey, app.providerPath)).toBe('succeeded')
      expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', newProviderKey, app.providerPath)).toBe('1')
      expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', oldProviderKey, app.providerPath)).toBe('0')
      expect(count('SELECT COUNT(*) FROM allocations WHERE invoice_id=?', invoiceID)).toBe(1)
      expect(count('SELECT amount_minor FROM allocations WHERE operation_id=?', replacementID)).toBe(1500)
      expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C07' AND c.target_id=?", invoiceID)).toBe(1)
      expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C09' AND c.target_id=?", replacementID)).toBe(1)
    } finally {
      await other.close()
    }
  })

  test('replacing a payment invalidates an older dispatch preview in another tab', async ({ page }) => {
    await signIn(page)
    const { invoiceID, operationID } = await createAcceptedSubscription(page, `stale-dispatch-${randomUUID()}`, 'basic')
    const oldProviderKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', operationID)
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByRole('button', { name: '確認送出付款' })).toBeVisible()

    const other = await page.context().newPage()
    try {
      await other.goto(`${app.baseURL}/admin/invoices/${invoiceID}/payments/new`)
      await other.getByRole('textbox', { name: '付款金額（最小貨幣單位）' }).fill('1500')
      await other.getByRole('button', { name: '預覽付款' }).click()
      await other.getByRole('button', { name: '確認建立付款' }).click()
      await other.getByRole('dialog').getByRole('button', { name: '確認建立' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

      const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST' && (response.request().postData() ?? '').includes('"action_id":"C09"'))
      await page.getByRole('button', { name: '確認送出付款' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
      expect((await staleResponse).status()).toBe(409)
      await expect(page.getByText('原預覽已失效', { exact: true })).toBeVisible()
      await expect(page.getByText('無法建立預覽', { exact: false })).toBeVisible()
      await expect(page.getByRole('button', { name: '確認送出付款' })).toHaveCount(0)

      expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('cancelled')
      expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C09' AND target_id=? AND status='failed' AND error_code='PREVIEW_STALE'", operationID)).toBe(1)
      expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C09' AND c.target_id=?", operationID)).toBe(0)
      const replacementID = scalar("SELECT id FROM payment_operations WHERE invoice_id=? AND status='created'", invoiceID)
      const newProviderKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', replacementID)
      expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', oldProviderKey, app.providerPath)).toBe('0')

      await other.goto(`${app.baseURL}/admin/payments/${replacementID}/dispatch`)
      await other.getByRole('button', { name: '建立預覽' }).click()
      await other.getByRole('button', { name: '確認送出付款' }).last().click()
      await other.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(scalar('SELECT amount_minor FROM captures WHERE provider_key=?', newProviderKey, app.providerPath)).toBe('1500')
      expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', oldProviderKey, app.providerPath)).toBe('0')
    } finally {
      await other.close()
    }
  })

  test('a second tab cannot complete the same payment dispatch', async ({ page }) => {
    await signIn(page)
    const { operationID } = await createAcceptedSubscription(page, 'same-payment-dispatch-' + randomUUID(), 'basic')
    const providerKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', operationID)
    const other = await page.context().newPage()
    try {
      await page.goto(app.baseURL + '/admin/payments/' + operationID + '/dispatch')
      await other.goto(app.baseURL + '/admin/payments/' + operationID + '/dispatch')
      await page.getByRole('button', { name: '建立預覽' }).click()
      await other.getByRole('button', { name: '建立預覽' }).click()
      await expect(other.getByRole('button', { name: '確認送出付款' })).toBeVisible()

      await page.getByRole('button', { name: '確認送出付款' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

      const staleResponse = other.waitForResponse((response) =>
        response.url().endsWith('/admin/api/commands') &&
        response.request().method() === 'POST' &&
        (response.request().postData() ?? '').includes('"action_id":"C09"'),
      )
      await other.getByRole('button', { name: '確認送出付款' }).last().click()
      await other.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
      expect((await staleResponse).status()).toBe(409)
      await expect(other.getByText('原預覽已失效', { exact: true })).toBeVisible()
      await expect(other.getByText('無法建立預覽')).toBeVisible()
      await expect(other.getByRole('button', { name: '確認送出付款' })).toHaveCount(0)
      await expect(other.getByText('操作狀態已變更，原預覽不可再送出')).toBeVisible()
      await expect(other.getByText('created → succeeded')).toBeVisible()
      await expect(other.getByText('pending → done')).toBeVisible()
      await expect(other.getByText('觀測時間')).toBeVisible()

      expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C09' AND target_id=? AND status='succeeded'", operationID)).toBe(1)
      expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C09' AND target_id=? AND status='failed' AND error_code='PREVIEW_STALE'", operationID)).toBe(1)
      expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C09' AND c.target_id=?", operationID)).toBe(1)
      expect(count('SELECT COUNT(*) FROM allocations WHERE operation_id=?', operationID)).toBe(1)
      expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath)).toBe('1')

      const invoiceID = scalar('SELECT invoice_id FROM payment_operations WHERE id=?', operationID)
      await page.goto(`${app.baseURL}/admin/payments?id_prefix=${encodeURIComponent(operationID)}`)
      await page.getByRole('button', { name: '開啟操作' }).click()
      await expect(page).toHaveURL(`${app.baseURL}/admin/payments/${operationID}`)
      await expect(page.getByRole('heading', { name: '付款操作詳情' })).toBeVisible()
      await expect(page.getByText('succeeded', { exact: true })).toBeVisible()
      await expect(page.getByRole('button', { name: '查證原操作' })).toBeVisible()
      await page.getByRole('button', { name: invoiceID }).click()
      await expect(page).toHaveURL(`${app.baseURL}/admin/invoices/${invoiceID}`)
    } finally {
      await other.close()
    }
  })

  test('dispatching a selected payment does not send an earlier queued operation', async ({ page }) => {
    await signIn(page)
    const first = await createAcceptedSubscription(page, `queued-payment-first-${randomUUID()}`, 'basic')
    const second = await createAcceptedSubscription(page, `queued-payment-second-${randomUUID()}`, 'basic')
    const firstProviderKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', first.operationID)
    const secondProviderKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', second.operationID)
    expect(count("SELECT COUNT(*) FROM outbox WHERE kind='capture' AND object_id IN (?,?) AND status='pending'", first.operationID, second.operationID)).toBe(2)

    await page.goto(`${app.baseURL}/admin/payments/${second.operationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    expect(scalar('SELECT status FROM payment_operations WHERE id=?', first.operationID)).toBe('created')
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', second.operationID)).toBe('succeeded')
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', firstProviderKey, app.providerPath)).toBe('0')
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', secondProviderKey, app.providerPath)).toBe('1')
    expect(count("SELECT COUNT(*) FROM outbox WHERE kind='capture' AND object_id=? AND status='pending'", first.operationID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C09' AND c.target_id=?", second.operationID)).toBe(1)
  })

  test('a retry payment shows a lower balance after another operator reduces the invoice', async ({ page }) => {
    await signIn(page)
    const { invoiceID, operationID } = await createAcceptedSubscription(page, `retry-stale-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/lab/payment-decisions/${operationID}`)
    await page.getByRole('combobox', { name: /結果/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('確定失敗', { exact: true }).click()
  await page.getByRole('button', { name: '確認付款結果' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '確認付款結果' }).click()
  await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
  await page.getByRole('button', { name: '執行另一個操作' }).click()
    await page.getByRole('combobox', { name: /結果/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('成功', { exact: true }).click()
    const rejectedDecisionPreview = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認付款結果' }).click()
    expect((await rejectedDecisionPreview).status()).toBe(409)
    await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(count('SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id=? AND c.target_id=?', 'C47', operationID)).toBe(1)
  await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('definitively_failed')

    await page.goto(`${app.baseURL}/admin/payments/${operationID}/retry`)
    await page.getByRole('button', { name: '預覽重試' }).click()
    await expect(page.getByRole('button', { name: '確認重試' })).toBeVisible()
    const beforeOperations = count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)
    await page.route('**/admin/api/commands', async (route) => {
      if (route.request().method() === 'POST' && (route.request().postData() ?? '').includes('"action_id":"C08"')) {
        await route.fulfill({ status: 422, contentType: 'application/json', body: '{"error":{"code":"INVALID_PAYLOAD","message":"Injected rejection"}}' })
      } else {
        await route.continue()
      }
    }, { times: 1 })
    await page.getByRole('button', { name: '確認重試' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '建立重試' }).click()
    await expect(page.getByText('命令未被接受，請檢查輸入')).toBeVisible()
    await expect(page.getByRole('button', { name: '預覽重試' })).toBeFocused()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C08' AND target_id=?", operationID)).toBe(0)
    expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(beforeOperations)
    await page.unroute('**/admin/api/commands')
    await page.getByRole('button', { name: '預覽重試' }).click()
    await expect(page.getByRole('button', { name: '確認重試' })).toBeVisible()
    const other = await page.context().newPage()
    try {
      await other.goto(`${app.baseURL}/admin/invoices/${invoiceID}/reductions/new`)
      await other.getByLabel('減額（最小貨幣單位）', { exact: true }).fill('500')
      await other.getByLabel('減額理由', { exact: true }).fill('retry preview source change')
      await other.getByRole('button', { name: '建立預覽' }).click()
      await other.getByRole('button', { name: '確認減額' }).last().click()
      await other.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

      const response = page.waitForResponse((value) => value.url().endsWith('/admin/api/commands') && value.request().method() === 'POST' && (value.request().postData() ?? '').includes('"action_id":"C08"'))
      await page.getByRole('button', { name: '確認重試' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '建立重試' }).click()
      expect((await response).status()).toBe(409)
      const stale = page.locator('.ant-alert').filter({ hasText: '原重試預覽已失效，請檢查帳單的最新狀態' })
      await expect(stale.getByText('原先：USD 20.00')).toBeVisible()
      await expect(stale.getByText('現在：USD 15.00')).toBeVisible()
      await expect(stale.locator('.ant-descriptions-item').filter({ hasText: '目前未清餘額' }).getByText('USD 15.00')).toBeVisible()
      await expect(page.getByRole('button', { name: '確認重試' })).toBeFocused()
      expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(beforeOperations)
      await other.goto(`${app.baseURL}/admin/payments/${operationID}/retry`)
      await other.getByRole('button', { name: '預覽重試' }).click()
      await expect(other.getByRole('button', { name: '確認重試' })).toBeVisible()
      await expect(page.getByRole('dialog')).toHaveCount(0)
      await page.getByRole('main').getByRole('button', { name: '確認重試' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '建立重試' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(beforeOperations + 1)
      expect(count("SELECT amount_minor FROM payment_operations WHERE invoice_id=? AND status='created'", invoiceID)).toBe(1500)
      const retryOperationID = scalar("SELECT id FROM payment_operations WHERE invoice_id=? AND status='created'", invoiceID)
      const secondResponse = other.waitForResponse((value) => value.url().endsWith('/admin/api/commands') && value.request().method() === 'POST' && (value.request().postData() ?? '').includes('"action_id":"C08"'))
      await other.getByRole('button', { name: '確認重試' }).click()
      await other.getByRole('dialog').getByRole('button', { name: '建立重試' }).click()
      expect((await secondResponse).status()).toBe(409)
      const otherStale = other.locator('.ant-alert').filter({ hasText: '原重試預覽已失效，請檢查帳單的最新狀態' })
      await expect(otherStale.locator('.ant-descriptions-item').filter({ hasText: '其他付款操作' }).getByText(new RegExp(retryOperationID))).toBeVisible()
      await expect(other.getByText('無法建立預覽')).toBeVisible()
      await expect(other.getByRole('button', { name: '確認重試' })).toHaveCount(0)
      await expect(other.locator('#stale-retry-warning')).toBeFocused()
      expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(beforeOperations + 1)
    } finally {
      await other.close()
    }
  })

  test('competing retry previews leave one obligation and dispatch its exact amount', async ({ page }) => {
    await signIn(page)
    const { invoiceID, operationID } = await createAcceptedSubscription(page, `competing-retry-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/lab/payment-decisions/${operationID}`)
    await page.getByRole('combobox', { name: /結果/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('確定失敗', { exact: true }).click()
    await page.getByRole('button', { name: '確認付款結果' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認付款結果' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('definitively_failed')

    await page.goto(`${app.baseURL}/admin/payments/${operationID}/retry`)
    await page.getByRole('button', { name: '預覽重試' }).click()
    await expect(page.getByRole('button', { name: '確認重試' })).toBeVisible()
    const other = await page.context().newPage()
    try {
      await other.goto(`${app.baseURL}/admin/payments/${operationID}/retry`)
      await other.getByRole('button', { name: '預覽重試' }).click()
      await expect(other.getByRole('button', { name: '確認重試' })).toBeVisible()
      expect(count('SELECT COUNT(*) FROM admin_previews WHERE action_id=? AND target_id=?', 'C08', operationID)).toBe(2)

      await other.getByRole('button', { name: '確認重試' }).click()
      await other.getByRole('dialog').getByRole('button', { name: '建立重試' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST' && (response.request().postData() ?? '').includes('"action_id":"C08"'))
      await page.getByRole('button', { name: '確認重試' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '建立重試' }).click()
      expect((await staleResponse).status()).toBe(409)
      await expect(page.locator('.ant-alert').filter({ hasText: '原重試預覽已失效' })).toBeVisible()

      expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID)).toBe(2)
      expect(count('SELECT COUNT(*) FROM payment_retry_requests WHERE invoice_id=?', invoiceID)).toBe(1)
      expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C08' AND target_id=? AND status='succeeded'", operationID)).toBe(1)
      expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C08' AND target_id=? AND status='failed' AND error_code='PREVIEW_STALE'", operationID)).toBe(1)
      expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C08' AND c.target_id=?", operationID)).toBe(1)
      const retryOperationID = scalar('SELECT id FROM payment_operations WHERE invoice_id=? ORDER BY rowid DESC LIMIT 1', invoiceID)
      const retryProviderKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', retryOperationID)
      expect(scalar('SELECT amount_minor FROM payment_operations WHERE id=?', retryOperationID)).toBe('2000')
      expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', retryProviderKey, app.providerPath)).toBe('0')

      await other.goto(`${app.baseURL}/admin/payments/${retryOperationID}/dispatch`)
      await other.getByRole('button', { name: '建立預覽' }).click()
      await other.getByRole('button', { name: '確認送出付款' }).last().click()
      await other.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(scalar('SELECT status FROM payment_operations WHERE id=?', retryOperationID)).toBe('succeeded')
      expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('definitively_failed')
      expect(scalar('SELECT amount_minor FROM captures WHERE provider_key=?', retryProviderKey, app.providerPath)).toBe('2000')
      expect(scalar('SELECT currency FROM captures WHERE provider_key=?', retryProviderKey, app.providerPath)).toBe('USD')
      expect(scalar('SELECT status FROM captures WHERE provider_key=?', retryProviderKey, app.providerPath)).toBe('succeeded')
      expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', retryProviderKey, app.providerPath)).toBe('1')
      expect(count('SELECT COUNT(*) FROM allocations WHERE invoice_id=?', invoiceID)).toBe(1)
      expect(count('SELECT amount_minor FROM allocations WHERE operation_id=?', retryOperationID)).toBe(2000)
      expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C09' AND c.target_id=?", retryOperationID)).toBe(1)
    } finally {
      await other.close()
    }
  })

  test('concurrent cancel and resume keep their original intents after source changes', async ({ page }) => {
    await signIn(page)
    const { subscriptionID } = await createPaidSubscription(page, `cancel-stale-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/subscriptions/${subscriptionID}/cancel`)
    await page.getByRole('button', { name: '預覽取消' }).click()
    await expect(page.getByRole('button', { name: '確認排程取消' })).toBeVisible()

    const other = await page.context().newPage()
    try {
      await other.goto(`${app.baseURL}/admin/subscriptions/${subscriptionID}/cancel`)
      await other.getByRole('button', { name: '預覽取消' }).click()
      await other.getByRole('button', { name: '確認排程取消' }).click()
      await other.getByRole('dialog').getByRole('button', { name: '確認排程' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      const response = page.waitForResponse((value) => value.url().endsWith('/admin/api/commands') && value.request().method() === 'POST')
      await page.getByRole('button', { name: '確認排程取消' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認排程' }).click()
      expect((await response).status()).toBe(409)
      const stale = page.locator('.ant-alert').filter({ hasText: '原取消預覽已失效，請檢查訂閱的最新狀態' })
      await expect(stale).toBeVisible()
      await expect(stale.locator('.ant-descriptions-item').filter({ hasText: '原操作意圖' }).getByText('排程取消訂閱')).toBeVisible()
      await expect(stale.locator('.ant-descriptions-item').filter({ hasText: '目前可預覽操作' }).getByText('恢復取消排程')).toBeVisible()
      await expect(stale.locator('.ant-descriptions-item').filter({ hasText: '來源 revision' }).getByText(/\d+ → \d+/)).toBeVisible()
      await expect(stale.locator('.ant-descriptions-item').filter({ hasText: '取消排程' }).getByText(/無 → /)).toBeVisible()
      await expect(page.getByRole('button', { name: '預覽恢復取消' })).toBeVisible()
      await expect(page.getByRole('button', { name: '確認恢復取消' })).toHaveCount(0)
      expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='cancel' AND status='scheduled'", subscriptionID)).toBe(1)

      await page.getByRole('button', { name: '預覽恢復取消' }).click()
      await expect(page.getByRole('button', { name: '確認恢復取消' })).toBeVisible()
      await other.goto(`${app.baseURL}/admin/subscriptions/${subscriptionID}/cancel`)
      await other.getByRole('button', { name: '依最新訂閱狀態繼續操作' }).click()
      await other.getByRole('button', { name: '預覽恢復取消' }).click()
      await other.getByRole('button', { name: '確認恢復取消' }).click()
      await other.getByRole('dialog').getByRole('button', { name: '確認恢復' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

      const resumeResponse = page.waitForResponse((value) => value.url().endsWith('/admin/api/commands') && value.request().method() === 'POST')
      await page.getByRole('button', { name: '確認恢復取消' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認恢復' }).click()
      expect((await resumeResponse).status()).toBe(409)
      await expect(stale.locator('.ant-descriptions-item').filter({ hasText: '原操作意圖' }).getByText('恢復取消排程')).toBeVisible()
      await expect(stale.locator('.ant-descriptions-item').filter({ hasText: '目前可預覽操作' }).getByText('排程取消訂閱')).toBeVisible()
      await expect(stale.locator('.ant-descriptions-item').filter({ hasText: '取消排程' }).getByText(/ → 無/)).toBeVisible()
      await expect(page.getByRole('button', { name: '預覽取消' })).toBeVisible()
      await expect(page.getByRole('button', { name: '確認排程取消' })).toHaveCount(0)
      expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='cancel' AND status='scheduled'", subscriptionID)).toBe(0)
    } finally {
      await other.close()
    }
  })

  test('a different seat price quote cannot use the original plan binding fingerprint', async ({ page }) => {
    await signIn(page)
    const customerID = `plan-binding-${randomUUID()}`
    const { subscriptionID } = await createPaidSubscription(page, customerID, 'basic')
    const revision = scalar('SELECT revision FROM subscriptions WHERE id=?', subscriptionID)

    async function createBoundQuote(seats: string) {
      await page.goto(`${app.baseURL}/admin/quotes/new`)
      await page.getByRole('button', { name: '建立另一筆報價' }).click()
      await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
      await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
      await page.getByRole('textbox', { name: /席次/ }).fill(seats)
      await page.getByRole('checkbox', { name: '這是現有訂閱的變更報價' }).check()
      await page.getByRole('textbox', { name: /訂閱 ID/ }).fill(subscriptionID)
      await page.getByRole('combobox', { name: /變更方式/ }).click()
      await page.locator('.ant-select-dropdown:visible').getByText('下期變更', { exact: true }).click()
      await page.getByRole('textbox', { name: /目前 Revision/ }).fill(revision)
      await page.getByRole('button', { name: '建立報價' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      return scalar('SELECT q.id FROM quotes q JOIN change_quote_bindings b ON b.quote_id=q.id WHERE b.subscription_id=? ORDER BY q.rowid DESC LIMIT 1', subscriptionID)
    }

    const fiveSeatQuoteID = await createBoundQuote('5')
    const sevenSeatQuoteID = await createBoundQuote('7')
    expect(scalar('SELECT seat_quantity FROM quotes WHERE id=?', fiveSeatQuoteID)).toBe('5')
    expect(scalar('SELECT seat_quantity FROM quotes WHERE id=?', sevenSeatQuoteID)).toBe('7')
    expect(scalar('SELECT amount_minor FROM quotes WHERE id=?', fiveSeatQuoteID)).toBe('10000')
    expect(scalar('SELECT amount_minor FROM quotes WHERE id=?', sevenSeatQuoteID)).toBe('12000')
    await page.goto(`${app.baseURL}/admin/quotes/${fiveSeatQuoteID}/accept`)
    await page.getByRole('button', { name: '前往下期變更' }).click()
    const quoteField = page.getByRole('textbox', { name: '已綁定的報價 ID' })
    await expect(quoteField).toHaveValue(fiveSeatQuoteID)
    const originalFingerprint = await page.getByRole('textbox', { name: '變更綁定 Fingerprint' }).inputValue()
    await quoteField.fill(sevenSeatQuoteID)

    const previewResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '預覽下期變更' }).click()
    const response = await previewResponse
    expect(response.status()).toBe(409)
    expect((await response.json()).error.code).toBe('CHANGE_QUOTE_BINDING_MISMATCH')
    await expect(page.getByText('報價與綁定資料不一致')).toBeVisible()
    await expect(page.getByText(/報價 ID、綁定 Fingerprint 或訂閱不相符/)).toBeVisible()
    await expect(quoteField).toHaveValue(sevenSeatQuoteID)
    await expect(page.getByRole('textbox', { name: '變更綁定 Fingerprint' })).toHaveValue(originalFingerprint)
    expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='change'", subscriptionID)).toBe(0)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C03' AND target_id=?", subscriptionID)).toBe(0)

    await quoteField.fill(fiveSeatQuoteID)
    await expect(page.getByText('報價與綁定資料不一致', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '確認排程', exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: '預覽下期變更' }).click()
    await expect(page.getByText('變更預覽', { exact: true })).toBeVisible()
    await expect(page.getByRole('cell', { name: 'USD 100.00', exact: true })).toBeVisible()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C03' AND target_id=?", subscriptionID)).toBe(0)
  })

  test('a stale subscription revision rolls back a new change quote and binding', async ({ page }) => {
    await signIn(page)
    const customerID = `change-quote-atomic-${randomUUID()}`
    const { subscriptionID } = await createPaidSubscription(page, customerID, 'basic')
    const revision = BigInt(scalar('SELECT revision FROM subscriptions WHERE id=?', subscriptionID))
    const quotesBefore = count('SELECT COUNT(*) FROM quotes WHERE customer_id=?', customerID)

    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('button', { name: '建立另一筆報價' }).click()
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
    await page.getByRole('textbox', { name: /席次/ }).fill('5')
    await page.getByRole('checkbox', { name: '這是現有訂閱的變更報價' }).check()
    await page.getByRole('textbox', { name: /訂閱 ID/ }).fill(subscriptionID)
    await page.getByRole('combobox', { name: /變更方式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('下期變更', { exact: true }).click()
    await page.getByRole('textbox', { name: /目前 Revision/ }).fill((revision + 1n).toString())
    await page.getByRole('button', { name: '建立報價' }).click()

    await expect(page.getByRole('main').getByText('failed', { exact: true }).first()).toBeVisible()
    const commandID = scalar('SELECT id FROM admin_commands WHERE action_id=? ORDER BY rowid DESC LIMIT 1', 'C01')
    expect(scalar('SELECT status FROM admin_commands WHERE id=?', commandID)).toBe('failed')
    expect(scalar('SELECT error_code FROM admin_commands WHERE id=?', commandID)).toBe('CHANGE_QUOTE_REVISION_CHANGED')
    expect(count('SELECT COUNT(*) FROM quotes WHERE customer_id=?', customerID)).toBe(quotesBefore)
    expect(count('SELECT COUNT(*) FROM change_quote_bindings WHERE subscription_id=?', subscriptionID)).toBe(0)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(0)
    await expect(page.getByRole('row', { name: /報價 ID/ })).toContainText('尚未產生')
    await expect(page.getByRole('row', { name: /錯誤/ })).toContainText('訂閱 Revision 已改變')
  })

  test('lost scheduled plan response recovers its original command and one schedule', async ({ page }) => {
    await signIn(page)
    const customerID = `plan-recovery-${randomUUID()}`
    const { subscriptionID } = await createPaidSubscription(page, customerID, 'basic')
    const revision = scalar('SELECT revision FROM subscriptions WHERE id=?', subscriptionID)

    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('button', { name: '建立另一筆報價' }).click()
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
    await page.getByRole('textbox', { name: /席次/ }).fill('5')
    await page.getByRole('checkbox', { name: '這是現有訂閱的變更報價' }).check()
    await page.getByRole('textbox', { name: /訂閱 ID/ }).fill(subscriptionID)
    await page.getByRole('combobox', { name: /變更方式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('下期變更', { exact: true }).click()
    await page.getByRole('textbox', { name: /目前 Revision/ }).fill(revision)
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT quote_id FROM change_quote_bindings WHERE subscription_id=?', subscriptionID)

    await page.getByRole('button', { name: '前往排程下期變更' }).click()
    await page.getByRole('button', { name: '預覽下期變更' }).click()
    await expect(page.getByRole('button', { name: '確認排程' })).toBeVisible()

    let originalKey = ''
    let originalBody = ''
    let dropped = false
    await page.route('**/admin/api/commands', async (route) => {
      if (!dropped && route.request().method() === 'POST') {
        dropped = true
        originalKey = route.request().headers()['idempotency-key']
        originalBody = route.request().postData() ?? ''
        const committed = await commitThenDropResponse(page, route)
        expect(committed.status()).toBe(202)
      } else {
        await route.continue()
      }
    })
    await page.getByRole('button', { name: '確認排程' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認排程' }).click()
    await expect(page.getByText('原排程命令的結果尚未確認')).toBeVisible()
    expect(originalKey).toBeTruthy()
    expect(originalBody).toBeTruthy()
    const commandID = scalar("SELECT id FROM admin_commands WHERE action_id='C03' AND target_id=?", subscriptionID)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C03' AND target_id=?", subscriptionID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='change' AND status='scheduled'", subscriptionID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(scalar("SELECT target_price_version_id FROM subscription_schedules WHERE subscription_id=? AND kind='change'", subscriptionID)).toBe(scalar('SELECT price_version_id FROM quotes WHERE id=?', quoteID))
    expect(scalar("SELECT seat_quantity FROM subscription_schedules WHERE subscription_id=? AND kind='change'", subscriptionID)).toBe('5')

    await page.reload()
    await expect(page.getByText('原排程命令的結果尚未確認')).toBeVisible()
    const replay = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST')
    await page.getByRole('button', { name: '用原 request key 查詢' }).click()
    expect((await replay).headers()['idempotency-key']).toBe(originalKey)
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C03' AND target_id=?", subscriptionID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='change'", subscriptionID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)

    const session = await (await page.request.get(`${app.baseURL}/admin/api/session`)).json() as { csrf_token: string }
    const changed = JSON.parse(originalBody) as { payload: { quote_id: string } }
    changed.payload.quote_id = 'different-quote'
    const conflict = await page.request.post(`${app.baseURL}/admin/api/commands`, {
      headers: { Origin: app.baseURL, 'Idempotency-Key': originalKey, 'X-CSRF-Token': session.csrf_token },
      data: changed,
    })
    expect(conflict.status()).toBe(409)
    expect((await conflict.json()).error.code).toBe('IDEMPOTENCY_CONFLICT')
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C03' AND target_id=?", subscriptionID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='change'", subscriptionID)).toBe(1)
  })

  test('lost immediate upgrade response recovers one obligation without switching service early', async ({ page }) => {
    await signIn(page)
    const customerID = `upgrade-recovery-${randomUUID()}`
    const { subscriptionID } = await createPaidSubscription(page, customerID, 'basic')
    const revision = scalar('SELECT revision FROM subscriptions WHERE id=?', subscriptionID)

    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('button', { name: '建立另一筆報價' }).click()
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
    await page.getByRole('textbox', { name: /席次/ }).fill('5')
    await page.getByRole('checkbox', { name: '這是現有訂閱的變更報價' }).check()
    await page.getByRole('textbox', { name: /訂閱 ID/ }).fill(subscriptionID)
    await page.getByRole('combobox', { name: /變更方式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('立即升級', { exact: true }).click()
    await page.getByRole('textbox', { name: /目前 Revision/ }).fill(revision)
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.getByRole('button', { name: '前往立即升級' }).click()
    await page.getByRole('button', { name: '預覽立即升級' }).click()
    await expect(page.getByRole('button', { name: '確認升級' })).toBeVisible()

    let originalKey = ''
    let originalBody = ''
    let dropped = false
    await page.route('**/admin/api/commands', async (route) => {
      if (!dropped && route.request().method() === 'POST') {
        dropped = true
        originalKey = route.request().headers()['idempotency-key']
        originalBody = route.request().postData() ?? ''
        const committed = await commitThenDropResponse(page, route)
        expect(committed.status()).toBe(202)
      } else {
        await route.continue()
      }
    })
    await page.getByRole('button', { name: '確認升級' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認升級' }).click()
    await expect(page.getByText('原升級命令的結果尚未確認')).toBeVisible()
    expect(originalKey).toBeTruthy()
    expect(originalBody).toBeTruthy()
    const commandID = scalar("SELECT id FROM admin_commands WHERE action_id='C04' AND target_id=?", subscriptionID)
    const changeID = scalar('SELECT id FROM immediate_changes WHERE subscription_id=?', subscriptionID)
    const invoiceID = scalar('SELECT invoice_id FROM immediate_changes WHERE id=?', changeID)
    const operationID = scalar('SELECT operation_id FROM immediate_changes WHERE id=?', changeID)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C04' AND target_id=?", subscriptionID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?', subscriptionID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM invoices WHERE id=?', invoiceID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM payment_operations WHERE id=?', operationID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(scalar('SELECT price_version_id FROM subscriptions WHERE id=?', subscriptionID)).toBe('basic-v1')

    await page.reload()
    await expect(page.getByText('原升級命令的結果尚未確認')).toBeVisible()
    const replay = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST')
    await page.getByRole('button', { name: '用原 request key 查詢' }).click()
    expect((await replay).headers()['idempotency-key']).toBe(originalKey)
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C04' AND target_id=?", subscriptionID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?', subscriptionID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM invoices WHERE id=?', invoiceID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM payment_operations WHERE id=?', operationID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(scalar('SELECT price_version_id FROM subscriptions WHERE id=?', subscriptionID)).toBe('basic-v1')

    const session = await (await page.request.get(`${app.baseURL}/admin/api/session`)).json() as { csrf_token: string }
    const changed = JSON.parse(originalBody) as { payload: { quote_id: string } }
    changed.payload.quote_id = 'different-quote'
    const conflict = await page.request.post(`${app.baseURL}/admin/api/commands`, {
      headers: { Origin: app.baseURL, 'Idempotency-Key': originalKey, 'X-CSRF-Token': session.csrf_token },
      data: changed,
    })
    expect(conflict.status()).toBe(409)
    expect((await conflict.json()).error.code).toBe('IDEMPOTENCY_CONFLICT')
    expect(count('SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?', subscriptionID)).toBe(1)
  })

  test('a newly selected Pro price rejects the old bound quote before scheduling', async ({ page }) => {
    await signIn(page)
    const customerID = `superseded-plan-price-${randomUUID()}`
    const { subscriptionID } = await createPaidSubscription(page, customerID, 'basic')
    const revision = scalar('SELECT revision FROM subscriptions WHERE id=?', subscriptionID)

    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('button', { name: '建立另一筆報價' }).click()
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
    await page.getByRole('textbox', { name: /席次/ }).fill('5')
    await page.getByRole('checkbox', { name: '這是現有訂閱的變更報價' }).check()
    await page.getByRole('textbox', { name: /訂閱 ID/ }).fill(subscriptionID)
    await page.getByRole('combobox', { name: /變更方式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('下期變更', { exact: true }).click()
    await page.getByRole('textbox', { name: /目前 Revision/ }).fill(revision)
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT quote_id FROM change_quote_bindings WHERE subscription_id=?', subscriptionID)
    expect(scalar('SELECT amount_minor FROM quotes WHERE id=?', quoteID)).toBe('10000')

    const priceID = `superseding_pro_${randomUUID().replaceAll('-', '')}`
    const version = String(count("SELECT COALESCE(MAX(version), 0) + 1 FROM price_versions WHERE plan_id='pro'"))
    const periodStart = BigInt(scalar('SELECT period_start FROM billing_periods WHERE subscription_id=?', subscriptionID))
    const effective = new Date(Number((periodStart + 60n * 1_000_000_000n) / 1_000_000n)).toISOString()
    await page.goto(`${app.baseURL}/admin/prices/pro/new`)
    for (const [label, value] of [
      ['價格版本 ID', priceID], ['版本號', version],
      ['固定金額（最小單位）', '7000'], ['每席金額（最小單位）', '1000'],
      ['包含任務量', '100'], ['超額費率分子', '1'],
      ['超額費率分母', '1'], ['生效起點（UTC）', effective],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認發布價格' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認發布價格' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await page.goto(`${app.baseURL}/admin/catalog-selections/new`)
    for (const [label, value] of [
      ['方案 ID', 'pro'], ['Cohort', 'default'],
      ['生效時間（UTC）', effective], ['價格版本 ID', priceID],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認選價' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認選價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const session = await (await page.request.get(`${app.baseURL}/admin/api/session`)).json() as { csrf_token: string }
    try {
      await submitClockControl(page, app.baseURL, session.csrf_token, 'fixed', new Date(Date.parse(effective) + 1000).toISOString())
      await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
      await page.getByRole('button', { name: '前往下期變更' }).click()
      const response = page.waitForResponse((value) => value.url().endsWith('/admin/api/previews') && value.request().method() === 'POST')
      await page.getByRole('button', { name: '預覽下期變更' }).click()
      const previewResponse = await response
      expect(previewResponse.status()).toBe(409)
      expect((await previewResponse.json()).error.code).toBe('CHANGE_QUOTE_PRICE_SUPERSEDED')
      await expect(page.getByText('報價價格版本已被取代', { exact: true })).toBeVisible()
      await expect(page.getByText(/請重新建立變更報價，再確認新金額/)).toBeVisible()
      expect(count("SELECT COUNT(*) FROM admin_previews WHERE action_id='C03' AND target_id=?", subscriptionID)).toBe(0)
      expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C03' AND target_id=?", subscriptionID)).toBe(0)
      expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='change'", subscriptionID)).toBe(0)
      expect(scalar('SELECT price_version_id FROM subscriptions WHERE id=?', subscriptionID)).toBe('basic-v1')
    } finally {
      // The serial suite shares this SQLite fixture. Restore the default Pro
      // selection after the supersession assertion so later C03 previews see
      // the same catalog they started with.
      const restoreAt = BigInt(Date.parse(effective) + 2_000) * 1_000_000n
      const restoreSelection = `import sqlite3,sys
db = sqlite3.connect(sys.argv[1])
db.execute("INSERT INTO catalog_selection(plan_id,cohort,effective_at,price_version_id) VALUES('pro','default',?,'pro-v1')", (int(sys.argv[2]),))
db.commit()`
      execFileSync('python3', ['-c', restoreSelection, app.commercePath, String(restoreAt)])
      await submitClockControl(page, app.baseURL, session.csrf_token, 'real')
    }
  })

  test('a concurrent subscription change preserves the original plan binding intent', async ({ page }) => {
    await signIn(page)
    const customerID = `plan-stale-${randomUUID()}`
    const { subscriptionID } = await createPaidSubscription(page, customerID, 'basic')
    const revision = scalar('SELECT revision FROM subscriptions WHERE id=?', subscriptionID)
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('button', { name: '建立另一筆報價' }).click()
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
    await page.getByRole('textbox', { name: /席次/ }).fill('5')
    await page.getByRole('checkbox', { name: '這是現有訂閱的變更報價' }).check()
    await page.getByRole('textbox', { name: /訂閱 ID/ }).fill(subscriptionID)
    await page.getByRole('combobox', { name: /變更方式/ }).click()
    await page.getByText('下期變更', { exact: true }).click()
    await page.getByRole('textbox', { name: /目前 Revision/ }).fill(revision)
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const boundQuoteID = scalar("SELECT quote_id FROM change_quote_bindings WHERE subscription_id=? AND mode='next_period'", subscriptionID)
    await page.goto(`${app.baseURL}/admin/quotes/${boundQuoteID}/accept`)
    await expect(page.getByRole('button', { name: '預覽接受' })).toHaveCount(0)
    await expect(page.getByRole('row', { name: /報價接受時現在應付/ })).toContainText('USD 0.00')
    await expect(page.getByRole('row', { name: /下一整期固定承諾/ })).toContainText('USD 100.00')
    await page.getByRole('button', { name: '前往下期變更' }).click()
    const quoteID = await page.getByRole('textbox', { name: '已綁定的報價 ID' }).inputValue()
    const fingerprint = await page.getByRole('textbox', { name: '變更綁定 Fingerprint' }).inputValue()
    await page.getByRole('button', { name: '預覽下期變更' }).click()
    await expect(page.getByRole('button', { name: '確認排程' })).toBeVisible()

    const other = await page.context().newPage()
    try {
      await other.goto(`${app.baseURL}/admin/subscriptions/${subscriptionID}/cancel`)
      await other.getByRole('button', { name: '預覽取消' }).click()
      await other.getByRole('button', { name: '確認排程取消' }).click()
      await other.getByRole('dialog').getByRole('button', { name: '確認排程' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

      const response = page.waitForResponse((value) => value.url().endsWith('/admin/api/commands') && value.request().method() === 'POST')
      await page.getByRole('button', { name: '確認排程' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認排程' }).click()
      expect((await response).status()).toBe(409)
      const stale = page.locator('.ant-alert').filter({ hasText: '原方案變更預覽已失效，請檢查最新來源' })
      await expect(stale).toBeVisible()
      await expect(stale.getByText('訂閱 revision 已改變，請建立對應新 revision 的變更報價與綁定。')).toBeVisible()
      await expect(stale.locator('.ant-descriptions-item').filter({ hasText: '訂閱 revision' }).getByText(/\d+ → \d+/)).toBeVisible()
      await expect(page.getByRole('textbox', { name: '已綁定的報價 ID' })).toHaveValue(quoteID)
      await expect(page.getByRole('textbox', { name: '變更綁定 Fingerprint' })).toHaveValue(fingerprint)
      await expect(page.getByRole('button', { name: '確認排程' })).toHaveCount(0)
      expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='change'", subscriptionID)).toBe(0)
      expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='cancel' AND status='scheduled'", subscriptionID)).toBe(1)
      const stalePreview = page.waitForResponse((value) => value.url().endsWith('/admin/api/previews') && value.request().method() === 'POST')
      await page.getByRole('button', { name: '預覽下期變更' }).click()
      const stalePreviewResponse = await stalePreview
      expect(stalePreviewResponse.status()).toBe(409)
      expect((await stalePreviewResponse.json()).error.code).toBe('CHANGE_QUOTE_REVISION_CHANGED')
      await expect(page.getByText('訂閱 Revision 已改變', { exact: true })).toBeVisible()
      expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='change'", subscriptionID)).toBe(0)
    } finally {
      await other.close()
    }
  })

  test('an immediate upgrade refuses a preview after another operator changes the subscription', async ({ page }) => {
    await signIn(page)
    const customerID = `upgrade-source-stale-${randomUUID()}`
    const { subscriptionID } = await createPaidSubscription(page, customerID, 'basic')
    const revision = scalar('SELECT revision FROM subscriptions WHERE id=?', subscriptionID)
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('button', { name: '建立另一筆報價' }).click()
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
    await page.getByRole('textbox', { name: /席次/ }).fill('5')
    await page.getByRole('checkbox', { name: '這是現有訂閱的變更報價' }).check()
    await page.getByRole('textbox', { name: /訂閱 ID/ }).fill(subscriptionID)
    await page.getByRole('combobox', { name: /變更方式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('立即升級', { exact: true }).click()
    await page.getByRole('textbox', { name: /目前 Revision/ }).fill(revision)
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.getByRole('button', { name: '前往立即升級' }).click()
    const quoteID = await page.getByRole('textbox', { name: '已綁定的報價 ID' }).inputValue()
    const fingerprint = await page.getByRole('textbox', { name: '變更綁定 Fingerprint' }).inputValue()
    await page.getByRole('button', { name: '預覽立即升級' }).click()
    await expect(page.getByRole('button', { name: '確認升級' })).toBeVisible()
    await page.getByRole('main').getByRole('button', { name: '確認升級' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '返回檢查' }).click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(page.getByRole('main').getByRole('button', { name: '確認升級' })).toBeFocused()

    const other = await page.context().newPage()
    try {
      await other.goto(`${app.baseURL}/admin/subscriptions/${subscriptionID}/cancel`)
      await other.getByRole('button', { name: '預覽取消' }).click()
      await other.getByRole('button', { name: '確認排程取消' }).click()
      await other.getByRole('dialog').getByRole('button', { name: '確認排程' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

      const response = page.waitForResponse((value) => value.url().endsWith('/admin/api/commands') && value.request().method() === 'POST' && (value.request().postData() ?? '').includes('"action_id":"C04"'))
      await page.getByRole('button', { name: '確認升級' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認升級' }).click()
      expect((await response).status()).toBe(409)
      const stale = page.locator('.ant-alert').filter({ hasText: '原方案變更預覽已失效，請檢查最新來源' })
      await expect(stale).toBeVisible()
      await expect(stale.getByText('訂閱 revision 已改變，請建立對應新 revision 的變更報價與綁定。')).toBeVisible()
      await expect(page.getByRole('textbox', { name: '已綁定的報價 ID' })).toHaveValue(quoteID)
      await expect(page.getByRole('textbox', { name: '變更綁定 Fingerprint' })).toHaveValue(fingerprint)
      await expect(page.getByRole('button', { name: '確認升級' })).toHaveCount(0)
      await expect(page.getByRole('button', { name: '預覽立即升級' })).toBeFocused()
      expect(count('SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?', subscriptionID)).toBe(0)
      expect(count('SELECT COUNT(*) FROM supplemental_invoices WHERE subscription_id=?', subscriptionID)).toBe(0)
      expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C04' AND c.target_id=?", subscriptionID)).toBe(0)
      const stalePreview = page.waitForResponse((value) => value.url().endsWith('/admin/api/previews') && value.request().method() === 'POST')
      await page.getByRole('button', { name: '預覽立即升級' }).click()
      const stalePreviewResponse = await stalePreview
      expect(stalePreviewResponse.status()).toBe(409)
      expect((await stalePreviewResponse.json()).error.code).toBe('CHANGE_QUOTE_REVISION_CHANGED')
      await expect(page.getByText('訂閱 Revision 已改變', { exact: true })).toBeVisible()
      expect(count('SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?', subscriptionID)).toBe(0)
    } finally {
      await other.close()
    }
  })

  test('an immediate upgrade shows lower and higher amounts after business clock changes', async ({ page }) => {
    await signIn(page)
    const customerID = `upgrade-stale-${randomUUID()}`
    const { subscriptionID } = await createPaidSubscription(page, customerID, 'basic')
    const start = BigInt(scalar('SELECT period_start FROM billing_periods WHERE subscription_id=?', subscriptionID))
    const end = BigInt(scalar('SELECT period_end FROM billing_periods WHERE subscription_id=?', subscriptionID))
    const midpoint = new Date(Number(((start + end) / 2n) / 1_000_000n)).toISOString()
    const later = new Date(Date.parse(midpoint) + 10 * 60 * 1000).toISOString()
    const earlier = new Date(Date.parse(midpoint) + 60 * 1000).toISOString()
    const sessionResponse = await page.request.get(`${app.baseURL}/admin/api/session`)
    const session = await sessionResponse.json() as { csrf_token: string }
    async function setClock(mode: 'fixed' | 'real', value?: string) {
      await submitClockControl(page, app.baseURL, session.csrf_token, mode, value)
    }
    try {
      await setClock('fixed', midpoint)
      const revision = scalar('SELECT revision FROM subscriptions WHERE id=?', subscriptionID)
      await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('button', { name: '建立另一筆報價' }).click()
      await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
      await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
      await page.getByRole('textbox', { name: /席次/ }).fill('5')
      await page.getByRole('checkbox', { name: '這是現有訂閱的變更報價' }).check()
      await page.getByRole('textbox', { name: /訂閱 ID/ }).fill(subscriptionID)
      await page.getByRole('combobox', { name: /變更方式/ }).click()
      await page.locator('.ant-select-dropdown:visible').getByText('立即升級', { exact: true }).click()
      await page.getByRole('textbox', { name: /目前 Revision/ }).fill(revision)
      await page.getByRole('button', { name: '建立報價' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      await page.getByRole('button', { name: '前往立即升級' }).click()
      await page.getByRole('button', { name: '預覽立即升級' }).click()
      await expect(page.getByText('USD 40.00')).toBeVisible()

      await setClock('fixed', later)
      const response = page.waitForResponse((value) => value.url().endsWith('/admin/api/commands') && value.request().method() === 'POST' && (value.request().postData() ?? '').includes('"action_id":"C04"'))
      await page.getByRole('button', { name: '確認升級' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認升級' }).click()
      const lowerRejected = await response
      expect(lowerRejected.status()).toBe(409)
      expect((await lowerRejected.json()).error.code).toBe('PREVIEW_STALE')
      const stale = page.locator('.ant-alert').filter({ hasText: '原方案變更預覽已失效，請檢查最新來源' })
      await expect(stale.getByText('原先：USD 40.00')).toBeVisible()
      await expect(stale.getByText(/現在：USD 39\.\d{2}/)).toBeVisible()
      await expect(page.getByRole('main').getByRole('button', { name: '確認升級' })).toBeVisible()
      await expect(page.getByRole('dialog')).toHaveCount(0)
      await expect(page.getByRole('main').getByRole('button', { name: '確認升級' })).toBeFocused()
      expect(count('SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?', subscriptionID)).toBe(0)

      await setClock('fixed', earlier)
      const higherResponse = page.waitForResponse((value) => value.url().endsWith('/admin/api/commands') && value.request().method() === 'POST' && (value.request().postData() ?? '').includes('"action_id":"C04"'))
      await page.getByRole('main').getByRole('button', { name: '確認升級' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認升級' }).click()
      const higherRejected = await higherResponse
      expect(higherRejected.status()).toBe(409)
      expect((await higherRejected.json()).error.code).toBe('PREVIEW_STALE')
      await expect(stale.getByText(/原先：USD 39\.\d{2}/)).toBeVisible()
      await expect(stale.getByText('現在：USD 40.00')).toBeVisible()
      await expect(page.getByRole('dialog')).toHaveCount(0)
      expect(count('SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?', subscriptionID)).toBe(0)

      await page.getByRole('main').getByRole('button', { name: '確認升級' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認升級' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(count('SELECT COUNT(*) FROM immediate_changes WHERE subscription_id=?', subscriptionID)).toBe(1)
      expect(count('SELECT quoted_amount_minor FROM immediate_changes WHERE subscription_id=?', subscriptionID)).toBe(4000)
      const supplementInvoiceID = scalar('SELECT invoice_id FROM immediate_changes WHERE subscription_id=?', subscriptionID)
      await page.goto(`${app.baseURL}/admin/invoices?id_prefix=${encodeURIComponent(supplementInvoiceID)}`)
      await expect(page.getByRole('columnheader', { name: 'PeriodIndex' })).toBeVisible()
      const supplementRow = page.getByRole('row').filter({ hasText: supplementInvoiceID })
      await expect(supplementRow.getByRole('cell').nth(2)).toHaveText('未知')
    } finally {
      await setClock('real')
    }
  })

  test('an expired quote stays immutable and offers a new quote for the same customer', async ({ page }) => {
    await signIn(page)
    const customerID = `expired-quote-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=? ORDER BY rowid DESC LIMIT 1', customerID)
    const expiry = BigInt(scalar('SELECT expires_at FROM quotes WHERE id=?', quoteID))
    const expiredAt = new Date(Number(expiry / 1_000_000n) + 1000).toISOString()
    const session = await (await page.request.get(`${app.baseURL}/admin/api/session`)).json() as { csrf_token: string }
    async function setClock(mode: 'fixed' | 'real', value?: string) {
      await submitClockControl(page, app.baseURL, session.csrf_token, mode, value)
    }

    try {
      await setClock('fixed', expiredAt)
      await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
      await expect(page.getByRole('row', { name: /報價金額/ })).toContainText('USD 20.00')
      const previewResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
      await page.getByRole('button', { name: '預覽接受' }).click()
      const response = await previewResponse
      expect(response.status()).toBe(409)
      expect((await response.json()).error.code).toBe('QUOTE_EXPIRED')
      await expect(page.getByText('報價已過期')).toBeVisible()
      await expect(page.getByRole('button', { name: '建立新報價' })).toBeVisible()
      expect(count('SELECT COUNT(*) FROM subscriptions WHERE quote_id=?', quoteID)).toBe(0)
      expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C02' AND target_id=?", quoteID)).toBe(0)
      await page.getByRole('button', { name: '建立新報價' }).click()
      await expect(page.getByRole('textbox', { name: /客戶 ID/ })).toHaveValue(customerID)
    } finally {
      await setClock('real')
    }
  })

  test('a concurrent quote acceptance shows its latest state without a second obligation', async ({ page }) => {
    await signIn(page)
    const customerID = `quote-stale-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=? ORDER BY rowid DESC LIMIT 1', customerID)
    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await page.getByRole('button', { name: '預覽接受' }).click()
    await expect(page.getByRole('button', { name: '確認接受並建立付款義務' })).toBeVisible()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '返回檢查' }).click()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '確認接受並建立付款義務' })).toBeFocused()

    const other = await page.context().newPage()
    try {
      await other.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
      await other.getByRole('button', { name: '預覽接受' }).click()
      await other.getByRole('button', { name: '確認接受並建立付款義務' }).click()
      await other.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

      const response = page.waitForResponse((value) => value.url().endsWith('/admin/api/commands') && value.request().method() === 'POST')
      await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
      expect((await response).status()).toBe(409)
      const stale = page.locator('.ant-alert').filter({ hasText: '原接受預覽已失效，請檢查報價的最新狀態' })
      await expect(stale).toBeVisible()
      await expect(page.locator('#stale-quote-acceptance')).toBeFocused()
      await expect(stale.locator('.ant-descriptions-item').filter({ hasText: '接受狀態' }).getByText('尚未接受 → 已接受')).toBeVisible()
      await expect(page.getByRole('button', { name: '預覽接受' })).toHaveCount(0)
      expect(count('SELECT COUNT(*) FROM subscriptions WHERE quote_id=?', quoteID)).toBe(1)
      expect(count('SELECT COUNT(*) FROM invoices WHERE subscription_id=(SELECT id FROM subscriptions WHERE quote_id=?)', quoteID)).toBe(1)
    } finally {
      await other.close()
    }
  })

  test('rejected quote acceptance returns keyboard focus to preview and permits a new intent', async ({ page }) => {
    await signIn(page)
    const customerID = `quote-rejected-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=?', customerID)
    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await page.getByRole('button', { name: '預覽接受' }).click()
    await expect(page.getByRole('button', { name: '確認接受並建立付款義務' })).toBeVisible()

    await page.route('**/admin/api/commands', async (route) => {
      if (route.request().method() === 'POST' && (route.request().postData() ?? '').includes('"action_id":"C02"')) {
        await route.fulfill({ status: 422, contentType: 'application/json', body: '{"error":{"code":"INVALID_PAYLOAD","message":"Injected rejection"}}' })
      } else {
        await route.continue()
      }
    }, { times: 1 })
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(page.getByText('命令未被接受，請檢查輸入')).toBeVisible()
    await expect(page.getByRole('button', { name: '預覽接受' })).toBeFocused()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C02' AND target_id=?", quoteID)).toBe(0)

    await page.unroute('**/admin/api/commands')
    await page.getByRole('button', { name: '預覽接受' }).click()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C02' AND target_id=?", quoteID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM subscriptions WHERE quote_id=?', quoteID)).toBe(1)
  })

  test('an uncertain usage response cannot become a second event intent', async ({ page }) => {
    await signIn(page)
    const { subscriptionID } = await createPaidSubscription(page, `usage-uncertain-${randomUUID()}`, 'pro', '5')
    const eventID = `usage-uncertain-${randomUUID()}`
    const before = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C26')
    await page.goto(`${app.baseURL}/admin/usage-events/new`)
    for (const [label, value] of [
      ['訂閱 ID', subscriptionID], ['Meter ID', 'tasks'], ['來源', 'browser'],
      ['事件 ID', eventID], ['發生時間（UTC）', new Date().toISOString()], ['數量', '7'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    let dropped = false
    await page.route('**/admin/api/commands', async (route) => {
      if (!dropped && route.request().method() === 'POST') {
        dropped = true
        await commitThenDropResponse(page, route)
      } else {
        await route.continue()
      }
    })
    await page.getByRole('button', { name: '檢查並記錄' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認記錄' }).click()
    await expect(page.getByText('原事件的命令結果尚未確認')).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C26')).toBe(before + 1)
    await page.reload()
    await expect(page.getByText('原事件的命令結果尚未確認')).toBeVisible()
    await expect(page.getByRole('textbox', { name: '事件 ID' })).toBeDisabled()
    await expect(page.getByRole('button', { name: '檢查並記錄' })).toBeDisabled()
    await page.getByRole('button', { name: '用原 request key 查詢' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM usage_events WHERE event_id=?', eventID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C26')).toBe(before + 1)
    await page.getByRole('button', { name: '記錄另一筆事件' }).click()
    await expect(page.getByRole('textbox', { name: '事件 ID' })).toBeEnabled()
  })

  function scalar(sql: string, value: string, databasePath = app.commercePath): string {
    const script = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); print(db.execute(sys.argv[2],(sys.argv[3],)).fetchone()[0])'
    return execFileSync('python3', ['-c', script, databasePath, sql, value], { encoding: 'utf8' }).trim()
  }

  function count(sql: string, ...values: string[]): number {
    const script = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); print(db.execute(sys.argv[2],sys.argv[3:]).fetchone()[0])'
    return Number(execFileSync('python3', ['-c', script, app.commercePath, sql, ...values], { encoding: 'utf8' }).trim())
  }

  async function createAcceptedSubscription(page: Page, customerID: string, planID: string, seats = '0') {
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    const customerField = page.getByRole('textbox', { name: /客戶 ID/ })
    await expect(customerField).toBeVisible()
    if (await customerField.isDisabled()) await page.getByRole('button', { name: '建立另一筆報價' }).click()
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill(planID)
    await page.getByRole('textbox', { name: /席次/ }).fill(seats)
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=? ORDER BY rowid DESC LIMIT 1', customerID)
    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await page.getByRole('button', { name: '預覽接受' }).click()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const subscriptionID = scalar('SELECT id FROM subscriptions WHERE quote_id=?', quoteID)
    const invoiceID = scalar('SELECT id FROM invoices WHERE subscription_id=?', subscriptionID)
    const operationID = scalar('SELECT id FROM payment_operations WHERE invoice_id=?', invoiceID)
    return { quoteID, subscriptionID, invoiceID, operationID }
  }

  async function createPaidSubscription(page: Page, customerID: string, planID: string, seats = '0') {
    const { quoteID, subscriptionID, invoiceID, operationID } = await createAcceptedSubscription(page, customerID, planID, seats)
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/reconcile`)
    await page.getByRole('button', { name: '確認查證' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認查證' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    return { quoteID, subscriptionID, invoiceID, operationID }
  }

  test('login, protected reads, logout, and responsive form validation', async ({ page }) => {
    const unauthenticated = await page.request.get(`${app.baseURL}/admin/api/overview`)
    expect(unauthenticated.status()).toBe(401)
    await signIn(page)
    const overview = await page.request.get(`${app.baseURL}/admin/api/overview`)
    expect(overview.status()).toBe(200)
    expect((await overview.json()).counts).toBeTruthy()

    await page.setViewportSize({ width: 390, height: 844 })
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByText('請輸入客戶 ID')).toBeVisible()
    await expect(page.getByText('請輸入方案 ID')).toBeVisible()
    const menuButton = page.getByRole('button', { name: '開啟選單' })
    await menuButton.click()
    await expect(page.getByRole('button', { name: '關閉' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('button', { name: '關閉' })).toHaveCount(0)
    await expect(menuButton).toBeFocused()
    await page.getByRole('button', { name: '登 出' }).click()
    await expect(page).toHaveURL(/\/admin\/login$/)
    expect((await page.request.get(`${app.baseURL}/admin/api/overview`)).status()).toBe(401)
  })

  test('logout in one tab sends another tab to login on its next read', async ({ page }) => {
    await signIn(page)
    const other = await page.context().newPage()
    await other.goto(`${app.baseURL}/admin/`)
    await expect(other.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    await page.getByRole('button', { name: '登 出' }).click()
    await expect(page).toHaveURL(/\/admin\/login$/)
    await other.getByRole('menuitem', { name: '付款', exact: true }).click()
    await expect(other).toHaveURL(/\/admin\/login$/)
    expect(await other.evaluate(() => window.history.state?.usr?.from)).toBe('/payments')
    expect((await other.request.get(`${app.baseURL}/admin/api/payments`)).status()).toBe(401)
    await other.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await other.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await other.getByRole('button', { name: '登 入' }).click()
    await expect(other).toHaveURL(/\/admin\/payments$/)
  })

  test('logout with an expired session clears the stale admin screen', async ({ page }) => {
    await signIn(page)
    const other = await page.context().newPage()
    await other.goto(`${app.baseURL}/admin/`)
    await expect(other.getByRole('heading', { name: '營運概覽' })).toBeVisible()

    await page.getByRole('button', { name: '登 出' }).click()
    await expect(page).toHaveURL(/\/admin\/login$/)
    await other.getByRole('button', { name: '登 出' }).click()
    await expect(other).toHaveURL(/\/admin\/login$/)
  })

  test('late 401 from an old request cannot clear a newer login', async ({ page }) => {
    await signIn(page)
    let releaseOld!: () => void
    let markStarted!: () => void
    const held = new Promise<void>((resolve) => { releaseOld = resolve })
    const started = new Promise<void>((resolve) => { markStarted = resolve })
    let interceptFirst = true
    await page.route('**/admin/api/payments?**', async (route) => {
      if (!interceptFirst) return route.continue()
      interceptFirst = false
      markStarted()
      await held
      await route.fulfill({ status: 401, contentType: 'application/json', body: '{"error":{"code":"SESSION_REQUIRED","message":"Sign in to continue"}}' })
    })

    await page.getByRole('menuitem', { name: '付款', exact: true }).click()
    await started
    const session = await (await page.request.get(`${app.baseURL}/admin/api/session`)).json() as { csrf_token: string }
    const logout = await page.request.delete(`${app.baseURL}/admin/api/session`, { headers: { Origin: app.baseURL, 'X-CSRF-Token': session.csrf_token } })
    expect(logout.status()).toBe(204)
    await page.getByRole('menuitem', { name: '報價', exact: true }).click()
    await expect(page).toHaveURL(/\/admin\/login$/)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page).toHaveURL(/\/admin\/quotes$/)

    const lateResponse = page.waitForResponse((response) => response.url().includes('/admin/api/payments?') && response.status() === 401)
    releaseOld()
    await lateResponse
    await page.waitForTimeout(250)
    await expect(page).toHaveURL(/\/admin\/quotes$/)
    expect((await page.request.get(`${app.baseURL}/admin/api/session`)).status()).toBe(200)
  })

  test('embedded admin upgrades a CLI database and preserves its invoice and provider capture', async ({ page }) => {
    const upgraded = await startLocalAdmin({ seedDemo: true })
    try {
      expect(scalar('SELECT MAX(version) FROM admin_schema_versions WHERE ? IS NOT NULL', 'seeded', upgraded.commercePath)).toBe('9')
      expect(scalar("SELECT COUNT(*) FROM pragma_table_info('admin_commands') WHERE name='request_id' AND ? IS NOT NULL", 'seeded', upgraded.commercePath)).toBe('1')
      expect(scalar("SELECT COUNT(*) FROM pragma_table_info('admin_audit') WHERE name='request_id' AND ? IS NOT NULL", 'seeded', upgraded.commercePath)).toBe('1')
      expect(scalar("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='admin_external_dispatch_claims' AND ? IS NOT NULL", 'seeded', upgraded.commercePath)).toBe('1')
      expect(scalar('SELECT COUNT(*) FROM admin_external_dispatch_claims WHERE ? IS NOT NULL', 'seeded', upgraded.commercePath)).toBe('0')
      const invoiceID = scalar('SELECT i.id FROM invoices i JOIN subscriptions s ON s.id=i.subscription_id WHERE s.customer_id=?', 'demo-customer', upgraded.commercePath)
      expect(scalar('SELECT total_minor FROM invoices WHERE id=?', invoiceID, upgraded.commercePath)).toBe('2000')
      const operationID = scalar('SELECT id FROM payment_operations WHERE invoice_id=?', invoiceID, upgraded.commercePath)
      const providerKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', operationID, upgraded.commercePath)
      expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, upgraded.providerPath))).toBe(1)

      await page.goto(`${upgraded.baseURL}/admin/login`)
      await page.getByRole('textbox', { name: /帳號/ }).fill(upgraded.username)
      await page.getByRole('textbox', { name: /密碼/ }).fill(upgraded.password)
      await page.getByRole('button', { name: '登 入' }).click()
      await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
      await page.goto(`${upgraded.baseURL}/admin/invoices/${invoiceID}`)
      await expect(page.getByText(invoiceID).first()).toBeVisible()
      expect(scalar('SELECT total_minor FROM invoices WHERE id=?', invoiceID, upgraded.commercePath)).toBe('2000')
      expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, upgraded.providerPath))).toBe(1)
    } finally {
      await upgraded.stop()
    }
  })

  test('invalid login and missing CSRF cannot create a financial command', async ({ page }) => {
    await page.goto(`${app.baseURL}/admin/login`)
    await page.getByRole('textbox', { name: /帳號/ }).fill(app.username)
    await page.getByRole('textbox', { name: /密碼/ }).fill('incorrect-password')
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByText('Invalid username or password')).toBeVisible()
    expect((await page.request.get(`${app.baseURL}/admin/api/overview`)).status()).toBe(401)

    await page.getByRole('textbox', { name: /密碼/ }).fill(app.password)
    await page.getByRole('button', { name: '登 入' }).click()
    await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
    const customerID = `csrf-blocked-${randomUUID()}`
    const response = await page.request.post(`${app.baseURL}/admin/api/commands`, {
      data: { idempotency_key: randomUUID(), action_id: 'C01', payload: { customer_id: customerID, plan_id: 'basic' } },
    })
    expect(response.status()).toBe(403)
    expect(count('SELECT COUNT(*) FROM quotes WHERE customer_id=?', customerID)).toBe(0)
  })

  test('known operation routes render and an error does not leak to another action', async ({ page }) => {
    await signIn(page)
    await page.goto(`${app.baseURL}/admin/jobs/entitlement-refresh`)
    // This assertion is about UI error isolation; eligible subscriptions are
    // created by earlier tests, so a real empty-batch error is not stable.
    await page.route('**/admin/api/previews', async (route) => {
      await route.fulfill({ status: 422, contentType: 'application/json', body: '{"error":{"code":"INVALID_PREVIEW","message":"Preview unavailable"}}' })
    })
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    await page.unroute('**/admin/api/previews')
    await page.getByRole('menuitem', { name: '發布 Pro 價格' }).click()
    await expect(page.getByRole('heading', { name: '發布 Pro 價格版本' })).toBeVisible()
    await expect(page.getByText('無法建立預覽')).toHaveCount(0)

    const routes = [
      '/admin/quotes/new', '/admin/usage-events/new', '/admin/jobs/change-corrections',
      '/admin/prices/pro/new', '/admin/meters/new', '/admin/prices/metered/new',
      '/admin/catalog-selections/new', '/admin/price-migrations/new', '/admin/usage-adjustments/new',
      '/admin/jobs/usage-credit-notes', '/admin/contracts/new', '/admin/jobs/contract-collections',
      '/admin/reconciliation-runs/new', '/admin/account-migrations/new', '/admin/jobs/renewals',
      '/admin/jobs/entitlement-refresh', '/admin/lab/controls', '/admin/commands', '/admin/commands/missing',
      '/admin/data/Payments', '/admin/data/UsageEvents',
      '/admin/quotes/missing/accept', '/admin/price-migrations/missing', '/admin/price-migrations/missing/pause',
      '/admin/subscriptions/missing/cancel', '/admin/subscriptions/missing/schedule-plan',
      '/admin/subscriptions/missing/upgrade', '/admin/invoices/missing/payments/new',
      '/admin/invoices/missing/reductions/new', '/admin/credits/missing/apply',
      '/admin/invoices/missing/history/corrections', '/admin/invoices/missing/history/applications',
      '/admin/invoices/missing/history/grants', '/admin/invoices/missing/history/refunds',
      '/admin/changes/missing/resolve-unfulfilled', '/admin/credits/missing/refunds/new',
      '/admin/payments/missing/dispatch', '/admin/payments/missing/reconcile',
      '/admin/refunds/missing/dispatch', '/admin/refunds/missing/reconcile',
      '/admin/price-migrations/missing/skip', '/admin/price-migrations/missing/resume',
      '/admin/usage-periods/missing/close', '/admin/usage-periods/missing/rerate',
      '/admin/discrepancies/missing/repair', '/admin/discrepancies/missing/manual-decisions',
      '/admin/account-migrations/missing/shadow-quotes', '/admin/account-migrations/missing/shadow-entitlements',
      '/admin/account-migrations/missing/provenance',
      '/admin/account-migrations/missing/provenance/legacy/resolve',
      '/admin/account-migrations/missing/switch-read', '/admin/account-migrations/missing/switch-writer',
      '/admin/account-migrations/missing/stop', '/admin/lab/clock',
      '/admin/lab/payment-decisions/missing', '/admin/lab/refund-decisions/missing',
      '/admin/lab/faults/missing', '/admin/payments/missing/retry',
    ]
    const pageErrors: string[] = []
    page.on('pageerror', (error) => pageErrors.push(error.message))
    await page.setViewportSize({ width: 390, height: 844 })
    for (const route of routes) {
      await page.goto(`${app.baseURL}${route}`)
      await expect(page).toHaveURL(`${app.baseURL}${route}`)
      await expect(page.getByText('找不到頁面')).toHaveCount(0)
      await expect(page.locator('body')).not.toBeEmpty()
      await expect(page.locator('.ant-skeleton')).toHaveCount(0)
      const mobileMenu = page.getByRole('button', { name: '開啟選單' })
      await expect(mobileMenu).toBeVisible()
      await page.keyboard.press('Tab')
      await expect(mobileMenu).toBeFocused()
      const unassociatedLabels = await page.locator('.ant-form-item-label label').evaluateAll((labels) => labels.flatMap((label) => {
        if (!label.getClientRects().length) return []
        const item = label.closest('.ant-form-item')
        const control = item?.querySelector('input, textarea, select, [role="combobox"], [role="checkbox"], [role="switch"]')
        if (!control) return []
        const forID = label.getAttribute('for')
        if (forID && label.control?.id === forID && item?.contains(label.control)) return []
        if (control.getAttribute('aria-label') || control.getAttribute('aria-labelledby')) return []
        return [label.textContent?.trim() ?? '(empty label)']
      }))
      expect(unassociatedLabels, `unassociated form labels at ${route}`).toEqual([])
      const unnamedControls = await page.locator('input, textarea, select').evaluateAll((controls) => controls.flatMap((control) => {
        if (!control.getClientRects().length || control.getAttribute('type') === 'hidden') return []
        if ((control as HTMLInputElement).labels?.length) return []
        if (control.getAttribute('aria-label') || control.getAttribute('aria-labelledby')) return []
        return [`${control.tagName.toLowerCase()}#${control.id || '(no id)'}`]
      }))
      expect(unnamedControls, `unnamed form controls at ${route}`).toEqual([])
      const overflow = await page.evaluate(() => Math.max(document.body.scrollWidth, document.documentElement.scrollWidth) - window.innerWidth)
      expect(overflow, `horizontal page overflow at ${route}`).toBeLessThanOrEqual(1)
    }
    expect(pageErrors).toEqual([])
  })

  test('quote and acceptance create one financial obligation under replay', async ({ page }) => {
    await signIn(page)
    const customerID = `browser-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByText('succeeded')).toBeVisible()

    const commandsResponse = await page.request.get(`${app.baseURL}/admin/api/commands?limit=100`)
    expect(commandsResponse.status()).toBe(200)
    const commands = (await commandsResponse.json()).items as Array<{
      id: string; action_id: string; result_refs: Record<string, string>
    }>
    const create = commands.find((command) => command.action_id === 'C01')
    expect(create?.result_refs.quote_id).toBeTruthy()
    const quoteID = create!.result_refs.quote_id
    expect(count('SELECT COUNT(*) FROM quotes WHERE id=?', quoteID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', create!.id)).toBe(1)

    const sessionResponse = await page.request.get(`${app.baseURL}/admin/api/session`)
    const session = await sessionResponse.json() as { csrf_token: string }
    const originalPayload = { customer_id: customerID, plan_id: 'basic', cohort: 'default', seats: '0' }
    const replay = await page.request.post(`${app.baseURL}/admin/api/commands`, {
      headers: { 'Origin': app.baseURL, 'Idempotency-Key': scalar('SELECT idempotency_key FROM admin_commands WHERE id=?', create!.id), 'X-CSRF-Token': session.csrf_token },
      data: { action_id: 'C01', target_id: '', payload: originalPayload },
    })
    expect(replay.status()).toBe(200)
    expect((await replay.json()).id).toBe(create!.id)
    const conflict = await page.request.post(`${app.baseURL}/admin/api/commands`, {
      headers: { 'Origin': app.baseURL, 'Idempotency-Key': scalar('SELECT idempotency_key FROM admin_commands WHERE id=?', create!.id), 'X-CSRF-Token': session.csrf_token },
      data: { action_id: 'C01', target_id: '', payload: { ...originalPayload, customer_id: 'different' } },
    })
    expect(conflict.status()).toBe(409)
    expect(count('SELECT COUNT(*) FROM quotes WHERE id=?', quoteID)).toBe(1)

    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await expect(page.getByRole('heading', { name: '接受報價' })).toBeVisible()
    await expect(page.getByRole('row', { name: /報價接受時現在應付/ })).toContainText('USD 20.00')
    await expect(page.getByRole('row', { name: /下一整期固定承諾/ })).toContainText('USD 20.00')
    await expect(page.getByRole('row', { name: /用量費率/ })).toContainText('未設定用量收費')
    await expect(page.getByRole('row', { name: /稅務/ })).toContainText('報價未包含稅額')
    await page.getByRole('button', { name: '預覽接受' }).click()
    await expect(page.getByText('將建立的付款義務')).toBeVisible()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(page.getByText('succeeded')).toBeVisible()
    expect(count('SELECT COUNT(*) FROM subscriptions WHERE quote_id=?', quoteID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM invoices WHERE subscription_id=(SELECT id FROM subscriptions WHERE quote_id=?)', quoteID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id IN (SELECT id FROM invoices WHERE subscription_id=(SELECT id FROM subscriptions WHERE quote_id=?))', quoteID)).toBe(1)

    const acceptedCommands = (await (await page.request.get(`${app.baseURL}/admin/api/commands?limit=100`)).json()).items as typeof commands
    const accept = acceptedCommands.find((command) => command.action_id === 'C02')
    expect(accept).toBeTruthy()
    const acceptReplay = await page.request.post(`${app.baseURL}/admin/api/commands`, {
      headers: { 'Origin': app.baseURL, 'Idempotency-Key': scalar('SELECT idempotency_key FROM admin_commands WHERE id=?', accept!.id), 'X-CSRF-Token': session.csrf_token },
      data: {
        action_id: 'C02', target_id: quoteID,
        preview_id: scalar('SELECT preview_id FROM admin_commands WHERE id=?', accept!.id),
        payload: JSON.parse(scalar('SELECT payload_json FROM admin_commands WHERE id=?', accept!.id)),
      },
    })
    expect(acceptReplay.status()).toBe(200)
    expect((await acceptReplay.json()).id).toBe(accept!.id)
    expect(count('SELECT COUNT(*) FROM subscriptions WHERE quote_id=?', quoteID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM payment_operations WHERE invoice_id IN (SELECT id FROM invoices WHERE subscription_id=(SELECT id FROM subscriptions WHERE quote_id=?))', quoteID)).toBe(1)

  const operationID = accept!.result_refs.operation_id
  expect(operationID).toBeTruthy()
  const invoiceID = scalar('SELECT id FROM invoices WHERE subscription_id=(SELECT id FROM subscriptions WHERE quote_id=?)', quoteID)
  await page.goto(`${app.baseURL}/admin/lab/faults/${operationID}`)
    await page.getByRole('combobox', { name: /操作種類/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('付款', { exact: true }).click()
    await page.getByRole('combobox', { name: /故障模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('回應遺失', { exact: true }).click()
  await page.getByRole('button', { name: '確認故障票據' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '確認故障票據' }).click()
  await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
  await page.getByRole('button', { name: '執行另一個操作' }).click()
  await page.getByRole('combobox', { name: /操作種類/ }).click()
  await page.locator('.ant-select-dropdown:visible').getByText('付款', { exact: true }).click()
    await page.getByRole('combobox', { name: /故障模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('提供者完成後中斷', { exact: true }).click()
    const rejectedFaultPreview = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認故障票據' }).click()
    expect((await rejectedFaultPreview).status()).toBe(409)
    await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(count('SELECT COUNT(*) FROM admin_fault_tickets WHERE operation_id=?', operationID)).toBe(1)

  await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByText('waiting_verification')).toBeVisible()
    const dispatchID = scalar("SELECT id FROM admin_commands WHERE action_id=? ORDER BY created_at DESC LIMIT 1", 'C09')
    expect(scalar('SELECT status FROM admin_commands WHERE id=?', dispatchID)).toBe('waiting_verification')
  const providerKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', operationID)
  expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath))).toBe(1)
  await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/reductions/new`)
  await page.getByLabel('減額（最小貨幣單位）', { exact: true }).fill('500')
  await page.getByLabel('減額理由', { exact: true }).fill('capture still unknown')
  const blockedReduction = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
  await page.getByRole('button', { name: '建立預覽' }).click()
  expect((await blockedReduction).status()).toBe(409)
  await expect(page.getByText('無法建立預覽')).toBeVisible()
  expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=? AND target_id=?', 'C11', invoiceID)).toBe(0)
  expect(count('SELECT COUNT(*) FROM corrections WHERE invoice_id=?', invoiceID)).toBe(0)
  await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
  await page.reload()
    await expect(page.getByText('waiting_verification', { exact: true })).toBeVisible()
    await app.restart()
    expect((await page.request.get(`${app.baseURL}/admin/api/overview`)).status()).toBe(401)
    await signIn(page)
    await page.goto(`${app.baseURL}/admin/commands/${dispatchID}`)
    await expect(page.getByText('命令詳情', { exact: true })).toBeVisible()
    // Restart recovery may finish the command before the browser opens its detail page.
    const resume = page.getByRole('button', { name: '重新查證' })
    if (await resume.count()) await resume.click()
    await expect(page.getByText('succeeded', { exact: true })).toBeVisible()
    expect(scalar('SELECT status FROM admin_commands WHERE id=?', dispatchID)).toBe('succeeded')
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', dispatchID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_fault_tickets WHERE operation_id=?', operationID)).toBe(1)
    expect(scalar('SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?', operationID)).toBe(dispatchID)
    expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath))).toBe(1)
  })

  test('provider commit followed by a crash recovers the original payment in a new session', async ({ page, browser }) => {
    await signIn(page)
    const { operationID } = await createAcceptedSubscription(page, `crash-capture-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/lab/faults/${operationID}`)
    await page.getByRole('combobox', { name: /操作種類/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('付款', { exact: true }).click()
    await page.getByRole('combobox', { name: /故障模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('提供者完成後中斷', { exact: true }).click()
    await page.getByRole('button', { name: '確認故障票據' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認故障票據' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    const dispatchResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    const response = await dispatchResponse
    expect(response.status()).toBe(503)
    const body = await response.json() as { command_id: string; error: { code: string; retryable: boolean } }
    expect(body.error).toMatchObject({ code: 'COMMAND_PENDING_RETRY', retryable: true })
    expect(body.command_id).toBeTruthy()
    const commandID = body.command_id
    await expect(page.getByRole('main').getByText('accepted', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '繼續原命令' })).toBeVisible()
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('submitted')
    expect(count('SELECT COUNT(*) FROM allocations WHERE operation_id=?', operationID)).toBe(0)
    const providerKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', operationID)
    expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath))).toBe(1)

    const freshContext = await browser.newContext()
    try {
      const freshPage = await freshContext.newPage()
      await signIn(freshPage)
      await freshPage.goto(`${app.baseURL}/admin/commands/${commandID}`)
      await expect(freshPage.getByRole('main').getByText('accepted', { exact: true }).first()).toBeVisible()
      await freshPage.getByRole('button', { name: '繼續原命令' }).click()
      await expect(freshPage.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    } finally {
      await freshContext.close()
    }
    await page.reload()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('succeeded')
    expect(count('SELECT COUNT(*) FROM allocations WHERE operation_id=?', operationID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=? AND target_id=?', 'C09', operationID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_fault_tickets WHERE operation_id=?', operationID)).toBe(1)
    expect(scalar('SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?', operationID)).toBe(commandID)
    expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath))).toBe(1)
  })

  test('provider commit followed by a crash preserves the refund reservation across sessions', async ({ page, browser }) => {
    await signIn(page)
    const { invoiceID } = await createPaidSubscription(page, `crash-refund-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/reductions/new`)
    await page.getByLabel('減額（最小貨幣單位）', { exact: true }).fill('1000')
    await page.getByLabel('減額理由', { exact: true }).fill('crash refund credit')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認減額' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const grantID = scalar('SELECT id FROM credit_grants WHERE source_invoice_id=?', invoiceID)
    await page.goto(`${app.baseURL}/admin/credits/${grantID}/refunds/new`)
    await page.getByLabel('退款金額（最小貨幣單位）', { exact: true }).fill('500')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認預留退款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認預留退款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const refundID = scalar('SELECT id FROM refund_operations WHERE grant_id=?', grantID)

    await page.goto(`${app.baseURL}/admin/lab/faults/${refundID}`)
    await page.getByRole('combobox', { name: /操作種類/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('退款', { exact: true }).click()
    await page.getByRole('combobox', { name: /故障模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('提供者完成後中斷', { exact: true }).click()
    await page.getByRole('button', { name: '確認故障票據' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認故障票據' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await page.goto(`${app.baseURL}/admin/refunds/${refundID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出退款' }).last().click()
    const dispatchResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('dialog').getByRole('button', { name: '確認送出退款' }).click()
    const response = await dispatchResponse
    expect(response.status()).toBe(503)
    const body = await response.json() as { command_id: string; error: { code: string; retryable: boolean } }
    expect(body.error).toMatchObject({ code: 'COMMAND_PENDING_RETRY', retryable: true })
    expect(body.command_id).toBeTruthy()
    const commandID = body.command_id
    await expect(page.getByRole('main').getByText('accepted', { exact: true })).toBeVisible()
    expect(scalar('SELECT status FROM refund_operations WHERE id=?', refundID)).toBe('submitted')
    expect(count("SELECT COALESCE(SUM(amount_minor),0) FROM refund_operations WHERE grant_id=? AND status IN ('created','submitted','unknown')", grantID)).toBe(500)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(0)
    const providerKey = scalar('SELECT provider_key FROM refund_operations WHERE id=?', refundID)
    expect(Number(scalar('SELECT COUNT(*) FROM refunds WHERE provider_key=?', providerKey, app.providerPath))).toBe(1)

    const freshContext = await browser.newContext()
    try {
      const freshPage = await freshContext.newPage()
      await signIn(freshPage)
      await freshPage.goto(`${app.baseURL}/admin/commands/${commandID}`)
      await expect(freshPage.getByRole('main').getByText('accepted', { exact: true }).first()).toBeVisible()
      await freshPage.getByRole('button', { name: '繼續原命令' }).click()
      await expect(freshPage.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    } finally {
      await freshContext.close()
    }
    await page.reload()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(scalar('SELECT status FROM refund_operations WHERE id=?', refundID)).toBe('succeeded')
    expect(count("SELECT COALESCE(SUM(amount_minor),0) FROM refund_operations WHERE grant_id=? AND status IN ('created','submitted','unknown')", grantID)).toBe(0)
    expect(count("SELECT COALESCE(SUM(amount_minor),0) FROM refund_operations WHERE grant_id=? AND status='succeeded'", grantID)).toBe(500)
    expect(count('SELECT COUNT(*) FROM refund_operations WHERE grant_id=?', grantID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=? AND target_id=?', 'C16', refundID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(scalar('SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?', refundID)).toBe(commandID)
    expect(Number(scalar('SELECT COUNT(*) FROM refunds WHERE provider_key=?', providerKey, app.providerPath))).toBe(1)
  })

  test('clock changes while a payment awaits verification preserve its accepted business time', async ({ page }) => {
    await signIn(page)
    const originalTime = new Date(Math.floor(Date.now() / 1000) * 1000).toISOString()
    const laterTime = new Date(Date.parse(originalTime) + 24 * 60 * 60 * 1000).toISOString()
    const canonicalOriginalTime = originalTime.replace('.000Z', 'Z')
    const canonicalLaterTime = laterTime.replace('.000Z', 'Z')
    const session = await (await page.request.get(`${app.baseURL}/admin/api/session`)).json() as { csrf_token: string }
    const setClock = async (mode: 'fixed' | 'real', value?: string) => {
      await submitClockControl(page, app.baseURL, session.csrf_token, mode, value)
    }

    try {
      await setClock('fixed', originalTime)
      const { operationID, subscriptionID } = await createAcceptedSubscription(page, `clock-pending-${randomUUID()}`, 'basic')
      await page.goto(`${app.baseURL}/admin/lab/faults/${operationID}`)
      await page.getByRole('combobox', { name: /操作種類/ }).click()
      await page.locator('.ant-select-dropdown:visible').getByText('付款', { exact: true }).click()
      await page.getByRole('combobox', { name: /故障模式/ }).click()
      await page.locator('.ant-select-dropdown:visible').getByText('回應遺失', { exact: true }).click()
      await page.getByRole('button', { name: '確認故障票據' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認故障票據' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

      await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
      await page.getByRole('button', { name: '建立預覽' }).click()
      await page.getByRole('button', { name: '確認送出付款' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
      await expect(page.getByText('waiting_verification')).toBeVisible()
      const commandID = scalar('SELECT id FROM admin_commands WHERE action_id=? ORDER BY created_at DESC LIMIT 1', 'C09')
      expect(scalar('SELECT business_time FROM admin_commands WHERE id=?', commandID)).toBe(canonicalOriginalTime)
      expect(scalar('SELECT status FROM subscriptions WHERE id=?', subscriptionID)).toBe('pending')
      expect(count('SELECT COUNT(*) FROM allocations WHERE operation_id=?', operationID)).toBe(0)
      const providerKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', operationID)
      expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath))).toBe(1)

      await page.goto(`${app.baseURL}/admin/lab/clock`)
      await page.getByRole('combobox', { name: /模式/ }).click()
      await page.locator('.ant-select-dropdown:visible').getByText('固定時間', { exact: true }).click()
      await page.getByRole('textbox', { name: /固定 UTC 時間/ }).fill(laterTime)
      await page.getByRole('button', { name: '確認設定時鐘' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect((await (await page.request.get(`${app.baseURL}/admin/api/lab/clock`)).json()).business_time).toBe(canonicalLaterTime)
      expect(count('SELECT COUNT(*) FROM allocations WHERE operation_id=?', operationID)).toBe(0)
      expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath))).toBe(1)

      await page.goto(`${app.baseURL}/admin/commands/${commandID}`)
      await expect(page.getByText('waiting_verification')).toBeVisible()
      await page.getByRole('button', { name: '重新查證' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(scalar('SELECT business_time FROM admin_commands WHERE id=?', commandID)).toBe(canonicalOriginalTime)
      expect(scalar('SELECT status FROM subscriptions WHERE id=?', subscriptionID)).toBe('active')
      expect(scalar('SELECT period_start FROM billing_periods WHERE subscription_id=? AND period_index=0', subscriptionID)).toBe(String(BigInt(Date.parse(originalTime)) * 1_000_000n))
      expect(count('SELECT COUNT(*) FROM allocations WHERE operation_id=?', operationID)).toBe(1)
      expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
      expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath))).toBe(1)
    } finally {
      await setClock('real')
    }
  })

  test('customer and subscription workbench shows actual price and scheduled intent', async ({ page }) => {
    await signIn(page)
    expect((await page.request.get(`${app.baseURL}/admin/api/customers?cursor=invalid`)).status()).toBe(400)
    expect((await page.request.get(`${app.baseURL}/admin/api/customers/missing-customer`)).status()).toBe(404)
    expect((await page.request.get(`${app.baseURL}/admin/api/subscriptions/missing-subscription`)).status()).toBe(404)
    const customerID = `detail-browser-${randomUUID()}`
    const { subscriptionID } = await createPaidSubscription(page, customerID, 'pro', '5')
    await page.goto(`${app.baseURL}/admin/customers`)
    await page.getByRole('row').filter({ hasText: customerID }).getByRole('button', { name: '詳情' }).click()
    await expect(page.getByRole('heading', { name: '客戶詳情' })).toBeVisible()
    await expect(page.getByText(customerID, { exact: true }).first()).toBeVisible()
    await page.getByRole('button', { name: '為此客戶建立報價' }).click()
    await expect(page.getByRole('textbox', { name: /客戶 ID/ })).toHaveValue(customerID)
    await page.goto(`${app.baseURL}/admin/data/Customers`)
    await expect(page.getByRole('row').filter({ hasText: customerID })).toBeVisible()
    await page.goto(`${app.baseURL}/admin/customers/${encodeURIComponent(customerID)}`)
    await page.getByRole('button', { name: subscriptionID }).first().click()
    await expect(page.getByRole('heading', { name: '訂閱詳情' })).toBeVisible()
    await expect(page.getByText('USD 100.00')).toBeVisible()
    await expect(page.getByText('pro-v1')).toBeVisible()
    await expect(page.getByText('當前帳期')).toBeVisible()
    await page.getByRole('button', { name: '排程取消', exact: true }).click()
    await page.getByRole('button', { name: '預覽取消' }).click()
    await page.getByRole('button', { name: '確認排程取消' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認排程' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/subscriptions/${encodeURIComponent(subscriptionID)}`)
    await expect(page.getByRole('button', { name: '恢復取消排程' })).toBeVisible()
  })

  test('concurrent contract publication keeps the first version and rejects an older preview', async ({ page, context }) => {
    await signIn(page)
    const contractID = `concurrent-contract-${randomUUID()}`
    const customerID = `concurrent-customer-${randomUUID()}`
    const from = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString()
    const to = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000).toISOString()
    const fillContract = async (activePage: Page, fixedMinor: string) => {
      await activePage.goto(`${app.baseURL}/admin/contracts/new`)
      for (const [label, value] of [
        ['合約版本 ID', contractID], ['客戶 ID', customerID], ['版本序號', '1'],
        ['基礎價格版本 ID', 'pro-v1'], ['固定金額', fixedMinor], ['每席金額', '700'],
        ['生效起點（UTC）', from], ['生效終點（UTC）', to],
      ]) await activePage.getByLabel(label, { exact: true }).fill(value)
      await activePage.getByRole('button', { name: '建立預覽' }).click()
      await expect(activePage.getByText('操作預覽')).toBeVisible()
    }

    await fillContract(page, '4000')
    const other = await context.newPage()
    await fillContract(other, '5000')
    await other.getByRole('button', { name: '確認發布合約' }).last().click()
    await other.getByRole('dialog').getByRole('button', { name: '確認發布合約' }).click()
    await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await other.close()

    const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認發布合約' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認發布合約' }).click()
    const rejected = await staleResponse
    expect(rejected.status()).toBe(409)
    expect((await rejected.json()).error.code).toBe('PREVIEW_STALE')
    await expect(page.getByText('原預覽已失效', { exact: true })).toBeVisible()
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    expect(scalar('SELECT fixed_minor FROM contract_versions WHERE id=?', contractID)).toBe('5000')
    expect(count('SELECT COUNT(*) FROM contract_versions WHERE id=?', contractID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C31' AND json_extract(payload_json,'$.id')=? AND status='failed'", contractID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C31' AND json_extract(c.payload_json,'$.id')=?", contractID)).toBe(1)
  })

  test('publishes enterprise contract, quotes seats and accepts Net30 without early capture', async ({ page }) => {
    await signIn(page)
    const suffix = randomUUID().replaceAll('-', '')
    const customerID = `contract-browser-${suffix}`
    const contractID = `contract-browser-${suffix}`
    const priceID = 'pro-v1'
    const start = new Date(Date.now() - 24 * 60 * 60 * 1000).toISOString()
    const end = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000).toISOString()

    await page.goto(`${app.baseURL}/admin/contracts/new`)
    for (const [label, value] of [
      ['合約版本 ID', contractID], ['客戶 ID', customerID], ['版本序號', '1'],
      ['基礎價格版本 ID', priceID], ['固定金額', '4000'], ['每席金額', '700'],
      ['生效起點（UTC）', start], ['生效終點（UTC）', end],
    ] as const) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認發布合約' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認發布合約' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await page.goto(`${app.baseURL}/admin/contracts?id_prefix=${encodeURIComponent(contractID)}`)
    const contractRow = page.getByRole('row').filter({ hasText: contractID })
    await expect(page.getByRole('columnheader', { name: 'EffectiveFrom' })).toBeVisible()
    await expect(page.getByRole('columnheader', { name: 'EffectiveTo' })).toBeVisible()
    await expect(contractRow).toContainText(start.slice(0, 19))
    await expect(contractRow).toContainText(end.slice(0, 19))

    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('combobox', { name: '報價種類' }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('企業合約', { exact: true }).click()
    await page.getByRole('textbox', { name: /合約版本 ID/ }).fill(contractID)
    await page.getByRole('textbox', { name: /席次/ }).fill('5')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT quote_id FROM contract_quotes WHERE contract_version_id=?', contractID)
    expect(count('SELECT amount_minor FROM quotes WHERE id=?', quoteID)).toBe(7500)

    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await expect(page.getByText('Net30，到期後才送出收款')).toBeVisible()
    await expect(page.getByRole('row', { name: /報價接受時現在應付/ })).toContainText('USD 0.00')
    await expect(page.getByRole('row', { name: /下一整期固定承諾/ })).toContainText('USD 75.00')
    await expect(page.getByRole('row', { name: /用量費率/ })).toContainText('1/10 最小貨幣單位')
    await expect(page.getByRole('row', { name: /稅務/ })).toContainText('報價未包含稅額')
    await page.getByRole('button', { name: '預覽接受' }).click()
    await expect(page.getByText('合約 checksum')).toBeVisible()
    await expect(page.getByText('接受時應付')).toBeVisible()
    await expect(page.getByText('首期預計到期日')).toBeVisible()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const subscriptionID = scalar('SELECT id FROM subscriptions WHERE quote_id=?', quoteID)
    const invoiceID = scalar('SELECT invoice_id FROM billing_periods WHERE subscription_id=?', subscriptionID)
    const createdAt = BigInt(scalar('SELECT created_at FROM subscriptions WHERE id=?', subscriptionID))
    const dueAt = BigInt(scalar('SELECT due_at FROM billing_periods WHERE subscription_id=?', subscriptionID))
    expect(dueAt - createdAt).toBe(30n * 24n * 60n * 60n * 1_000_000_000n)
    expect(count('SELECT total_minor FROM invoices WHERE id=?', invoiceID)).toBe(7500)
    expect(count('SELECT COUNT(*) FROM outbox WHERE id IN (SELECT "capture:"||o.id FROM payment_operations o WHERE o.invoice_id=?)', invoiceID)).toBe(0)
    expect(count('SELECT COUNT(*) FROM contract_subscriptions WHERE subscription_id=?', subscriptionID)).toBe(1)

    await page.goto(`${app.baseURL}/admin/contracts?id_prefix=${encodeURIComponent(contractID)}`)
    await page.getByRole('button', { name: '開啟合約' }).click()
    await expect(page).toHaveURL(`${app.baseURL}/admin/contracts/${contractID}`)
    await expect(page.getByRole('heading', { name: '合約版本詳情' })).toBeVisible()
    await page.reload()
    await expect(page.getByRole('heading', { name: '合約版本詳情' })).toBeVisible()
    await expect(page.getByText('Net30', { exact: true })).toBeVisible()
    await expect(page.getByText('尚未指定合約期滿後價格')).toBeVisible()
  await expect(page.getByRole('row', { name: /本地未結清金額/ }).getByText('USD 75.00')).toBeVisible()
  await expect(page.getByRole('button', { name: invoiceID })).toBeVisible()
  await page.getByRole('button', { name: '查看全部報價' }).click()
  await expect(page).toHaveURL(`${app.baseURL}/admin/quotes?contract_version_id=${contractID}`)
  await expect(page.getByRole('textbox', { name: '合約版本 ID' })).toHaveValue(contractID)
  await expect(page.getByRole('row').filter({ hasText: quoteID })).toBeVisible()
  await page.reload()
  await expect(page.getByRole('textbox', { name: '合約版本 ID' })).toHaveValue(contractID)
  await page.goto(`${app.baseURL}/admin/contracts/${contractID}`)
  await page.getByRole('button', { name: '查看全部訂閱' }).click()
  await expect(page).toHaveURL(`${app.baseURL}/admin/subscriptions?contract_version_id=${contractID}`)
  await expect(page.getByRole('textbox', { name: '合約版本 ID' })).toHaveValue(contractID)
  await expect(page.getByRole('row').filter({ hasText: subscriptionID })).toBeVisible()
  await page.goto(`${app.baseURL}/admin/contracts/${contractID}`)
  await page.setViewportSize({ width: 390, height: 844 })
    const contractOverflow = await page.evaluate(() => ({
      documentWidth: document.documentElement.scrollWidth,
      viewportWidth: window.innerWidth,
      elements: [...document.querySelectorAll('body *')].filter((element) => {
        const rect = element.getBoundingClientRect()
        return rect.right > window.innerWidth + 1 && rect.left < window.innerWidth && rect.width > 0
      }).slice(0, 12).map((element) => ({ tag: element.tagName, className: element.className, text: element.textContent?.trim().slice(0, 40) })),
    }))
    expect(contractOverflow.documentWidth, JSON.stringify(contractOverflow.elements)).toBeLessThanOrEqual(contractOverflow.viewportWidth)
    await page.setViewportSize({ width: 1280, height: 720 })
    await page.getByRole('button', { name: '建立合約報價' }).click()
    await expect(page.getByRole('textbox', { name: '客戶 ID' })).toHaveValue(customerID)
    await expect(page.getByRole('textbox', { name: '合約版本 ID' })).toHaveValue(contractID)
    const sessionResponse = await page.request.get(`${app.baseURL}/admin/api/session`)
    const session = await sessionResponse.json() as { csrf_token: string }
    async function setClock(mode: 'fixed' | 'real', value?: string) {
      await submitClockControl(page, app.baseURL, session.csrf_token, mode, value)
    }
    try {
      const afterDue = new Date(Number(dueAt / 1_000_000n + 1000n)).toISOString()
      await setClock('fixed', afterDue)
      await page.goto(`${app.baseURL}/admin/jobs/contract-collections`)
      await page.getByRole('button', { name: '建立預覽' }).click()
      let collectionKey = ''
      let droppedCollectionResponse = false
      await page.route('**/admin/api/commands', async (route) => {
        if (!droppedCollectionResponse && route.request().method() === 'POST') {
          droppedCollectionResponse = true
          collectionKey = route.request().headers()['idempotency-key']
          await commitThenDropResponse(page, route)
        } else {
          await route.continue()
        }
      })
      await page.getByRole('button', { name: '確認建立收款工作' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認建立收款工作' }).click()
      await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
      expect(collectionKey).toBeTruthy()
      const collectionCommandID = scalar('SELECT id FROM admin_commands WHERE idempotency_key=?', collectionKey)
      const collectionJobID = `job:${collectionCommandID}`
      const collectionOperationID = scalar('SELECT id FROM payment_operations WHERE invoice_id=?', invoiceID)
      expect(count('SELECT COUNT(*) FROM admin_jobs WHERE id=?', collectionJobID)).toBe(1)
      expect(count('SELECT COUNT(*) FROM admin_job_items WHERE job_id=? AND target_id=?', collectionJobID, collectionOperationID)).toBe(1)
      expect(count('SELECT COUNT(*) FROM outbox WHERE id IN (SELECT "capture:"||o.id FROM payment_operations o WHERE o.invoice_id=?)', invoiceID)).toBe(1)
      await page.reload()
      await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
      const collectionReplay = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST')
      await page.getByRole('button', { name: '用原 request key 查詢' }).click()
      expect((await collectionReplay).headers()['idempotency-key']).toBe(collectionKey)
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(count('SELECT COUNT(*) FROM admin_commands WHERE id=?', collectionCommandID)).toBe(1)
      expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', collectionCommandID)).toBe(1)
      expect(count('SELECT COUNT(*) FROM admin_jobs WHERE id=?', collectionJobID)).toBe(1)
      expect(count('SELECT COUNT(*) FROM admin_job_items WHERE job_id=? AND target_id=?', collectionJobID, collectionOperationID)).toBe(1)
      expect(count('SELECT COUNT(*) FROM outbox WHERE id IN (SELECT "capture:"||o.id FROM payment_operations o WHERE o.invoice_id=?)', invoiceID)).toBe(1)
    } finally {
      await setClock('real')
    }
  })

  test('contract without a follow-on price holds renewal and shows the reason', async ({ page }) => {
    await signIn(page)
    const suffix = randomUUID().replaceAll('-', '')
    const customerID = `contract-hold-${suffix}`
    const contractID = `contract-hold-${suffix}`
    async function setClock(mode: 'fixed' | 'real', instant?: string) {
      await page.goto(`${app.baseURL}/admin/lab/clock`)
      await expect(page.locator('.form-page')).toBeVisible()
      const another = page.getByRole('button', { name: '執行另一個操作' })
      if (await another.count()) await another.click()
      await page.getByRole('combobox', { name: /模式/ }).click()
      await page.locator('.ant-select-dropdown:visible').getByText(mode === 'fixed' ? '固定時間' : '實際時間', { exact: true }).click()
      if (instant) await page.getByRole('textbox', { name: /固定 UTC 時間/ }).fill(instant)
      await page.getByRole('button', { name: '確認設定時鐘' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    }

    await setClock('fixed', '2026-09-01T00:00:00Z')
    try {
      await page.goto(`${app.baseURL}/admin/contracts/new`)
      for (const [label, value] of [
        ['合約版本 ID', contractID], ['客戶 ID', customerID], ['版本序號', '1'],
        ['基礎價格版本 ID', 'pro-v1'], ['固定金額', '4000'], ['每席金額', '700'],
        ['生效起點（UTC）', '2026-09-01T00:00:00Z'], ['生效終點（UTC）', '2026-10-01T00:00:00Z'],
      ]) await page.getByLabel(label, { exact: true }).fill(value)
      await page.getByRole('button', { name: '建立預覽' }).click()
      await page.getByRole('button', { name: '確認發布合約' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認發布合約' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

      await page.goto(`${app.baseURL}/admin/quotes/new`)
      await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
      await page.getByRole('combobox', { name: '報價種類' }).click()
      await page.locator('.ant-select-dropdown:visible').getByText('企業合約', { exact: true }).click()
      await page.getByRole('textbox', { name: /合約版本 ID/ }).fill(contractID)
      await page.getByRole('textbox', { name: /席次/ }).fill('5')
      await page.getByRole('button', { name: '建立報價' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=? ORDER BY rowid DESC LIMIT 1', customerID)
      await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
      await page.getByRole('button', { name: '預覽接受' }).click()
      await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      const subscriptionID = scalar('SELECT id FROM subscriptions WHERE quote_id=?', quoteID)

      await setClock('fixed', '2026-10-01T00:00:00Z')
      await page.goto(`${app.baseURL}/admin/jobs/renewals`)
      await page.getByRole('button', { name: '建立預覽' }).click()
      await page.getByRole('button', { name: '確認續約批次' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認續約批次' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(scalar('SELECT reason FROM renewal_holds WHERE subscription_id=? AND period_index=1', subscriptionID)).toBe('contract_next_price_missing')
      expect(count('SELECT COUNT(*) FROM billing_periods WHERE subscription_id=?', subscriptionID)).toBe(1)
      await page.goto(`${app.baseURL}/admin/subscriptions/${subscriptionID}`)
      await expect(page.getByText('合約到期後缺少後續價格，續約已暫停。').first()).toBeVisible()
      await expect(page.getByText('contract_next_price_missing').last()).toBeVisible()
    } finally {
      await setClock('real')
    }
  })

  test('cancel, resume and schedule a plan change without duplicating the subscription', async ({ page }) => {
    await signIn(page)
    const customerID = `cancel-browser-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.reload()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=?', customerID)
    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await page.getByRole('button', { name: '預覽接受' }).click()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const subscriptionID = scalar('SELECT id FROM subscriptions WHERE quote_id=?', quoteID)
    const invoiceID = scalar('SELECT id FROM invoices WHERE subscription_id=?', subscriptionID)
    const operationID = scalar('SELECT id FROM payment_operations WHERE invoice_id=?', invoiceID)
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/reconcile`)
    await page.getByRole('button', { name: '確認查證' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認查證' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await page.goto(`${app.baseURL}/admin/subscriptions/${subscriptionID}/cancel`)
    await expect(page.getByRole('heading', { name: '排程取消訂閱' })).toBeVisible()
    await page.getByRole('button', { name: '預覽取消' }).click()
    await page.getByRole('button', { name: '確認排程取消' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認排程' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='cancel' AND status='scheduled'", subscriptionID)).toBe(1)
	const cancelCommandID = scalar("SELECT id FROM admin_commands WHERE action_id='C05' AND target_id=?", subscriptionID)
	await page.reload()
	await expect(page.getByText(cancelCommandID, { exact: true })).toBeVisible()
	await page.getByRole('button', { name: '依最新訂閱狀態繼續操作' }).click()

    await page.goto(`${app.baseURL}/admin/subscriptions/${subscriptionID}/cancel`)
    await expect(page.getByRole('heading', { name: '恢復取消排程' })).toBeVisible()
    await page.getByRole('button', { name: '預覽恢復取消' }).click()
    await page.getByRole('button', { name: '確認恢復取消' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認恢復' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='cancel' AND status='scheduled'", subscriptionID)).toBe(0)
    expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='cancel' AND status='cancelled'", subscriptionID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM subscriptions WHERE quote_id=?', quoteID)).toBe(1)

    const revision = scalar('SELECT revision FROM subscriptions WHERE id=?', subscriptionID)
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('button', { name: '建立另一筆報價' }).click()
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
    await page.getByRole('textbox', { name: /席次/ }).fill('5')
    await page.getByRole('checkbox', { name: '這是現有訂閱的變更報價' }).check()
    await page.getByRole('textbox', { name: /訂閱 ID/ }).fill(subscriptionID)
    await page.getByRole('combobox', { name: /變更方式/ }).click()
    await page.getByText('下期變更', { exact: true }).click()
    await page.getByRole('textbox', { name: /目前 Revision/ }).fill(revision)
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.getByRole('button', { name: '前往排程下期變更' }).click()
    await expect(page).toHaveURL(new RegExp(`/subscriptions/${subscriptionID}/schedule-plan$`))
    await page.getByRole('button', { name: '預覽下期變更' }).click()
    await expect(page.getByText('變更預覽', { exact: true })).toBeVisible()
    const bindingFingerprint = page.getByRole('textbox', { name: '變更綁定 Fingerprint' })
    const originalFingerprint = await bindingFingerprint.inputValue()
    await bindingFingerprint.fill('changed-after-preview')
    await expect(page.getByText('變更預覽', { exact: true })).toHaveCount(0)
    await expect(page.getByText('變更輸入已修改，請重新預覽')).toBeVisible()
    await bindingFingerprint.fill(originalFingerprint)
    await page.getByRole('button', { name: '預覽下期變更' }).click()
    await expect(page.getByText('變更預覽', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '確認排程' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認排程' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='change' AND status='scheduled'", subscriptionID)).toBe(1)
    expect(scalar("SELECT target_price_version_id FROM subscription_schedules WHERE subscription_id=? AND kind='change' AND status='scheduled'", subscriptionID)).toBe('pro-v1')
    expect(count('SELECT COUNT(*) FROM subscriptions WHERE quote_id=?', quoteID)).toBe(1)
    for (const actionID of ['C03', 'C05', 'C06']) {
      expect(count(`SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id
        WHERE c.action_id=? AND c.target_id=?`, actionID, subscriptionID)).toBe(1)
    }
    await page.goto(`${app.baseURL}/admin/subscriptions/${subscriptionID}`)
    await expect(page.getByText('下期安排', { exact: true })).toBeVisible()
    await expect(page.locator('.ant-descriptions-row').filter({ has: page.getByText('價格與席次變更', { exact: true }) })).toContainText('pro-v1／5 席')
  })

  test('funded reduction reserves and refunds exactly once after a lost response', async ({ page }) => {
    await signIn(page)
    const customerID = `refund-browser-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=?', customerID)

    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await page.getByRole('button', { name: '預覽接受' }).click()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const invoiceID = scalar('SELECT i.id FROM invoices i JOIN subscriptions s ON s.id=i.subscription_id WHERE s.quote_id=?', quoteID)
    const operationID = scalar('SELECT id FROM payment_operations WHERE invoice_id=?', invoiceID)

    await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/reconcile`)
    await page.getByRole('button', { name: '確認查證' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認查證' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/reductions/new`)
    await page.getByLabel('減額（最小貨幣單位）', { exact: true }).fill('1000')
    await page.getByLabel('減額理由', { exact: true }).fill('browser service credit')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認減額' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const grantID = scalar('SELECT id FROM credit_grants WHERE source_invoice_id=?', invoiceID)
    expect(count('SELECT amount_minor FROM credit_grants WHERE id=?', grantID)).toBe(1000)

    await page.goto(`${app.baseURL}/admin/credits/${grantID}/refunds/new`)
    await page.getByLabel('退款金額（最小貨幣單位）', { exact: true }).fill('500')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認預留退款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認預留退款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const refundID = scalar('SELECT id FROM refund_operations WHERE grant_id=?', grantID)
    expect(count('SELECT amount_minor FROM refund_operations WHERE id=?', refundID)).toBe(500)
    const providerKey = scalar('SELECT provider_key FROM refund_operations WHERE id=?', refundID)

    await page.goto(`${app.baseURL}/admin/lab/refund-decisions/${refundID}`)
    await page.getByRole('combobox', { name: /結果/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('成功', { exact: true }).click()
    await page.getByRole('button', { name: '確認退款結果' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認退款結果' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(Number(scalar('SELECT COUNT(*) FROM refunds WHERE provider_key=?', providerKey, app.providerPath))).toBe(0)
    await page.getByRole('button', { name: '執行另一個操作' }).click()
    await page.getByRole('combobox', { name: /結果/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('確定失敗', { exact: true }).click()
    const rejectedRefundDecisionPreview = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認退款結果' }).click()
    expect((await rejectedRefundDecisionPreview).status()).toBe(409)
    await expect(page.getByRole('dialog')).toHaveCount(0)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id=? AND c.target_id=?', 'C48', refundID)).toBe(1)
    expect(Number(scalar('SELECT COUNT(*) FROM refunds WHERE provider_key=?', providerKey, app.providerPath))).toBe(0)

    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}`)
    await expect(page.getByText('此來源額度的退款', { exact: true })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: refundID }).getByText('USD 5.00', { exact: true })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: refundID }).getByRole('button', { name: '送出' })).toBeVisible()
    await page.locator('.ant-card').filter({ has: page.locator('.ant-card-head-title').getByText('此來源額度的退款', { exact: true }) }).getByRole('button', { name: '查看完整歷史' }).click()
    await expect(page).toHaveURL(new RegExp(`/admin/invoices/${invoiceID}/history/refunds$`))
    await expect(page.getByRole('row').filter({ hasText: refundID }).getByText('created', { exact: true })).toBeVisible()

    await page.goto(`${app.baseURL}/admin/lab/faults/${refundID}`)
    await page.getByRole('combobox', { name: /操作種類/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('退款', { exact: true }).click()
    await page.getByRole('combobox', { name: /故障模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('回應遺失', { exact: true }).click()
    await page.getByRole('button', { name: '確認故障票據' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認故障票據' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await page.goto(`${app.baseURL}/admin/refunds/${refundID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出退款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出退款' }).click()
    await expect(page.getByText('waiting_verification')).toBeVisible()
    const dispatchID = scalar("SELECT id FROM admin_commands WHERE action_id=? ORDER BY created_at DESC LIMIT 1", 'C16')
    expect(scalar('SELECT status FROM admin_commands WHERE id=?', dispatchID)).toBe('waiting_verification')
    expect(Number(scalar('SELECT COUNT(*) FROM refunds WHERE provider_key=?', providerKey, app.providerPath))).toBe(1)
    await page.reload()
    await expect(page.getByText('waiting_verification', { exact: true })).toBeVisible()
    await page.goto(`${app.baseURL}/admin/refunds/${refundID}/reconcile`)
    await page.getByRole('button', { name: '確認查證' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認查證' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const reconcileID = scalar("SELECT id FROM admin_commands WHERE action_id='C17' AND target_id=? ORDER BY created_at DESC LIMIT 1", refundID)
    expect(scalar('SELECT status FROM refund_operations WHERE id=?', refundID)).toBe('succeeded')
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', reconcileID)).toBe(1)
    expect(Number(scalar('SELECT COUNT(*) FROM refunds WHERE provider_key=?', providerKey, app.providerPath))).toBe(1)
    await page.goto(`${app.baseURL}/admin/commands`)
    const row = page.getByRole('row').filter({ hasText: dispatchID })
    await row.getByRole('button', { name: '詳情' }).click()
    await page.getByRole('button', { name: '重新查證' }).click()
    await expect(page.locator('.ant-drawer-body').getByText('succeeded', { exact: true })).toBeVisible()
    expect(scalar('SELECT status FROM admin_commands WHERE id=?', dispatchID)).toBe('succeeded')
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', dispatchID)).toBe(1)
    expect(Number(scalar('SELECT COUNT(*) FROM refunds WHERE provider_key=?', providerKey, app.providerPath))).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_fault_tickets WHERE operation_id=?', refundID)).toBe(1)
    expect(scalar('SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?', refundID)).toBe(dispatchID)
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}`)
    const completedRefund = page.getByRole('row').filter({ hasText: refundID })
    await expect(completedRefund.getByText('succeeded', { exact: true })).toBeVisible()
    await expect(completedRefund.getByRole('button', { name: '送出' })).toHaveCount(0)
  })

  test('concurrent invoice reduction shows changed money before reconfirmation', async ({ page }) => {
    await signIn(page)
    const { invoiceID } = await createPaidSubscription(page, `reduction-stale-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/reductions/new`)
    await page.getByLabel('減額（最小貨幣單位）', { exact: true }).fill('100')
    await page.getByLabel('減額理由', { exact: true }).fill('first operator review')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByRole('button', { name: '確認減額' }).last()).toBeVisible()

    const other = await page.context().newPage()
    try {
      await other.goto(`${app.baseURL}/admin/invoices/${invoiceID}/reductions/new`)
      await other.getByLabel('減額（最小貨幣單位）', { exact: true }).fill('500')
      await other.getByLabel('減額理由', { exact: true }).fill('concurrent operator reduction')
      await other.getByRole('button', { name: '建立預覽' }).click()
      await other.getByRole('button', { name: '確認減額' }).last().click()
      await other.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(count('SELECT COUNT(*) FROM corrections WHERE invoice_id=?', invoiceID)).toBe(1)

      const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
      await page.getByRole('button', { name: '確認減額' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
      const rejected = await staleResponse
      expect(rejected.status()).toBe(409)
      const stale = page.locator('.ant-alert').filter({ hasText: '原預覽已失效，請檢查新預覽並再次確認' })
      await expect(stale).toBeVisible()
      await expect(stale.getByText(/原先：/).first()).toBeVisible()
      expect(count('SELECT COUNT(*) FROM corrections WHERE invoice_id=?', invoiceID)).toBe(1)
      const originalRequest = rejected.request()
      const headers = await originalRequest.allHeaders()
      const originalBody = originalRequest.postData()
      if (!originalBody) throw new Error('missing original command body')
      const replay = await page.request.post(`${app.baseURL}/admin/api/commands`, {
        headers: {
          Origin: app.baseURL,
          'X-CSRF-Token': headers['x-csrf-token'],
          'Idempotency-Key': headers['idempotency-key'],
          'Content-Type': 'application/json',
        },
        data: JSON.parse(originalBody),
      })
      expect(replay.status()).toBe(200)
      const originalID = scalar('SELECT id FROM admin_commands WHERE idempotency_key=?', headers['idempotency-key'])
      const original = await replay.json() as { id: string; status: string; error_code: string }
      expect(original).toMatchObject({ id: originalID, status: 'failed', error_code: 'PREVIEW_STALE' })
      expect(count('SELECT COUNT(*) FROM corrections WHERE invoice_id=?', invoiceID)).toBe(1)
      await page.getByRole('button', { name: '確認減額' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(count('SELECT COUNT(*) FROM corrections WHERE invoice_id=?', invoiceID)).toBe(2)
      expect(count('SELECT SUM(reduction_minor) FROM corrections WHERE invoice_id=?', invoiceID)).toBe(600)
      expect(count('SELECT SUM(amount_minor) FROM invoice_lines WHERE invoice_id=?', invoiceID)).toBe(2000)
    } finally {
      await other.close()
    }
  })

  test('credit detail separates available reserved and refunded balances', async ({ page }) => {
    await signIn(page)
    const { invoiceID, operationID } = await createAcceptedSubscription(page, `credit-balance-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/reductions/new`)
    await page.getByLabel('減額（最小貨幣單位）', { exact: true }).fill('1000')
    await page.getByLabel('減額理由', { exact: true }).fill('credit balance detail')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認減額' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const grantID = scalar('SELECT id FROM credit_grants WHERE source_invoice_id=?', invoiceID)
  const correctionID = scalar('SELECT r.correction_id FROM credit_grants g JOIN allocation_releases r ON r.id=g.release_id WHERE g.id=?', grantID)

    await page.goto(`${app.baseURL}/admin/credits?id_prefix=${encodeURIComponent(grantID)}`)
    await page.getByRole('row').filter({ hasText: grantID }).getByRole('button', { name: '詳情' }).click()
    await expect(page).toHaveURL(new RegExp(`/admin/credits/${grantID}$`))
  await expect(page.getByRole('row').filter({ hasText: '來源帳單更正' })).toContainText(correctionID)
    const amount = (label: string, value: string) => page.getByRole('row').filter({ has: page.getByRole('rowheader', { name: label, exact: true }) }).getByRole('cell', { name: value, exact: true })
    await expect(amount('原始額度', 'USD 10.00')).toBeVisible()
    await expect(amount('已抵扣', 'USD 0.00')).toBeVisible()
    await expect(amount('退款保留', 'USD 0.00')).toBeVisible()
    await expect(amount('已退款', 'USD 0.00')).toBeVisible()
    await expect(amount('可用額度', 'USD 10.00')).toBeVisible()
    await expect(page.getByRole('button', { name: invoiceID })).toBeVisible()

    await page.getByRole('button', { name: '預留退款' }).click()
    await page.getByLabel('退款金額（最小貨幣單位）', { exact: true }).fill('400')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認預留退款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認預留退款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const refundID = scalar('SELECT id FROM refund_operations WHERE grant_id=?', grantID)

    await page.goto(`${app.baseURL}/admin/credits/${grantID}`)
    await expect(amount('退款保留', 'USD 4.00')).toBeVisible()
    await expect(amount('已退款', 'USD 0.00')).toBeVisible()
    await expect(amount('可用額度', 'USD 6.00')).toBeVisible()
    await expect(page.getByText('仍有退款占用保留額')).toBeVisible()
    await page.goto(`${app.baseURL}/admin/refunds/${refundID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出退款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出退款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await page.goto(`${app.baseURL}/admin/credits/${grantID}`)
    await expect(amount('退款保留', 'USD 0.00')).toBeVisible()
    await expect(amount('已退款', 'USD 4.00')).toBeVisible()
    await expect(amount('可用額度', 'USD 6.00')).toBeVisible()
    await expect(page.getByText('仍有退款占用保留額')).toHaveCount(0)

    await page.getByRole('button', { name: '預留退款' }).click()
    await page.getByRole('button', { name: '執行另一個操作' }).click()
    await page.getByLabel('退款金額（最小貨幣單位）', { exact: true }).fill('300')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認預留退款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認預留退款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const failedRefundID = scalar('SELECT id FROM refund_operations WHERE grant_id=? ORDER BY rowid DESC LIMIT 1', grantID)
    expect(failedRefundID).not.toBe(refundID)
    await page.goto(`${app.baseURL}/admin/credits/${grantID}`)
    await expect(amount('退款保留', 'USD 3.00')).toBeVisible()
    await expect(amount('可用額度', 'USD 3.00')).toBeVisible()

    await page.goto(`${app.baseURL}/admin/lab/refund-decisions/${failedRefundID}`)
    await page.getByRole('combobox', { name: /結果/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('確定失敗', { exact: true }).click()
    await page.getByRole('button', { name: '確認退款結果' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認退款結果' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/refunds/${failedRefundID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出退款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出退款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('送出命令已完成，但退款操作確定失敗')).toBeVisible()
    expect(scalar('SELECT status FROM refund_operations WHERE id=?', failedRefundID)).toBe('definitively_failed')
    const failedDispatchCommandID = scalar("SELECT id FROM admin_commands WHERE action_id='C16' AND target_id=? ORDER BY rowid DESC LIMIT 1", failedRefundID)
    await page.goto(`${app.baseURL}/admin/commands/${failedDispatchCommandID}`)
    await expect(page.getByText('送出命令已完成，但退款操作確定失敗')).toBeVisible()
    await page.goto(`${app.baseURL}/admin/commands`)
    const failedDispatchRow = page.getByRole('row').filter({ hasText: failedDispatchCommandID })
    await expect(failedDispatchRow.getByText('退款失敗')).toBeVisible()
    await failedDispatchRow.getByRole('button', { name: '詳情' }).click()
    await expect(page.locator('.ant-drawer').getByText('送出命令已完成，但退款操作確定失敗')).toBeVisible()
    await page.goto(`${app.baseURL}/admin/credits/${grantID}`)
    await expect(amount('退款保留', 'USD 0.00')).toBeVisible()
    await expect(amount('已退款', 'USD 4.00')).toBeVisible()
    await expect(amount('可用額度', 'USD 6.00')).toBeVisible()
  })

  test('a second tab cannot complete the same refund dispatch', async ({ page }) => {
    await signIn(page)
    const { invoiceID } = await createPaidSubscription(page, 'same-refund-dispatch-' + randomUUID(), 'basic')
    await page.goto(app.baseURL + '/admin/invoices/' + invoiceID + '/reductions/new')
    await page.getByLabel('減額（最小貨幣單位）', { exact: true }).fill('1000')
    await page.getByLabel('減額理由', { exact: true }).fill('duplicate dispatch check')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認減額' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const grantID = scalar('SELECT id FROM credit_grants WHERE source_invoice_id=?', invoiceID)

    await page.goto(app.baseURL + '/admin/credits/' + grantID + '/refunds/new')
    await page.getByLabel('退款金額（最小貨幣單位）', { exact: true }).fill('400')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認預留退款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認預留退款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const refundID = scalar('SELECT id FROM refund_operations WHERE grant_id=?', grantID)
    const providerKey = scalar('SELECT provider_key FROM refund_operations WHERE id=?', refundID)

    const other = await page.context().newPage()
    try {
      await page.goto(app.baseURL + '/admin/refunds/' + refundID + '/dispatch')
      await other.goto(app.baseURL + '/admin/refunds/' + refundID + '/dispatch')
      await page.getByRole('button', { name: '建立預覽' }).click()
      await other.getByRole('button', { name: '建立預覽' }).click()
      await expect(other.getByRole('button', { name: '確認送出退款' })).toBeVisible()

      await page.getByRole('button', { name: '確認送出退款' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認送出退款' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

      const staleResponse = other.waitForResponse((response) =>
        response.url().endsWith('/admin/api/commands') &&
        response.request().method() === 'POST' &&
        (response.request().postData() ?? '').includes('"action_id":"C16"'),
      )
      await other.getByRole('button', { name: '確認送出退款' }).last().click()
      await other.getByRole('dialog').getByRole('button', { name: '確認送出退款' }).click()
      expect((await staleResponse).status()).toBe(409)
      await expect(other.getByText('原預覽已失效', { exact: true })).toBeVisible()
      await expect(other.getByText('無法建立預覽')).toBeVisible()
      await expect(other.getByRole('button', { name: '確認送出退款' })).toHaveCount(0)
      await expect(other.getByText('操作狀態已變更，原預覽不可再送出')).toBeVisible()
      await expect(other.getByText('created → succeeded')).toBeVisible()
      await expect(other.getByText('pending → done')).toBeVisible()
      await expect(other.getByText('觀測時間')).toBeVisible()

      expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C16' AND target_id=? AND status='succeeded'", refundID)).toBe(1)
      expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C16' AND target_id=? AND status='failed' AND error_code='PREVIEW_STALE'", refundID)).toBe(1)
      expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C16' AND c.target_id=?", refundID)).toBe(1)
      expect(scalar('SELECT status FROM refund_operations WHERE id=?', refundID)).toBe('succeeded')
      expect(scalar('SELECT COUNT(*) FROM refunds WHERE provider_key=?', providerKey, app.providerPath)).toBe('1')

      await page.goto(`${app.baseURL}/admin/refunds?id_prefix=${encodeURIComponent(refundID)}`)
      await page.getByRole('button', { name: '開啟操作' }).click()
      await expect(page).toHaveURL(`${app.baseURL}/admin/refunds/${refundID}`)
      await expect(page.getByRole('heading', { name: '退款操作詳情' })).toBeVisible()
      await expect(page.getByText('succeeded', { exact: true })).toBeVisible()
      await expect(page.getByRole('button', { name: '查證原操作' })).toBeVisible()
      await page.getByRole('button', { name: grantID }).click()
      await expect(page).toHaveURL(`${app.baseURL}/admin/credits/${grantID}`)
    } finally {
      await other.close()
    }
  })

  test('dispatching a selected refund does not send an earlier queued refund', async ({ page }) => {
    await signIn(page)
    const { invoiceID, operationID } = await createAcceptedSubscription(page, `queued-refund-${randomUUID()}`, 'basic')
    const sourceProviderKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', operationID)
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/reductions/new`)
    await page.getByLabel('減額（最小貨幣單位）', { exact: true }).fill('1000')
    await page.getByLabel('減額理由', { exact: true }).fill('queued refund dispatch')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認減額' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const grantID = scalar('SELECT id FROM credit_grants WHERE source_invoice_id=?', invoiceID)

    await page.goto(`${app.baseURL}/admin/credits/${grantID}/refunds/new`)
    for (const amount of ['300', '400']) {
      await page.getByLabel('退款金額（最小貨幣單位）', { exact: true }).fill(amount)
      await page.getByRole('button', { name: '建立預覽' }).click()
      await page.getByRole('button', { name: '確認預留退款' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認預留退款' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      if (amount === '300') await page.getByRole('button', { name: '執行另一個操作' }).click()
    }
    const firstRefundID = scalar('SELECT id FROM refund_operations WHERE grant_id=? ORDER BY rowid ASC LIMIT 1', grantID)
    const secondRefundID = scalar('SELECT id FROM refund_operations WHERE grant_id=? ORDER BY rowid DESC LIMIT 1', grantID)
    const firstProviderKey = scalar('SELECT provider_key FROM refund_operations WHERE id=?', firstRefundID)
    const secondProviderKey = scalar('SELECT provider_key FROM refund_operations WHERE id=?', secondRefundID)
    expect(firstRefundID).not.toBe(secondRefundID)
    expect(count("SELECT COUNT(*) FROM outbox WHERE kind='refund' AND object_id IN (?,?) AND status='pending'", firstRefundID, secondRefundID)).toBe(2)

    await page.goto(`${app.baseURL}/admin/refunds/${secondRefundID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出退款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出退款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    expect(scalar('SELECT status FROM refund_operations WHERE id=?', firstRefundID)).toBe('created')
    expect(scalar('SELECT status FROM refund_operations WHERE id=?', secondRefundID)).toBe('succeeded')
    expect(scalar('SELECT COUNT(*) FROM refunds WHERE provider_key=?', firstProviderKey, app.providerPath)).toBe('0')
    expect(scalar('SELECT COUNT(*) FROM refunds WHERE provider_key=?', secondProviderKey, app.providerPath)).toBe('1')
    expect(scalar('SELECT source_capture_key FROM refunds WHERE provider_key=?', secondProviderKey, app.providerPath)).toBe(sourceProviderKey)
    expect(scalar('SELECT amount_minor FROM refunds WHERE provider_key=?', secondProviderKey, app.providerPath)).toBe('400')
    expect(count("SELECT COUNT(*) FROM outbox WHERE kind='refund' AND object_id=? AND status='pending'", firstRefundID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C16' AND c.target_id=?", secondRefundID)).toBe(1)
  })

  test('concurrent refund reservation refreshes stale budget before confirmation', async ({ page }) => {
    await signIn(page)
    const { invoiceID } = await createPaidSubscription(page, `refund-stale-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/reductions/new`)
    await page.getByLabel('減額（最小貨幣單位）', { exact: true }).fill('1000')
    await page.getByLabel('減額理由', { exact: true }).fill('concurrent refund budget')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認減額' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const grantID = scalar('SELECT id FROM credit_grants WHERE source_invoice_id=?', invoiceID)
    const refundURL = `${app.baseURL}/admin/credits/${grantID}/refunds/new`
    await page.goto(refundURL)
    await page.getByLabel('退款金額（最小貨幣單位）', { exact: true }).fill('500')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByRole('button', { name: '確認預留退款' }).last()).toBeVisible()
    const other = await page.context().newPage()
    try {
      await other.goto(refundURL)
      await other.getByLabel('退款金額（最小貨幣單位）', { exact: true }).fill('500')
      await other.getByRole('button', { name: '建立預覽' }).click()
      await other.getByRole('button', { name: '確認預留退款' }).last().click()
      await other.getByRole('dialog').getByRole('button', { name: '確認預留退款' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(count('SELECT COUNT(*) FROM refund_operations WHERE grant_id=?', grantID)).toBe(1)
      const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
      await page.getByRole('button', { name: '確認預留退款' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認預留退款' }).click()
      const rejected = await staleResponse
      expect(rejected.status()).toBe(409)
      const originalKey = (await rejected.request().allHeaders())['idempotency-key']
      expect(originalKey).toBeTruthy()
      expect(count("SELECT COUNT(*) FROM admin_commands WHERE idempotency_key=? AND action_id='C15' AND status='failed' AND error_code='PREVIEW_STALE'", originalKey)).toBe(1)
      expect(count('SELECT COUNT(*) FROM refund_operations WHERE grant_id=?', grantID)).toBe(1)
      await expect(page.getByText('原預覽已失效，請檢查新預覽並再次確認')).toBeVisible()
      await page.getByRole('button', { name: '確認預留退款' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認預留退款' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(count('SELECT COUNT(*) FROM refund_operations WHERE grant_id=?', grantID)).toBe(2)
      expect(count('SELECT COALESCE(SUM(amount_minor),0) FROM refund_operations WHERE grant_id=?', grantID)).toBe(1000)
      expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C15' AND c.target_id=?", grantID)).toBe(2)
    } finally {
      await other.close()
    }
  })

  test('stale publish preview is re-created without publishing the rejected price', async ({ page, context }) => {
    await signIn(page)
    const priceID = `pro-browser-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/prices/pro/new`)
    for (const [label, value] of [
      ['價格版本 ID', priceID], ['版本號', '99'], ['固定金額（最小單位）', '5000'],
      ['每席金額（最小單位）', '1000'], ['包含任務量', '1000'],
      ['超額費率分子', '1'], ['超額費率分母', '100'],
      ['生效起點（UTC）', '2026-10-01T00:00:00Z'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByRole('button', { name: '確認發布價格' })).toBeVisible()

    const clock = await context.newPage()
    await clock.goto(`${app.baseURL}/admin/lab/clock`)
    await clock.getByRole('combobox', { name: /模式/ }).click()
    await clock.getByText('固定時間', { exact: true }).click()
    await clock.getByRole('textbox', { name: /固定 UTC 時間/ }).fill('2026-09-26T18:00:00Z')
    await clock.getByRole('button', { name: '確認設定時鐘' }).click()
    await clock.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
    await expect(clock.getByText('succeeded')).toBeVisible()
    await clock.close()

    await page.getByRole('button', { name: '確認發布價格' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認發布價格' }).click()
    await expect(page.getByText('原預覽已失效', { exact: true })).toBeVisible()
    await expect(page.getByText('原預覽已失效，請檢查新預覽並再次確認')).toBeVisible()
    await expect(page.getByRole('button', { name: '確認發布價格' }).last()).toBeEnabled()
    expect(count('SELECT COUNT(*) FROM price_versions WHERE id=?', priceID)).toBe(0)
  })

  test('midperiod upgrade previews USD 40.00 and keeps service on the old price until paid', async ({ page }) => {
    await signIn(page)
    const customerID = `upgrade-browser-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=?', customerID)
    const clockResponse = await page.request.get(`${app.baseURL}/admin/api/lab/clock`)
    const clock = await clockResponse.json() as { business_time: string }
    const quoteExpiry = BigInt(scalar('SELECT expires_at FROM quotes WHERE id=?', quoteID))
    expect(quoteExpiry).toBeGreaterThan(BigInt(Date.parse(clock.business_time)) * 1_000_000n)
    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await page.getByRole('button', { name: '預覽接受' }).click()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const subscriptionID = scalar('SELECT id FROM subscriptions WHERE quote_id=?', quoteID)
    const invoiceID = scalar('SELECT id FROM invoices WHERE subscription_id=?', subscriptionID)
    const operationID = scalar('SELECT id FROM payment_operations WHERE invoice_id=?', invoiceID)
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/reconcile`)
    await page.getByRole('button', { name: '確認查證' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認查證' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const start = BigInt(scalar('SELECT period_start FROM billing_periods WHERE subscription_id=?', subscriptionID))
    const end = BigInt(scalar('SELECT period_end FROM billing_periods WHERE subscription_id=?', subscriptionID))
    const midpoint = new Date(Number(((start + end) / 2n) / 1_000_000n)).toISOString()
    await page.goto(`${app.baseURL}/admin/lab/clock`)
    await page.getByRole('combobox', { name: /模式/ }).click()
    await page.getByText('固定時間', { exact: true }).click()
    await page.getByRole('textbox', { name: /固定 UTC 時間/ }).fill(midpoint)
    await page.getByRole('button', { name: '確認設定時鐘' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const revision = scalar('SELECT revision FROM subscriptions WHERE id=?', subscriptionID)
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('button', { name: '建立另一筆報價' }).click()
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
    await page.getByRole('textbox', { name: /席次/ }).fill('5')
    await page.getByRole('checkbox', { name: '這是現有訂閱的變更報價' }).check()
    await page.getByRole('textbox', { name: /訂閱 ID/ }).fill(subscriptionID)
    await page.getByRole('combobox', { name: /變更方式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('立即升級', { exact: true }).click()
    await page.getByRole('textbox', { name: /目前 Revision/ }).fill(revision)
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const changeQuoteID = scalar("SELECT quote_id FROM change_quote_bindings WHERE subscription_id=? AND mode='immediate'", subscriptionID)
    await page.goto(`${app.baseURL}/admin/quotes/${changeQuoteID}/accept`)
    await expect(page.getByText('這是現有訂閱的變更報價')).toBeVisible()
    await expect(page.getByRole('button', { name: '預覽接受' })).toHaveCount(0)
    await expect(page.getByRole('row', { name: /報價接受時現在應付/ })).toContainText('待立即升級預覽估算')
    await expect(page.getByRole('row', { name: /下一整期固定承諾/ })).toContainText('USD 100.00')
    const sessionResponse = await page.request.get(`${app.baseURL}/admin/api/session`)
    const session = await sessionResponse.json() as { csrf_token: string }
    const changeQuoteResponse = await page.request.get(`${app.baseURL}/admin/api/quotes/${changeQuoteID}`)
    const changeQuote = await changeQuoteResponse.json() as { Fingerprint: string; DueNowMinor: null }
    expect(changeQuote.DueNowMinor).toBeNull()
    const wrongPurchase = await page.request.post(`${app.baseURL}/admin/api/previews`, {
      headers: { Origin: app.baseURL, 'X-CSRF-Token': session.csrf_token },
      data: { action_id: 'C02', target_id: changeQuoteID, payload: { fingerprint: changeQuote.Fingerprint } },
    })
    expect(wrongPurchase.status()).toBe(409)
    expect(count('SELECT COUNT(*) FROM subscriptions WHERE quote_id=?', changeQuoteID)).toBe(0)
    await page.getByRole('button', { name: '前往立即升級' }).click()
    await page.getByRole('button', { name: '預覽立即升級' }).click()
    await expect(page.getByText('USD 40.00')).toBeVisible()
    await expect(page.getByText('本期升級預估應付上限')).toBeVisible()
    await expect(page.getByText('估算時刻')).toBeVisible()
    await expect(page.getByText('提交時重新計算；若應付金額增加，需重新預覽確認。')).toBeVisible()
    await page.getByRole('button', { name: '確認升級' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認升級' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await expect(page.getByRole('row', { name: /預覽估算金額/ })).toContainText('USD 40.00')
    await expect(page.getByRole('row', { name: /實際待付款義務/ })).toContainText('USD 40.00')
    await page.getByRole('link', { name: '開啟命令頁面' }).click()
    await expect(page.getByRole('row', { name: /預覽估算金額/ })).toContainText('USD 40.00')
    await expect(page.getByRole('row', { name: /實際待付款義務/ })).toContainText('USD 40.00')
    expect(count('SELECT quoted_amount_minor FROM immediate_changes WHERE subscription_id=?', subscriptionID)).toBe(4000)
    expect(scalar('SELECT price_version_id FROM subscriptions WHERE id=?', subscriptionID)).toBe('basic-v1')
    const changeID = scalar('SELECT id FROM immediate_changes WHERE subscription_id=?', subscriptionID)
    const changeOperationID = scalar('SELECT operation_id FROM immediate_changes WHERE id=?', changeID)
    await page.goto(`${app.baseURL}/admin/lab/faults/${changeOperationID}`)
    await page.getByRole('combobox', { name: /操作種類/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('付款', { exact: true }).click()
    await page.getByRole('combobox', { name: /故障模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('回應遺失', { exact: true }).click()
    await page.getByRole('button', { name: '確認故障票據' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認故障票據' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/payments/${changeOperationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByText('waiting_verification')).toBeVisible()
    const dispatchID = scalar('SELECT claimed_command_id FROM admin_fault_tickets WHERE operation_id=?', changeOperationID)
    expect(scalar('SELECT price_version_id FROM subscriptions WHERE id=?', subscriptionID)).toBe('basic-v1')
    expect(scalar('SELECT status FROM immediate_changes WHERE id=?', changeID)).toBe('requested')

    const twoDaysLater = new Date(Date.parse(midpoint) + 2 * 24 * 60 * 60 * 1000).toISOString()
    await page.goto(`${app.baseURL}/admin/lab/clock`)
    await page.getByRole('button', { name: '執行另一個操作' }).click()
    await page.getByRole('combobox', { name: /模式/ }).click()
    await page.getByText('固定時間', { exact: true }).click()
    await page.getByRole('textbox', { name: /固定 UTC 時間/ }).fill(twoDaysLater)
    await page.getByRole('button', { name: '確認設定時鐘' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/commands/${dispatchID}`)
    const resume = page.getByRole('button', { name: '重新查證' })
    await expect(resume).toBeVisible()
    await resume.click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(scalar('SELECT price_version_id FROM subscriptions WHERE id=?', subscriptionID)).toBe('pro-v1')
    expect(scalar('SELECT actual_amount_minor FROM immediate_changes WHERE id=?', changeID)).toBe('3466')
    expect(scalar('SELECT correction_minor FROM immediate_changes WHERE id=?', changeID)).toBe('534')
    await page.goto(`${app.baseURL}/admin/jobs/change-corrections`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    let correctionKey = ''
    let droppedCorrectionResponse = false
    await page.route('**/admin/api/commands', async (route) => {
      if (!droppedCorrectionResponse && route.request().method() === 'POST') {
        droppedCorrectionResponse = true
        correctionKey = route.request().headers()['idempotency-key']
        await commitThenDropResponse(page, route)
      } else {
        await route.continue()
      }
    })
    await page.getByRole('button', { name: '確認執行更正' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認執行更正' }).click()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    expect(correctionKey).toBeTruthy()
    const correctionCommandID = scalar('SELECT id FROM admin_commands WHERE idempotency_key=?', correctionKey)
    const correctionJobID = `job:${correctionCommandID}`
    expect(count('SELECT COUNT(*) FROM admin_jobs WHERE id=?', correctionJobID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_job_items WHERE job_id=? AND target_id=?', correctionJobID, changeID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM corrections WHERE request_key=?', `change-correction:${changeID}`)).toBe(1)
    await page.reload()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    const correctionReplay = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST')
    await page.getByRole('button', { name: '用原 request key 查詢' }).click()
    expect((await correctionReplay).headers()['idempotency-key']).toBe(correctionKey)
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE id=?', correctionCommandID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_jobs WHERE id=?', correctionJobID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_job_items WHERE job_id=? AND target_id=?', correctionJobID, changeID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', correctionCommandID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM corrections WHERE request_key=?', `change-correction:${changeID}`)).toBe(1)
    expect(scalar('SELECT reduction_minor FROM corrections WHERE request_key=?', `change-correction:${changeID}`)).toBe('534')
    const changeInvoiceID = scalar('SELECT invoice_id FROM immediate_changes WHERE id=?', changeID)
    await page.goto(`${app.baseURL}/admin/invoices/${changeInvoiceID}`)
    const delayedCorrection = page.getByRole('row').filter({ hasText: 'delayed Pro activation' })
    await expect(delayedCorrection.getByText('延遲開通更正', { exact: true })).toBeVisible()
    await expect(delayedCorrection.getByText(changeID, { exact: true })).toBeVisible()
    await expect(delayedCorrection.getByText('USD 5.34', { exact: true })).toBeVisible()
    await page.goto(`${app.baseURL}/admin/lab/clock`)
    await page.getByRole('button', { name: '執行另一個操作' }).click()
    await page.getByRole('combobox', { name: /模式/ }).click()
    await page.getByText('實際時間', { exact: true }).click()
    await page.getByRole('button', { name: '確認設定時鐘' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
  })

  test('unfulfilled upgrade exposes its correction and credit provenance in the invoice', async ({ page }) => {
    await signIn(page)
    const customerID = `unfulfilled-browser-${randomUUID()}`
    const succeeded = () => expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const runPreviewAction = async (path: string, label: string) => {
      await page.goto(`${app.baseURL}${path}`)
      await page.getByRole('button', { name: '建立預覽' }).click()
      await page.getByRole('button', { name: label }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: label }).click()
      await succeeded()
    }
    const setClock = async (time: string | null) => {
      await page.goto(`${app.baseURL}/admin/lab/clock`)
      const another = page.getByRole('button', { name: '執行另一個操作' })
      const mode = page.getByRole('combobox', { name: /模式/ })
      await expect.poll(async () => (await another.isVisible()) || (await mode.isEnabled())).toBe(true)
      if (await another.isVisible()) await another.click()
      await expect(mode).toBeEnabled()
      await mode.click()
      await page.getByText(time === null ? '實際時間' : '固定時間', { exact: true }).click()
      if (time !== null) await page.getByRole('textbox', { name: /固定 UTC 時間/ }).fill(time)
      await page.getByRole('button', { name: '確認設定時鐘' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
      await succeeded()
    }

    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    await page.getByRole('button', { name: '建立報價' }).click()
    await succeeded()
    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=?', customerID)
    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await page.getByRole('button', { name: '預覽接受' }).click()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await succeeded()
    const subscriptionID = scalar('SELECT id FROM subscriptions WHERE quote_id=?', quoteID)
    const invoiceID = scalar('SELECT id FROM invoices WHERE subscription_id=?', subscriptionID)
    const operationID = scalar('SELECT id FROM payment_operations WHERE invoice_id=?', invoiceID)
    await runPreviewAction(`/admin/payments/${operationID}/dispatch`, '確認送出付款')
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/reconcile`)
    await page.getByRole('button', { name: '確認查證' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認查證' }).click()
    await succeeded()

    const start = BigInt(scalar('SELECT period_start FROM billing_periods WHERE subscription_id=?', subscriptionID))
    const end = BigInt(scalar('SELECT period_end FROM billing_periods WHERE subscription_id=?', subscriptionID))
    await setClock(new Date(Number(((start + end) / 2n) / 1_000_000n)).toISOString())
    const revision = scalar('SELECT revision FROM subscriptions WHERE id=?', subscriptionID)
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('button', { name: '建立另一筆報價' }).click()
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
    await page.getByRole('textbox', { name: /席次/ }).fill('5')
    await page.getByRole('checkbox', { name: '這是現有訂閱的變更報價' }).check()
    await page.getByRole('textbox', { name: /訂閱 ID/ }).fill(subscriptionID)
    await page.getByRole('combobox', { name: /變更方式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('立即升級', { exact: true }).click()
    await page.getByRole('textbox', { name: /目前 Revision/ }).fill(revision)
    await page.getByRole('button', { name: '建立報價' }).click()
    await succeeded()
    await page.getByRole('button', { name: '前往立即升級' }).click()
    await page.getByRole('button', { name: '預覽立即升級' }).click()
    await page.getByRole('button', { name: '確認升級' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認升級' }).click()
    await succeeded()
    const changeID = scalar('SELECT id FROM immediate_changes WHERE subscription_id=?', subscriptionID)
    const changeOperationID = scalar('SELECT operation_id FROM immediate_changes WHERE id=?', changeID)
    const changeInvoiceID = scalar('SELECT invoice_id FROM immediate_changes WHERE id=?', changeID)
    await page.goto(`${app.baseURL}/admin/lab/faults/${changeOperationID}`)
    await page.getByRole('combobox', { name: /操作種類/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('付款', { exact: true }).click()
    await page.getByRole('combobox', { name: /故障模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('回應遺失', { exact: true }).click()
    await page.getByRole('button', { name: '確認故障票據' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認故障票據' }).click()
    await succeeded()
    await page.goto(`${app.baseURL}/admin/payments/${changeOperationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByText('waiting_verification')).toBeVisible()

    await setClock(new Date(Number(end / 1_000_000n) + 1_000).toISOString())
    await runPreviewAction('/admin/jobs/renewals', '確認續約批次')
    await page.goto(`${app.baseURL}/admin/payments/${changeOperationID}/reconcile`)
    await page.getByRole('button', { name: '確認查證' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認查證' }).click()
    await succeeded()
    expect(scalar('SELECT status FROM immediate_changes WHERE id=?', changeID)).toBe('needs_review')
    await page.goto(`${app.baseURL}/admin/changes/${changeID}/resolve-unfulfilled`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    let resolutionKey = ''
    let droppedResolutionResponse = false
    await page.route('**/admin/api/commands', async (route) => {
      if (!droppedResolutionResponse && route.request().method() === 'POST') {
        droppedResolutionResponse = true
        resolutionKey = route.request().headers()['idempotency-key']
        await commitThenDropResponse(page, route)
      } else {
        await route.continue()
      }
    })
    await page.getByRole('button', { name: '確認建立更正' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認建立更正' }).click()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    expect(resolutionKey).toBeTruthy()
    const resolutionCommandID = scalar('SELECT id FROM admin_commands WHERE idempotency_key=?', resolutionKey)
    expect(count('SELECT COUNT(*) FROM immediate_change_resolutions WHERE change_id=?', changeID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?', changeInvoiceID)).toBe(1)
    await page.reload()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    const resolutionReplay = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST')
    await page.getByRole('button', { name: '用原 request key 查詢' }).click()
    expect((await resolutionReplay).headers()['idempotency-key']).toBe(resolutionKey)
    await succeeded()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE id=?', resolutionCommandID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', resolutionCommandID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM immediate_change_resolutions WHERE change_id=?', changeID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?', changeInvoiceID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM immediate_change_resolutions WHERE change_id=?', changeID)).toBe(1)
    const correctionID = scalar('SELECT correction_id FROM immediate_change_resolutions WHERE change_id=?', changeID)
    await page.goto(`${app.baseURL}/admin/invoices/${changeInvoiceID}`)
    const correction = page.getByRole('row').filter({ hasText: correctionID })
    await expect(correction.getByText('未履行升級決議', { exact: true })).toBeVisible()
    await expect(correction.getByText(changeID, { exact: true })).toBeVisible()
    await expect(correction.getByText('USD 40.00', { exact: true }).first()).toBeVisible()
    const grants = page.locator('.ant-card').filter({ has: page.locator('.ant-card-head-title').getByText('本帳單釋出的 Credit', { exact: true }) })
    await expect(grants.getByRole('row').filter({ hasText: correctionID }).getByText('USD 40.00', { exact: true })).toBeVisible()
    await page.locator('.ant-card').filter({ has: page.locator('.ant-card-head-title').getByText('減額更正', { exact: true }) }).getByRole('button', { name: '查看完整歷史' }).click()
    await expect(page).toHaveURL(new RegExp(`/admin/invoices/${changeInvoiceID}/history/corrections$`))
    await expect(page.getByRole('row').filter({ hasText: correctionID }).getByText('未履行升級決議', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '返回帳單詳情' }).click()
    await grants.getByRole('button', { name: '查看完整歷史' }).click()
    await expect(page).toHaveURL(new RegExp(`/admin/invoices/${changeInvoiceID}/history/grants$`))
    const grantHistory = page.locator('.form-page').filter({ has: page.getByRole('heading', { name: '釋出 Credit 歷史' }) })
    const grantRow = grantHistory.getByRole('row').filter({ hasText: correctionID })
    await expect(grantRow).toHaveCount(1)
    await expect(grantRow.getByRole('cell', { name: 'USD 40.00', exact: true })).toBeVisible()
    await setClock(null)
  })

  test('partial payment replaces an unsent operation and retries only definitive failure', async ({ page }) => {
    await signIn(page)
    const customerID = `partial-browser-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
    await page.getByRole('textbox', { name: /方案 ID/ }).fill('basic')
    await page.getByRole('button', { name: '建立報價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=?', customerID)
    await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
    await page.getByRole('button', { name: '預覽接受' }).click()
    await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const invoiceID = scalar('SELECT i.id FROM invoices i JOIN subscriptions s ON s.id=i.subscription_id WHERE s.quote_id=?', quoteID)
    const originalOperationID = scalar('SELECT id FROM payment_operations WHERE invoice_id=?', invoiceID)

    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/payments/new`)
    await page.getByRole('textbox', { name: /付款金額/ }).fill('1000')
    await page.getByRole('button', { name: '預覽付款' }).click()
    await page.getByRole('button', { name: '確認建立付款' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認建立' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const partialOperationID = scalar('SELECT id FROM payment_operations WHERE invoice_id=? AND amount_minor=1000', invoiceID)
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', originalOperationID)).toBe('cancelled')
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', partialOperationID)).toBe('created')

    await page.goto(`${app.baseURL}/admin/lab/payment-decisions/${partialOperationID}`)
    await page.getByRole('combobox', { name: /結果/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('確定失敗', { exact: true }).click()
    await page.getByRole('button', { name: '確認付款結果' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認付款結果' }).click()
    await expect(page.getByRole('dialog', { name: '確認付款結果' })).toHaveCount(0)
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/payments/${partialOperationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('送出命令已完成，但付款操作確定失敗')).toBeVisible()
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', partialOperationID)).toBe('definitively_failed')

    await page.goto(`${app.baseURL}/admin/payments/${partialOperationID}/retry`)
    await page.getByRole('button', { name: '預覽重試' }).click()
    await page.getByRole('button', { name: '確認重試' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '建立重試' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const retryOperationID = scalar("SELECT id FROM payment_operations WHERE invoice_id=? AND status='created'", invoiceID)
    expect(retryOperationID).not.toBe(partialOperationID)
    expect(count('SELECT amount_minor FROM payment_operations WHERE id=?', retryOperationID)).toBe(2000)
    expect(count('SELECT COUNT(*) FROM payment_retry_requests WHERE invoice_id=?', invoiceID)).toBe(1)
  })

  test('reconcile keeps a submitted payment waiting until provider evidence appears', async ({ page }) => {
    await signIn(page)
    const { operationID } = await createAcceptedSubscription(page, `verify-wait-${randomUUID()}`, 'basic')
    const providerKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', operationID)
    const amount = scalar('SELECT amount_minor FROM payment_operations WHERE id=?', operationID)
    const currency = scalar('SELECT currency FROM payment_operations WHERE id=?', operationID)
    const markSubmitted = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("UPDATE payment_operations SET status=\'submitted\' WHERE id=?",(sys.argv[2],)); db.commit()'
    execFileSync('python3', ['-c', markSubmitted, app.commercePath, operationID])

    await page.goto(`${app.baseURL}/admin/payments/${operationID}/reconcile`)
    await page.getByRole('button', { name: '確認查證' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認查證' }).click()
    await expect(page.getByText('waiting_verification', { exact: true }).first()).toBeVisible()
    const commandID = scalar('SELECT id FROM admin_commands WHERE action_id=? ORDER BY rowid DESC LIMIT 1', 'C10')
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(0)
    expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath))).toBe(0)

    const insertCapture = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES(?,?,?,\'succeeded\')",(sys.argv[2],int(sys.argv[3]),sys.argv[4])); db.commit()'
    execFileSync('python3', ['-c', insertCapture, app.providerPath, providerKey, amount, currency])
    await page.getByRole('button', { name: '重新查證' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('succeeded')
    expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath))).toBe(1)
  })

  test('command pages use a stable cursor when a newer command arrives', async ({ page }) => {
    await signIn(page)
    const sessionResponse = await page.request.get(`${app.baseURL}/admin/api/session`)
    const session = await sessionResponse.json() as { csrf_token: string }
    async function createClockCommand() {
      return submitClockControl(page, app.baseURL, session.csrf_token, 'real')
    }
    for (let i = 0; i < 21; i++) await createClockCommand()
    const firstResponse = await page.request.get(`${app.baseURL}/admin/api/commands?limit=5`)
    expect(firstResponse.status()).toBe(200)
    const first = await firstResponse.json() as { items: Array<{ id: string }>; next_cursor: string }
    expect(first.items).toHaveLength(5)
    expect(first.next_cursor).not.toBe('')
    const latest = await createClockCommand()
    const secondResponse = await page.request.get(`${app.baseURL}/admin/api/commands?limit=5&cursor=${first.next_cursor}`)
    const second = await secondResponse.json() as { items: Array<{ id: string }> }
    expect(second.items).toHaveLength(5)
    expect(second.items.some((item) => first.items.some((old) => old.id === item.id))).toBe(false)
    expect(second.items.some((item) => item.id === latest.id)).toBe(false)
    expect((await page.request.get(`${app.baseURL}/admin/api/commands?cursor=invalid`)).status()).toBe(400)

    await page.goto(`${app.baseURL}/admin/commands`)
    await expect(page.getByText('第 1 頁')).toBeVisible()
    await expect(page.getByRole('row')).toHaveCount(21)
    await page.getByRole('button', { name: '下一頁' }).click()
    await expect(page.getByText('第 2 頁')).toBeVisible()
    await expect(page.getByRole('button', { name: '上一頁' })).toBeEnabled()
  })

  test('resource list shows missing entitlement separately from real period zero', async ({ page }) => {
    await signIn(page)
    const { subscriptionID, invoiceID } = await createAcceptedSubscription(page, `unknown-list-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/subscriptions?id_prefix=${encodeURIComponent(subscriptionID)}`)
    await expect(page.getByRole('columnheader', { name: 'EntitlementStatus' })).toBeVisible()
    const subscriptionRow = page.getByRole('row').filter({ hasText: subscriptionID })
    await expect(subscriptionRow.getByRole('cell').nth(3)).toHaveText('未知')
    await page.goto(`${app.baseURL}/admin/invoices?id_prefix=${encodeURIComponent(invoiceID)}`)
    await expect(page.getByRole('columnheader', { name: 'PeriodIndex' })).toBeVisible()
    const invoiceRow = page.getByRole('row').filter({ hasText: invoiceID })
    await expect(invoiceRow.getByRole('cell').nth(2)).toHaveText('0')
  })

  test('resource filters keep their cursor and page position in the URL', async ({ page }) => {
    await signIn(page)
    const customerA = `list-filter-a-${randomUUID()}`
    const customerB = `list-filter-b-${randomUUID()}`
    const sessionResponse = await page.request.get(`${app.baseURL}/admin/api/session`)
    const session = await sessionResponse.json() as { csrf_token: string }
    for (const [customerID, count] of [[customerA, 21], [customerB, 1]] as const) {
      for (let i = 0; i < count; i++) {
        const response = await page.request.post(`${app.baseURL}/admin/api/commands`, {
          headers: { Origin: app.baseURL, 'Idempotency-Key': randomUUID(), 'X-CSRF-Token': session.csrf_token },
          data: { action_id: 'C01', target_id: '', payload: { customer_id: customerID, plan_id: 'basic' } },
        })
        expect(response.ok(), `${response.status()} ${await response.text()}`).toBe(true)
      }
    }
    await page.goto(`${app.baseURL}/admin/quotes?customer_id=${encodeURIComponent(customerA)}`)
    await expect(page.getByRole('row').filter({ hasText: customerA })).toHaveCount(20)
    const inserted = await page.request.post(`${app.baseURL}/admin/api/commands`, {
      headers: { Origin: app.baseURL, 'Idempotency-Key': randomUUID(), 'X-CSRF-Token': session.csrf_token },
      data: { action_id: 'C01', target_id: '', payload: { customer_id: customerA, plan_id: 'basic' } },
    })
    expect(inserted.ok(), `${inserted.status()} ${await inserted.text()}`).toBe(true)
    await page.getByRole('button', { name: '下一頁' }).click()
    await expect(page).toHaveURL(/cursor=/)
    await expect(page.getByText('第 2 頁')).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: customerA })).toHaveCount(2)
    const secondPageURL = page.url()
    await page.reload()
    expect(page.url()).toBe(secondPageURL)
    await expect(page.getByRole('row').filter({ hasText: customerA })).toHaveCount(2)
    await page.getByRole('button', { name: '上一頁' }).click()
    await expect(page.getByRole('row').filter({ hasText: customerA })).toHaveCount(20)
    await page.getByLabel('客戶 ID', { exact: true }).fill(customerB)
    await page.getByRole('button', { name: '套用篩選' }).click()
    await expect(page).toHaveURL(new RegExp(`customer_id=${encodeURIComponent(customerB)}`))
    await expect(page.getByRole('row').filter({ hasText: customerB })).toHaveCount(1)
    await expect(page.getByRole('button', { name: '上一頁' })).toBeDisabled()
    expect((await page.request.get(`${app.baseURL}/admin/api/quotes?status=active`)).status()).toBe(400)
    await page.goto(`${app.baseURL}/admin/subscriptions`)
    await page.getByLabel('建立時間起（UTC）').fill('2100-01-01T00:00:00Z')
    await page.getByRole('button', { name: '套用篩選' }).click()
    await expect(page).toHaveURL(/created_from=2100-01-01T00%3A00%3A00Z/)
    await expect(page.getByText('目前沒有資料')).toBeVisible()
  })

  test('resource UTC filters reject lost nanosecond precision before querying', async ({ page }) => {
    await signIn(page)
    await page.goto(`${app.baseURL}/admin/subscriptions`)
    let invalidRequests = 0
    page.on('request', (request) => {
      if (request.method() === 'GET' && request.url().includes('/admin/api/subscriptions') && request.url().includes('1234567891')) invalidRequests += 1
    })
    const lower = page.getByLabel('建立時間起（UTC）')
    const upper = page.getByLabel('建立時間前（UTC）')
    await lower.fill('2026-01-01T00:00:00.1234567891Z')
    await page.getByRole('button', { name: '套用篩選' }).click()
    await expect(page.locator('#created_from_help')).toHaveText('請輸入有效 UTC 時間（最多 9 位小數秒）')
    await expect(lower).toHaveAttribute('aria-invalid', 'true')
    expect(new URL(page.url()).searchParams.has('created_from')).toBe(false)
    await lower.fill('2026-01-01T00:00:00.123456789Z')
    await upper.fill('2027-01-01T00:00:00.1234567891Z')
    await page.getByRole('button', { name: '套用篩選' }).click()
    await expect(page.locator('#created_before_help')).toHaveText('請輸入有效 UTC 時間（最多 9 位小數秒）')
    await expect(upper).toHaveAttribute('aria-invalid', 'true')
    expect(new URL(page.url()).searchParams.has('created_before')).toBe(false)
    await upper.fill('2027-01-01T00:00:00.123456789Z')
    await page.getByRole('button', { name: '套用篩選' }).click()
    await expect(page).toHaveURL(/created_from=/)
    expect(new URL(page.url()).searchParams.get('created_from')).toBe('2026-01-01T00:00:00.123456789Z')
    expect(new URL(page.url()).searchParams.get('created_before')).toBe('2027-01-01T00:00:00.123456789Z')
    expect(invalidRequests).toBe(0)
  })

  test('price and contract component boundaries reject invalid values before preview', async ({ page }) => {
    await signIn(page)
    const initialCommands = count('SELECT COUNT(*) FROM admin_commands WHERE action_id IN (?, ?)', 'C18', 'C20')
    let previewRequests = 0
    page.on('request', (request) => {
      if (request.method() === 'POST' && request.url().endsWith('/admin/api/previews')) previewRequests += 1
    })
    const effective = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000).toISOString().replace(/\.\d{3}Z$/, 'Z')
    const cases: Array<{ route: string; fields: Array<[string, string]>; numeric: string[]; positive: string[] }> = [
      {
        route: '/admin/prices/pro/new',
        fields: [
          ['價格版本 ID', `pro_boundary_${randomUUID()}`], ['版本號', '7'],
          ['固定金額（最小單位）', '100'], ['每席金額（最小單位）', '1'],
          ['包含任務量', '0'], ['超額費率分子', '0'], ['超額費率分母', '1'],
          ['生效起點（UTC）', effective],
        ],
        numeric: ['版本號', '固定金額（最小單位）', '每席金額（最小單位）', '包含任務量', '超額費率分子', '超額費率分母'],
        positive: ['版本號', '固定金額（最小單位）', '每席金額（最小單位）', '超額費率分母'],
      },
      {
        route: '/admin/prices/metered/new',
        fields: [
          ['價格版本 ID', `metered_boundary_${randomUUID()}`], ['方案 ID', 'boundary_plan'],
          ['版本號', '7'], ['固定金額（最小單位）', '100'], ['每席金額（最小單位）', '0'],
          ['計量表 ID', 'boundary_meter'], ['包含用量', '1'],
          ['超額費率分子', '1'], ['超額費率分母', '1'], ['生效起點（UTC）', effective],
        ],
        numeric: ['版本號', '固定金額（最小單位）', '每席金額（最小單位）', '包含用量', '超額費率分子', '超額費率分母'],
        positive: ['版本號', '固定金額（最小單位）', '包含用量', '超額費率分子', '超額費率分母'],
      },
    ]
    for (const testCase of cases) {
      await page.goto(`${app.baseURL}${testCase.route}`)
      for (const [label, value] of testCase.fields) await page.getByLabel(label, { exact: true }).fill(value)
      for (const label of testCase.numeric) {
        const field = page.getByLabel(label, { exact: true })
        const original = testCase.fields.find(([name]) => name === label)![1]
        await field.fill('9223372036854775808')
        await page.getByRole('button', { name: '建立預覽' }).click()
        await expect(page.getByText(`${label}不可超過 int64 上限`)).toBeVisible()
        await field.fill(original)
        await expect(page.getByText(`${label}不可超過 int64 上限`)).toHaveCount(0)
      }
      for (const label of testCase.positive) {
        const field = page.getByLabel(label, { exact: true })
        const original = testCase.fields.find(([name]) => name === label)![1]
        await field.fill('0')
        await page.getByRole('button', { name: '建立預覽' }).click()
        await expect(page.getByText(`${label}格式不正確`)).toBeVisible()
        await field.fill(original)
        await expect(page.getByText(`${label}格式不正確`)).toHaveCount(0)
      }
      const fixedField = page.getByLabel('固定金額（最小單位）', { exact: true })
      const seatField = page.getByLabel('每席金額（最小單位）', { exact: true })
      const originalSeat = testCase.fields.find(([name]) => name === '每席金額（最小單位）')![1]
      await fixedField.fill('9223372036854775807')
      await seatField.fill('1')
      await page.getByRole('button', { name: '建立預覽' }).click()
      await expect(page.getByText('固定金額加上一席費用不可超過 int64 上限')).toBeVisible()
      await expect(seatField).toHaveAttribute('aria-invalid', 'true')
      await expect(seatField).toHaveAttribute('aria-describedby', /.+/)
      expect(previewRequests).toBe(0)
      await fixedField.fill('100')
      await seatField.fill(originalSeat)
      await expect(page.getByText('固定金額加上一席費用不可超過 int64 上限')).toHaveCount(0)
      await expect(seatField).not.toHaveAttribute('aria-invalid', 'true')
      const effectiveField = page.getByLabel('生效起點（UTC）', { exact: true })
      await effectiveField.fill('2026-02-30T00:00:00Z')
      await page.getByRole('button', { name: '建立預覽' }).click()
      await expect(page.getByText(/生效起點（UTC）必須是有效 UTC 時間/)).toBeVisible()
      expect(previewRequests).toBe(0)
    }
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id IN (?, ?)', 'C18', 'C20')).toBe(initialCommands)
    const initialContractCommands = count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C31'")
    await page.goto(`${app.baseURL}/admin/contracts/new`)
    const contractEnd = new Date(Date.parse(effective) + 60 * 24 * 60 * 60 * 1000).toISOString().replace(/\.\d{3}Z$/, 'Z')
    for (const [label, value] of [
      ['合約版本 ID', `contract_boundary_${randomUUID()}`],
      ['客戶 ID', `contract_boundary_customer_${randomUUID()}`],
      ['版本序號', '1'],
      ['基礎價格版本 ID', 'pro-v1'],
      ['固定金額', '9223372036854775807'],
      ['每席金額', '1'],
      ['生效起點（UTC）', effective],
      ['生效終點（UTC）', contractEnd],
    ]) {
      await page.getByLabel(label, { exact: true }).fill(value)
    }
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('固定金額加上一席費用不可超過 int64 上限')).toBeVisible()
    expect(previewRequests).toBe(0)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C31'")).toBe(initialContractCommands)
  })

  test('price publication preserves a minor amount above the JS safe integer', async ({ page }) => {
    await signIn(page)
    const priceID = `pro_large_minor_${randomUUID().replaceAll('-', '')}`
    const fixedMinor = '9007199254740993'
    const version = String(count('SELECT COALESCE(MAX(version), 0) FROM price_versions WHERE plan_id=?', 'pro') + 1)
    const effective = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000).toISOString().replace(/\.\d{3}Z$/, 'Z')
    const before = count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C18'")
    let previewRequests = 0
    page.on('request', (request) => {
      if (request.method() === 'POST' && request.url().endsWith('/admin/api/previews')) previewRequests += 1
    })

    await page.goto(`${app.baseURL}/admin/prices/pro/new`)
    for (const [label, value] of [
      ['價格版本 ID', priceID],
      ['版本號', version],
      ['固定金額（最小單位）', fixedMinor],
      ['每席金額（最小單位）', '1'],
      ['包含任務量', '0'],
      ['超額費率分子', '1'],
      ['超額費率分母', '1'],
      ['生效起點（UTC）', effective],
    ] as Array<[string, string]>) await page.getByLabel(label, { exact: true }).fill(value)

    const fixedField = page.getByLabel('固定金額（最小單位）', { exact: true })
    await fixedField.fill('9e15')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('固定金額（最小單位）格式不正確')).toBeVisible()
    await fixedField.fill('-1')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('固定金額（最小單位）格式不正確')).toBeVisible()
    await fixedField.fill(fixedMinor)
    const denominator = page.getByLabel('超額費率分母', { exact: true })
    await denominator.fill('0')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('超額費率分母格式不正確')).toBeVisible()
    expect(previewRequests).toBe(0)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C18'")).toBe(before)

    await denominator.fill('1')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()
    expect(previewRequests).toBe(1)
    await page.getByRole('button', { name: '確認發布價格' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認發布價格' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    expect(scalar('SELECT fixed_amount_minor FROM price_versions WHERE id=?', priceID)).toBe(fixedMinor)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C18'")).toBe(before + 1)
    await page.goto(`${app.baseURL}/admin/catalog/prices/${priceID}`)
    await expect(page.getByText('USD 90,071,992,547,409.93').first()).toBeVisible()
  })

  test('meter registration refuses an older conflicting schema preview', async ({ page, context }) => {
    await signIn(page)
    const meterID = `meter_conflict_${randomUUID().replaceAll('-', '')}`
    const fillMeter = async (activePage: Page, unit: string) => {
      await activePage.goto(`${app.baseURL}/admin/meters/new`)
      for (const [label, value] of [
        ['計量表 ID', meterID],
        ['事件來源', 'api'],
        ['計量單位', unit],
        ['Schema 版本', '1'],
      ]) await activePage.getByLabel(label, { exact: true }).fill(value)
      await activePage.getByRole('button', { name: '建立預覽' }).click()
      await expect(activePage.getByText('操作預覽', { exact: true })).toBeVisible()
    }
    await fillMeter(page, 'request')
    const other = await context.newPage()
    await fillMeter(other, 'token')
    await other.getByRole('button', { name: '確認註冊' }).last().click()
    await other.getByRole('dialog').getByRole('button', { name: '確認註冊' }).click()
    await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await other.close()

    const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認註冊' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認註冊' }).click()
    const rejected = await staleResponse
    expect(rejected.status()).toBe(409)
    expect((await rejected.json()).error.code).toBe('PREVIEW_STALE')
    await expect(page.getByText('原預覽已失效', { exact: true })).toBeVisible()
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    expect(scalar('SELECT unit FROM meter_schemas WHERE id=?', meterID)).toBe('token')
    expect(count('SELECT COUNT(*) FROM meter_schemas WHERE id=?', meterID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C19' AND json_extract(payload_json,'$.id')=? AND status='failed'", meterID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C19' AND json_extract(c.payload_json,'$.id')=?", meterID)).toBe(1)
  })

  test('metered price publication refuses an older conflicting version preview', async ({ page, context }) => {
    await signIn(page)
    const suffix = randomUUID().replaceAll('-', '')
    const meterID = `meter_price_conflict_${suffix}`
    const planID = `ai_price_conflict_${suffix}`
    const priceID = `price_conflict_${suffix}`
    const effective = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000).toISOString()
    await page.goto(`${app.baseURL}/admin/meters/new`)
    for (const [label, value] of [
      ['計量表 ID', meterID], ['事件來源', 'api'], ['計量單位', 'token'], ['Schema 版本', '1'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認註冊' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認註冊' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const fillPrice = async (activePage: Page, fixedMinor: string) => {
      await activePage.goto(`${app.baseURL}/admin/prices/metered/new`)
      for (const [label, value] of [
        ['價格版本 ID', priceID], ['方案 ID', planID], ['版本號', '1'],
        ['固定金額（最小單位）', fixedMinor], ['每席金額（最小單位）', '0'],
        ['計量表 ID', meterID], ['包含用量', '100'],
        ['超額費率分子', '2'], ['超額費率分母', '1'],
        ['生效起點（UTC）', effective],
      ]) await activePage.getByLabel(label, { exact: true }).fill(value)
      await activePage.getByRole('button', { name: '建立預覽' }).click()
      await expect(activePage.getByText('操作預覽', { exact: true })).toBeVisible()
    }
    await fillPrice(page, '3000')
    const other = await context.newPage()
    await fillPrice(other, '3500')
    await other.getByRole('button', { name: '確認發布價格' }).last().click()
    await other.getByRole('dialog').getByRole('button', { name: '確認發布價格' }).click()
    await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await other.close()

    const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認發布價格' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認發布價格' }).click()
    const rejected = await staleResponse
    expect(rejected.status()).toBe(409)
    expect((await rejected.json()).error.code).toBe('PREVIEW_STALE')
    await expect(page.getByText('原預覽已失效', { exact: true })).toBeVisible()
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    expect(count('SELECT fixed_amount_minor FROM price_versions WHERE id=?', priceID)).toBe(3500)
    expect(count('SELECT COUNT(*) FROM price_components WHERE price_version_id=?', priceID)).toBe(2)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C20' AND json_extract(payload_json,'$.id')=? AND status='failed'", priceID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C20' AND json_extract(c.payload_json,'$.id')=?", priceID)).toBe(1)
  })

  test('catalog selection refuses an older competing price preview', async ({ page, context }) => {
    await signIn(page)
    const suffix = randomUUID().replaceAll('-', '')
    const meterID = `selection_meter_${suffix}`
    const planID = `selection_plan_${suffix}`
    const cohort = `cohort_${suffix}`
    const priceID1 = `selection_price_a_${suffix}`
    const priceID2 = `selection_price_b_${suffix}`
    const effective = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000).toISOString()
    await page.goto(`${app.baseURL}/admin/meters/new`)
    for (const [label, value] of [
      ['計量表 ID', meterID], ['事件來源', 'api'], ['計量單位', 'token'], ['Schema 版本', '1'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認註冊' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認註冊' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const publishPrice = async (priceID: string, version: string) => {
      await page.goto(`${app.baseURL}/admin/prices/metered/new`)
      const another = page.getByRole('button', { name: '執行另一個操作' })
      if (await page.getByLabel('價格版本 ID', { exact: true }).isDisabled()) await another.click()
      for (const [label, value] of [
        ['價格版本 ID', priceID], ['方案 ID', planID], ['版本號', version],
        ['固定金額（最小單位）', '3000'], ['每席金額（最小單位）', '0'],
        ['計量表 ID', meterID], ['包含用量', '100'],
        ['超額費率分子', '2'], ['超額費率分母', '1'],
        ['生效起點（UTC）', effective],
      ]) await page.getByLabel(label, { exact: true }).fill(value)
      await page.getByRole('button', { name: '建立預覽' }).click()
      await page.getByRole('button', { name: '確認發布價格' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認發布價格' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    }
    await publishPrice(priceID1, '1')
    await publishPrice(priceID2, '2')

    const fillSelection = async (activePage: Page, priceID: string) => {
      await activePage.goto(`${app.baseURL}/admin/catalog-selections/new`)
      for (const [label, value] of [
        ['方案 ID', planID], ['Cohort', cohort],
        ['生效時間（UTC）', effective], ['價格版本 ID', priceID],
      ]) await activePage.getByLabel(label, { exact: true }).fill(value)
      await activePage.getByRole('button', { name: '建立預覽' }).click()
      await expect(activePage.getByText('操作預覽', { exact: true })).toBeVisible()
    }
    await fillSelection(page, priceID1)
    const other = await context.newPage()
    await fillSelection(other, priceID2)
    await other.getByRole('button', { name: '確認選價' }).last().click()
    await other.getByRole('dialog').getByRole('button', { name: '確認選價' }).click()
    await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await other.close()

    const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認選價' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認選價' }).click()
    const rejected = await staleResponse
    expect(rejected.status()).toBe(409)
    expect((await rejected.json()).error.code).toBe('PREVIEW_STALE')
    await expect(page.getByText('原預覽已失效', { exact: true })).toBeVisible()
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    expect(scalar('SELECT price_version_id FROM catalog_selection WHERE plan_id=?', planID)).toBe(priceID2)
    expect(count('SELECT COUNT(*) FROM catalog_selection WHERE plan_id=?', planID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C21' AND json_extract(payload_json,'$.plan_id')=? AND status='failed'", planID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C21' AND json_extract(c.payload_json,'$.plan_id')=?", planID)).toBe(1)
  })

  test('lost C18 publish response recovers one price and receipt with the original key', async ({ page }) => {
    await signIn(page)
    const priceID = `pro_lost_response_${randomUUID().replaceAll('-', '')}`
    const version = String(count("SELECT COALESCE(MAX(version), 0) + 1 FROM price_versions WHERE plan_id='pro'"))
    const effective = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000).toISOString()
    const paymentsBefore = count('SELECT COUNT(*) FROM payment_operations')
    await page.goto(`${app.baseURL}/admin/prices/pro/new`)
    for (const [label, value] of [
      ['價格版本 ID', priceID], ['版本號', version],
      ['固定金額（最小單位）', '6000'], ['每席金額（最小單位）', '1000'],
      ['包含任務量', '100'], ['超額費率分子', '1'],
      ['超額費率分母', '1'], ['生效起點（UTC）', effective],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()

    let dropped = false
    await page.route('**/admin/api/commands', async (route) => {
      if (!dropped && route.request().method() === 'POST') {
        dropped = true
        const response = await commitThenDropResponse(page, route)
        expect(response.status()).toBe(202)
        return
      }
      await route.continue()
    })
    await page.getByRole('button', { name: '確認發布價格' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認發布價格' }).click()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    expect(dropped).toBe(true)

    const commandCount = () => count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C18' AND json_extract(payload_json,'$.id')=?", priceID)
    expect(commandCount()).toBe(1)
    const commandID = scalar("SELECT id FROM admin_commands WHERE action_id='C18' AND json_extract(payload_json,'$.id')=?", priceID)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM price_versions WHERE id=?', priceID)).toBe(1)
    const checksum = scalar('SELECT checksum FROM price_versions WHERE id=?', priceID)
    expect(checksum).not.toBe('')
    expect(count('SELECT COUNT(*) FROM price_components WHERE price_version_id=?', priceID)).toBe(3)
    const receiptRefs = JSON.parse(scalar('SELECT result_refs_json FROM admin_command_receipts WHERE command_id=?', commandID)) as Record<string, string>
    expect(receiptRefs.price_version_id).toBe(priceID)
    expect(receiptRefs.checksum).toBe(checksum)
    expect(count('SELECT COUNT(*) FROM payment_operations')).toBe(paymentsBefore)

    await page.unroute('**/admin/api/commands')
    await page.reload()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    await page.getByRole('button', { name: '用原 request key 查詢' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(commandCount()).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM price_versions WHERE id=?', priceID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM payment_operations')).toBe(paymentsBefore)

    const previewID = scalar('SELECT preview_id FROM admin_commands WHERE id=?', commandID)
    const requestKey = scalar('SELECT idempotency_key FROM admin_commands WHERE id=?', commandID)
    const payload = JSON.parse(scalar('SELECT payload_json FROM admin_commands WHERE id=?', commandID)) as Record<string, string>
    execFileSync('python3', ['-c', `
import sqlite3, sys
db = sqlite3.connect(sys.argv[1])
db.execute("UPDATE admin_previews SET expires_at='2000-01-01T00:00:00Z' WHERE id=?", (sys.argv[2],))
db.commit()
`, app.commercePath, previewID])
    const sessionResponse = await page.request.get(`${app.baseURL}/admin/api/session`)
    const session = await sessionResponse.json() as { csrf_token: string }
    const submit = (key: string, commandPayload: Record<string, string>) => page.request.post(`${app.baseURL}/admin/api/commands`, {
      headers: { Origin: app.baseURL, 'X-CSRF-Token': session.csrf_token, 'Idempotency-Key': key },
      data: { action_id: 'C18', target_id: '', preview_id: previewID, payload: commandPayload },
    })
    const replay = await submit(requestKey, payload)
    expect(replay.status()).toBe(200)
    expect((await replay.json()).id).toBe(commandID)
    const changedPayload = await submit(requestKey, { ...payload, fixed_minor: '6001' })
    expect(changedPayload.status()).toBe(409)
    expect((await changedPayload.json()).error.code).toBe('IDEMPOTENCY_CONFLICT')
    const expiredPreview = await submit(randomUUID(), payload)
    expect(expiredPreview.status()).toBe(409)
    expect((await expiredPreview.json()).error.code).toBe('PREVIEW_STALE')
    expect(commandCount()).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM price_versions WHERE id=?', priceID)).toBe(1)
  })

  test('catalog publishes immutable price and meter versions before cohort selection', async ({ page }) => {
    await signIn(page)
    const suffix = randomUUID().replaceAll('-', '')
    const proPriceID = `pro_browser_${suffix}`
    const meterID = `meter_browser_${suffix}`
    const planID = `ai_browser_${suffix}`
    const meteredPriceID = `price_browser_${suffix}`
    const effective = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000).toISOString()
    const proVersion = String(count('SELECT COALESCE(MAX(version), 0) FROM price_versions WHERE plan_id=?', 'pro') + 1)
    async function submit(route: string, fields: Array<[string, string]>, confirm: string) {
      await page.goto(`${app.baseURL}${route}`)
      const startAnother = page.getByRole('button', { name: '執行另一個操作' })
      if (await page.getByLabel(fields[0]![0], { exact: true }).isDisabled()) await startAnother.click()
      for (const [label, value] of fields) await page.getByLabel(label, { exact: true }).fill(value)
      await page.getByRole('button', { name: '建立預覽' }).click()
      await expect(page.getByText('操作預覽', { exact: true })).toBeVisible({ timeout: 30_000 })
      await page.getByRole('button', { name: confirm }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: confirm }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    }

    await submit('/admin/prices/pro/new', [
      ['價格版本 ID', proPriceID], ['版本號', proVersion], ['固定金額（最小單位）', '6000'],
      ['每席金額（最小單位）', '1000'], ['包含任務量', '100'],
      ['超額費率分子', '1'], ['超額費率分母', '1'], ['生效起點（UTC）', effective],
    ], '確認發布價格')
    const publishedChecksum = scalar('SELECT checksum FROM price_versions WHERE id=?', proPriceID)
    expect(publishedChecksum).not.toBe('')
    expect(scalar('SELECT fixed_amount_minor FROM price_versions WHERE id=?', proPriceID)).toBe('6000')
    expect(count('SELECT COUNT(*) FROM price_components WHERE price_version_id=?', proPriceID)).toBe(3)
    expect(scalar("SELECT amount_minor FROM price_components WHERE price_version_id=? AND component_code='seats'", proPriceID)).toBe('1000')
    expect(scalar("SELECT quantity FROM price_components WHERE price_version_id=? AND component_code='tasks_included'", proPriceID)).toBe('100')
    expect(scalar("SELECT rate_num || '/' || rate_den FROM price_components WHERE price_version_id=? AND component_code='tasks_overage'", proPriceID)).toBe('1/1')
    const publishedCommands = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C18')
    let resetPublishedCommand = true
    for (const [conflictingID, conflictingFixedMinor] of [
      [`pro_collision_${suffix}`, '6000'],
      [proPriceID, '6001'],
    ]) {
      await page.goto(`${app.baseURL}/admin/prices/pro/new`)
      const startAnother = page.getByRole('button', { name: '執行另一個操作' })
      if (resetPublishedCommand) {
        await startAnother.click()
        resetPublishedCommand = false
      }
      for (const [label, value] of [
        ['價格版本 ID', conflictingID],
        ['版本號', proVersion],
        ['固定金額（最小單位）', conflictingFixedMinor],
        ['每席金額（最小單位）', '1000'],
        ['包含任務量', '100'],
        ['超額費率分子', '1'],
        ['超額費率分母', '1'],
        ['生效起點（UTC）', effective],
      ]) await page.getByLabel(label, { exact: true }).fill(value)
      const conflict = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
      await page.getByRole('button', { name: '建立預覽' }).click()
      expect((await conflict).status()).toBe(409)
      await expect(page.getByText('無法建立預覽')).toBeVisible()
      await expect(page.getByText('操作預覽', { exact: true })).toHaveCount(0)
      expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C18')).toBe(publishedCommands)
      expect(scalar('SELECT checksum FROM price_versions WHERE id=?', proPriceID)).toBe(publishedChecksum)
    }

    await submit('/admin/meters/new', [
      ['計量表 ID', meterID], ['事件來源', 'api'], ['計量單位', 'request'], ['Schema 版本', '1'],
    ], '確認註冊')
    expect(scalar('SELECT unit FROM meter_schemas WHERE id=?', meterID)).toBe('request')

    await submit('/admin/prices/metered/new', [
      ['價格版本 ID', meteredPriceID], ['方案 ID', planID], ['版本號', '1'],
      ['固定金額（最小單位）', '3000'], ['每席金額（最小單位）', '0'],
      ['計量表 ID', meterID], ['包含用量', '100'], ['超額費率分子', '2'],
      ['超額費率分母', '1'], ['生效起點（UTC）', effective],
    ], '確認發布價格')
    expect(scalar('SELECT checksum FROM price_versions WHERE id=?', meteredPriceID)).not.toBe('')
    expect(scalar('SELECT fixed_amount_minor FROM price_versions WHERE id=?', meteredPriceID)).toBe('3000')
    expect(count('SELECT COUNT(*) FROM price_components WHERE price_version_id=?', meteredPriceID)).toBe(2)
    expect(scalar("SELECT quantity FROM price_components WHERE price_version_id=? AND kind='included_quantity'", meteredPriceID)).toBe('100')
    expect(scalar("SELECT rate_num || '/' || rate_den FROM price_components WHERE price_version_id=? AND kind='usage_overage'", meteredPriceID)).toBe('2/1')

    await submit('/admin/catalog-selections/new', [
      ['方案 ID', planID], ['Cohort', 'default'], ['生效時間（UTC）', effective],
      ['價格版本 ID', meteredPriceID],
    ], '確認選價')
    expect(scalar('SELECT price_version_id FROM catalog_selection WHERE plan_id=?', planID)).toBe(meteredPriceID)
    const alternatePriceID = `price_alternate_${suffix}`
    await page.route('**/admin/api/commands/*', async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 1000))
      await route.continue()
    }, { times: 1 })
    await submit('/admin/prices/metered/new', [
      ['價格版本 ID', alternatePriceID], ['方案 ID', planID], ['版本號', '2'],
      ['固定金額（最小單位）', '3100'], ['每席金額（最小單位）', '0'],
      ['計量表 ID', meterID], ['包含用量', '100'],
      ['超額費率分子', '2'], ['超額費率分母', '1'], ['生效起點（UTC）', effective],
    ], '確認發布價格')
    const selectionCommands = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C21')
    await page.goto(`${app.baseURL}/admin/catalog-selections/new`)
    const startAnotherSelection = page.getByRole('button', { name: '執行另一個操作' })
    await expect(startAnotherSelection).toBeVisible()
    await startAnotherSelection.click()
    for (const [label, value] of [
      ['方案 ID', planID], ['Cohort', 'default'],
      ['生效時間（UTC）', effective], ['價格版本 ID', alternatePriceID],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    const selectionConflict = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '建立預覽' }).click()
    expect((await selectionConflict).status()).toBe(409)
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C21')).toBe(selectionCommands)
    expect(scalar('SELECT price_version_id FROM catalog_selection WHERE plan_id=?', planID)).toBe(meteredPriceID)
    for (const [actionID, id] of [['C18', proPriceID], ['C19', meterID], ['C20', meteredPriceID], ['C20', alternatePriceID]]) {
      expect(count(`SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id
        WHERE c.action_id=? AND json_extract(c.payload_json,'$.id')=?`, actionID, id)).toBe(1)
    }
    expect(count(`SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id
      WHERE c.action_id='C21' AND json_extract(c.payload_json,'$.plan_id')=?`, planID)).toBe(1)
  })

  test('price migration pins members and preserves a skipped item through pause and resume', async ({ page }) => {
    await signIn(page)
    const subscriptions: string[] = []
    for (let i = 0; i < 2; i++) {
      const customerID = `migration-browser-${i}-${randomUUID()}`
      await page.goto(`${app.baseURL}/admin/quotes/new`)
      const customerField = page.getByRole('textbox', { name: /客戶 ID/ })
      await expect(customerField).toBeVisible()
      if (await customerField.isDisabled()) await page.getByRole('button', { name: '建立另一筆報價' }).click()
      await page.getByRole('textbox', { name: /客戶 ID/ }).fill(customerID)
      await page.getByRole('textbox', { name: /方案 ID/ }).fill('pro')
      await page.getByRole('textbox', { name: /席次/ }).fill('5')
      await page.getByRole('button', { name: '建立報價' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      const quoteID = scalar('SELECT id FROM quotes WHERE customer_id=?', customerID)
      await page.goto(`${app.baseURL}/admin/quotes/${quoteID}/accept`)
      await page.getByRole('button', { name: '預覽接受' }).click()
      await page.getByRole('button', { name: '確認接受並建立付款義務' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認接受' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      const subscriptionID = scalar('SELECT id FROM subscriptions WHERE quote_id=?', quoteID)
      subscriptions.push(subscriptionID)
      const invoiceID = scalar('SELECT id FROM invoices WHERE subscription_id=?', subscriptionID)
      const operationID = scalar('SELECT id FROM payment_operations WHERE invoice_id=?', invoiceID)
      await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
      await page.getByRole('button', { name: '建立預覽' }).click()
      await page.getByRole('button', { name: '確認送出付款' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      await page.goto(`${app.baseURL}/admin/payments/${operationID}/reconcile`)
      await page.getByRole('button', { name: '確認查證' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認查證' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    }

    const suffix = randomUUID().replaceAll('-', '')
    const priceID = `pro_migration_${suffix}`
    const proVersion = String(count('SELECT COALESCE(MAX(version), 0) FROM price_versions WHERE plan_id=?', 'pro') + 1)
    const migrationID = `migration_${suffix}`
    const effective = new Date(Date.now() - 60_000).toISOString()
    await page.goto(`${app.baseURL}/admin/prices/pro/new`)
    for (const [label, value] of [
      ['價格版本 ID', priceID], ['版本號', proVersion], ['固定金額（最小單位）', '6000'],
      ['每席金額（最小單位）', '1000'], ['包含任務量', '20000'],
      ['超額費率分子', '1'], ['超額費率分母', '10'], ['生效起點（UTC）', effective],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認發布價格' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認發布價格' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await page.goto(`${app.baseURL}/admin/catalog-selections/new`)
    for (const [label, value] of [
      ['方案 ID', 'pro'], ['Cohort', 'A'], ['生效時間（UTC）', effective], ['價格版本 ID', priceID],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認選價' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認選價' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    await page.goto(`${app.baseURL}/admin/price-migrations/new`)
    for (const [label, value] of [
      ['遷移批次 ID', migrationID], ['Cohort', 'A'], ['目標價格版本 ID', priceID],
      ['訂閱 ID（逗號或換行分隔）', subscriptions.join(',')],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認建立遷移批次' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認建立遷移批次' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM price_migration_items WHERE migration_id=?', migrationID)).toBe(2)

    await page.goto(`${app.baseURL}/admin/price-migrations/${migrationID}/pause`)
    await page.getByRole('button', { name: '暫停未完成項目' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認暫停' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(scalar('SELECT status FROM price_migrations WHERE id=?', migrationID)).toBe('paused')

    const resumePage = await page.context().newPage()
    await resumePage.goto(`${app.baseURL}/admin/price-migrations/${migrationID}/resume`)
    await resumePage.getByRole('button', { name: '建立預覽' }).click()
    await expect(resumePage.getByText('操作預覽')).toBeVisible()

    await page.goto(`${app.baseURL}/admin/price-migrations/${migrationID}/skip`)
    await page.getByLabel('訂閱 ID', { exact: true }).fill(subscriptions[0])
    await page.getByLabel('略過理由', { exact: true }).fill('customer opted out')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認略過' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認略過' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const staleResume = resumePage.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await resumePage.getByRole('button', { name: '確認恢復' }).last().click()
    await resumePage.getByRole('dialog').getByRole('button', { name: '確認恢復' }).click()
    const rejected = await staleResume
    expect(rejected.status()).toBe(409)
    expect((await rejected.json()).error.code).toBe('PREVIEW_STALE')
    await expect(resumePage.getByText('原預覽已失效，請檢查新預覽並再次確認')).toBeVisible()
    expect(scalar('SELECT status FROM price_migrations WHERE id=?', migrationID)).toBe('paused')
    expect(count("SELECT COUNT(*) FROM price_migration_items WHERE migration_id=? AND status='skipped'", migrationID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C25' AND c.target_id=?", migrationID)).toBe(0)

    await resumePage.getByRole('button', { name: '確認恢復' }).last().click()
    await resumePage.getByRole('dialog').getByRole('button', { name: '確認恢復' }).click()
    await expect(resumePage.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await resumePage.close()
    expect(scalar('SELECT status FROM price_migrations WHERE id=?', migrationID)).toBe('active')
    expect(count("SELECT COUNT(*) FROM price_migration_items WHERE migration_id=? AND status='skipped'", migrationID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM price_migration_items WHERE migration_id=? AND status='pending'", migrationID)).toBe(1)
    expect(count(`SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id
      WHERE c.action_id='C22' AND json_extract(c.payload_json,'$.id')=?`, migrationID)).toBe(1)
    for (const actionID of ['C23', 'C24', 'C25']) {
      expect(count(`SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id
        WHERE c.action_id=? AND c.target_id=?`, actionID, migrationID)).toBe(1)
    }
  })

  test('usage keeps the original event, closes at zero and rerates late usage to one cent', async ({ page }) => {
    await signIn(page)
    const customerID = `usage-browser-${randomUUID()}`
    const { subscriptionID } = await createPaidSubscription(page, customerID, 'pro', '5')
    const periodEnd = BigInt(scalar('SELECT period_end FROM billing_periods WHERE subscription_id=?', subscriptionID))
    const at = (nanos: bigint) => new Date(Number(nanos / 1_000_000n)).toISOString()
    const originalID = `usage-${randomUUID()}`
    async function record(eventID: string, quantity: string, occurredAt: string) {
      await page.goto(`${app.baseURL}/admin/usage-events/new`)
      const subscriptionField = page.getByLabel('訂閱 ID', { exact: true })
      await expect(subscriptionField).toBeVisible()
      if (await subscriptionField.isDisabled()) await page.getByRole('button', { name: '記錄另一筆事件' }).click()
      for (const [label, value] of [
        ['訂閱 ID', subscriptionID], ['Meter ID', 'tasks'], ['來源', 'browser'],
        ['事件 ID', eventID], ['發生時間（UTC）', occurredAt], ['數量', quantity],
      ]) await page.getByLabel(label, { exact: true }).fill(value)
      await page.getByRole('button', { name: '檢查並記錄' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認記錄' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    }
    const originalAt = new Date().toISOString()
    await record(originalID, '20003', originalAt)
    await record(originalID, '20003', originalAt)
    expect(count('SELECT COUNT(*) FROM usage_events WHERE event_id=?', originalID)).toBe(1)
    await page.goto(`${app.baseURL}/admin/subscriptions/${subscriptionID}`)
    await page.getByRole('button', { name: '0 · 用量詳情' }).first().click()
    await expect(page).toHaveURL(new RegExp(`/admin/usage-periods/${subscriptionID}/0$`))
    await expect(page.getByRole('main').getByText('估算中', { exact: true })).toBeVisible()
    await expect(page.getByRole('main').getByText('20003', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('尚無已保存計價修訂')).toBeVisible()
    await page.route('**/admin/api/usage-periods/**', async (route) => {
      await route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":{"code":"QUERY_FAILED","message":"simulated read failure"}}' })
    })
    await page.getByRole('main').getByRole('button', { name: '重新整理' }).first().click()
    await expect(page.getByText('帳期更新失敗，顯示上次讀取結果')).toBeVisible({ timeout: 20_000 })
    await expect(page.getByText('修訂歷史更新失敗，顯示上次讀取結果')).toBeVisible({ timeout: 20_000 })
    await expect(page.getByRole('main').getByText('估算中', { exact: true })).toBeVisible()
    await expect(page.getByRole('main').getByText('20003', { exact: true }).first()).toBeVisible()
    await expect(page.getByRole('button', { name: '關閉用量帳期' })).toBeDisabled()
    await page.unroute('**/admin/api/usage-periods/**')
    await page.getByRole('main').getByRole('button', { name: '重新整理' }).first().click()
    await expect(page.getByText('帳期更新失敗，顯示上次讀取結果')).toHaveCount(0)
    await expect(page.getByText('修訂歷史更新失敗，顯示上次讀取結果')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '關閉用量帳期' })).toBeEnabled()
    await page.route('**/admin/api/usage-periods/**', async (route) => {
      await route.fulfill({ status: 403, contentType: 'application/json', body: '{"error":{"code":"FORBIDDEN","message":"simulated access denial"}}' })
    })
    await page.getByRole('main').getByRole('button', { name: '重新整理' }).first().click()
    await expect(page.getByText('沒有權限查看用量帳期')).toBeVisible()
    await expect(page.getByRole('main').getByText('20003', { exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: '關閉用量帳期' })).toHaveCount(0)
    await page.unroute('**/admin/api/usage-periods/**')

    let clockChanges = 0
    async function setClock(value: string, mode = 'fixed') {
      await page.goto(`${app.baseURL}/admin/lab/clock`)
      if (clockChanges > 0) await page.getByRole('button', { name: '執行另一個操作' }).click()
      await page.getByRole('combobox', { name: /模式/ }).click()
      await page.locator('.ant-select-dropdown:visible').getByText(mode === 'fixed' ? '固定時間' : '實際時間', { exact: true }).click()
      if (mode === 'fixed') await page.getByRole('textbox', { name: /固定 UTC 時間/ }).fill(value)
      await page.getByRole('button', { name: '確認設定時鐘' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      clockChanges++
    }

    const closeAt = at(periodEnd + 1_000_000_000n)
    await setClock(closeAt)
    await page.goto(`${app.baseURL}/admin/usage-periods/${subscriptionID}/close`)
    await page.getByLabel('帳期序號', { exact: true }).fill('0')
    await page.getByLabel('接收截止時間（UTC）', { exact: true }).fill(closeAt)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認關帳' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認關帳' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT rated_minor FROM usage_periods WHERE subscription_id=?', subscriptionID)).toBe(0)
    await page.goto(`${app.baseURL}/admin/data/usage-periods?subscription_id=${subscriptionID}`)
    await page.getByRole('button', { name: '詳情', exact: true }).first().click()
    await expect(page).toHaveURL(new RegExp(`/admin/usage-periods/${subscriptionID}/0$`))
    await expect(page.getByRole('main').getByText('已關帳', { exact: true })).toBeVisible()
    await expect(page.getByRole('main').getByText('計價修訂歷史')).toBeVisible()
    await expect(page.locator('.ant-table-tbody .ant-table-row')).toHaveCount(1)
    const ratingsRoute = (url: URL) => url.pathname === `/admin/api/usage-periods/${subscriptionID}/0/ratings`
    await page.route(ratingsRoute, async (route) => {
      await route.fulfill({ status: 403, contentType: 'application/json', body: '{"error":{"code":"FORBIDDEN","message":"simulated history denial"}}' })
    })
    await page.getByRole('main').getByRole('button', { name: '重新整理' }).last().click()
    await expect(page.getByText('沒有權限查看計價修訂歷史')).toBeVisible()
    await expect(page.locator('.ant-table-tbody .ant-table-row')).toHaveCount(0)
    await expect(page.getByRole('button', { name: '重算用量' })).toBeEnabled()
    await page.unroute(ratingsRoute)

    await setClock(at(periodEnd + 24n * 60n * 60n * 1_000_000_000n))
    const lateID = `late-${randomUUID()}`
    await record(lateID, '7', at(periodEnd - 60n * 60n * 1_000_000_000n))
    await page.goto(`${app.baseURL}/admin/usage-periods/${subscriptionID}/rerate`)
    await page.getByLabel('帳期序號', { exact: true }).fill('0')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認重算' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認重算' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT rated_minor FROM usage_periods WHERE subscription_id=?', subscriptionID)).toBe(1)
    await page.goto(`${app.baseURL}/admin/usage-periods/${subscriptionID}/0`)
    await expect(page.getByRole('main').getByText('已重算', { exact: true })).toBeVisible()
    await expect(page.locator('.ant-table-tbody .ant-table-row')).toHaveCount(2)
    await page.getByRole('button', { name: '查看用量事件' }).click()
    await expect(page).toHaveURL(new RegExp(`subscription_id=${subscriptionID}.*period_index=0`))
    await expect(page.getByRole('main').getByText(originalID)).toBeVisible()
    await expect(page.getByRole('main').getByText(lateID)).toBeVisible()

    await page.goto(`${app.baseURL}/admin/jobs/renewals`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認續約批次' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認續約批次' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const renewalInvoiceID = scalar('SELECT invoice_id FROM billing_periods WHERE subscription_id=? AND period_index=1', subscriptionID)
    expect(count("SELECT COUNT(*) FROM invoice_lines WHERE invoice_id=? AND component_code='usage:tasks:period:0' AND amount_minor=1", renewalInvoiceID)).toBe(1)
    const renewalOperationID = scalar('SELECT id FROM payment_operations WHERE invoice_id=?', renewalInvoiceID)
    await page.goto(`${app.baseURL}/admin/payments/${renewalOperationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/payments/${renewalOperationID}/reconcile`)
    await page.getByRole('button', { name: '確認查證' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認查證' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const adjustmentID = `adjust-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/usage-adjustments/new`)
    for (const [label, value] of [
      ['新事件來源', 'browser'], ['新事件 ID', adjustmentID], ['訂閱 ID', subscriptionID],
      ['原事件來源', 'browser'], ['原事件 ID', lateID], ['反向數量', '7'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認反向調整' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認反向調整' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT quantity FROM usage_events WHERE event_id=?', adjustmentID)).toBe(-7)
    await page.goto(`${app.baseURL}/admin/usage-periods/${subscriptionID}/rerate`)
    await page.getByRole('button', { name: '執行另一個操作' }).click()
    await page.getByLabel('帳期序號', { exact: true }).fill('0')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認重算' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認重算' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT rated_minor FROM usage_periods WHERE subscription_id=?', subscriptionID)).toBe(0)
    await page.goto(`${app.baseURL}/admin/jobs/usage-credit-notes`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    let creditNoteKey = ''
    let droppedCreditNoteResponse = false
    await page.route('**/admin/api/commands', async (route) => {
      if (!droppedCreditNoteResponse && route.request().method() === 'POST') {
        droppedCreditNoteResponse = true
        creditNoteKey = route.request().headers()['idempotency-key']
        await commitThenDropResponse(page, route)
      } else {
        await route.continue()
      }
    })
    await page.getByRole('button', { name: '確認產生' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認產生' }).click()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    expect(creditNoteKey).toBeTruthy()
    const creditNoteCommandID = scalar('SELECT id FROM admin_commands WHERE idempotency_key=?', creditNoteKey)
    const creditNoteJobID = `job:${creditNoteCommandID}`
    expect(count('SELECT COUNT(*) FROM admin_jobs WHERE id=?', creditNoteJobID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_job_items WHERE job_id=? AND target_id=? AND period_key=?', creditNoteJobID, subscriptionID, '0')).toBe(1)
    expect(count('SELECT COUNT(*) FROM usage_credit_notes WHERE source_invoice_id=?', renewalInvoiceID)).toBe(1)
    await page.reload()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    const creditNoteReplay = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST')
    await page.getByRole('button', { name: '用原 request key 查詢' }).click()
    expect((await creditNoteReplay).headers()['idempotency-key']).toBe(creditNoteKey)
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE id=?', creditNoteCommandID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', creditNoteCommandID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_jobs WHERE id=?', creditNoteJobID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_job_items WHERE job_id=? AND target_id=? AND period_key=?', creditNoteJobID, subscriptionID, '0')).toBe(1)
    expect(count('SELECT amount_minor FROM usage_credit_notes WHERE source_invoice_id=?', renewalInvoiceID)).toBe(1)
    expect(count('SELECT amount_minor FROM credit_grants WHERE source_invoice_id=?', renewalInvoiceID)).toBe(1)
    await page.goto(`${app.baseURL}/admin/usage-periods/${subscriptionID}/0`)
    await expect(page.getByRole('main').getByText('已重算', { exact: true })).toBeVisible()
    await expect(page.locator('.ant-table-tbody .ant-table-row')).toHaveCount(3)
    await expect(page.getByText('Credit Note 減額合計')).toBeVisible()
    await setClock('', 'real')
  })

  test('subscription history pages and entitlement provenance use the real database', async ({ page }) => {
    await signIn(page)
    const customerID = `history-browser-${randomUUID()}`
    const { subscriptionID, invoiceID, operationID } = await createPaidSubscription(page, customerID, 'pro', '2')
    const base = `${app.baseURL}/admin/api/subscriptions/${encodeURIComponent(subscriptionID)}`
    for (const endpoint of ['periods', 'timeline', 'entitlement']) {
      expect((await page.request.get(`${app.baseURL}/admin/api/subscriptions/missing/${endpoint}`)).status()).toBe(404)
      expect((await page.request.get(`${base}/${endpoint}?cursor=invalid`)).status()).toBe(endpoint === 'entitlement' ? 200 : 400)
    }
    const periods = await (await page.request.get(`${base}/periods?limit=1`)).json()
    expect(periods.items).toHaveLength(1)
    expect(periods.items[0].InvoiceID).toBe(invoiceID)
    const timeline = await (await page.request.get(`${base}/timeline?limit=1`)).json()
    expect(timeline.items).toHaveLength(1)
    expect(timeline.next_cursor).not.toBe('')
    const secondTimelinePage = await (await page.request.get(`${base}/timeline?limit=1&cursor=${encodeURIComponent(timeline.next_cursor)}`)).json()
    expect(secondTimelinePage.items).toHaveLength(1)
    expect(secondTimelinePage.items[0]).not.toEqual(timeline.items[0])
    const financialEvents = await (await page.request.get(`${base}/timeline?limit=100`)).json()
    expect(financialEvents.items.map((event: { Kind: string }) => event.Kind)).toContain('invoice_finalized')
    expect((await (await page.request.get(`${base}/entitlement`)).json()).entitlement).toBeNull()
    await page.goto(`${app.baseURL}/admin/subscriptions/${encodeURIComponent(subscriptionID)}`)
    await expect(page.getByText('帳期歷史')).toBeVisible()
    await expect(page.getByText('事件時間軸')).toBeVisible()
    await expect(page.getByText('權益尚未投影')).toBeVisible()
    expect((await page.request.get(`${app.baseURL}/admin/api/invoices/${encodeURIComponent(invoiceID)}`)).status()).toBe(200)
    await page.getByRole('link', { name: invoiceID }).click()
    await expect(page.getByRole('heading', { name: '帳單詳情' })).toBeVisible()
    await expect(page.getByText(operationID, { exact: true })).toBeVisible()
    await page.goto(`${app.baseURL}/admin/invoices`)
    const invoiceRow = page.getByRole('row').filter({ hasText: invoiceID })
    await expect(page.getByRole('table')).toBeVisible()
    for (let pageNumber = 1; pageNumber <= 20 && await invoiceRow.count() === 0; pageNumber++) {
      await page.getByRole('button', { name: '下一頁' }).click()
      await expect(page.getByText(`第 ${pageNumber + 1} 頁`)).toBeVisible()
      await expect(page.getByRole('table')).toBeVisible()
    }
    await invoiceRow.getByRole('button', { name: '詳情' }).click()
    await expect(page.getByRole('heading', { name: '帳單詳情' })).toBeVisible()
    await page.goto(`${app.baseURL}/admin/jobs/entitlement-refresh`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認刷新權益' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認刷新權益' }).click()
    await expect.poll(async () => (await (await page.request.get(`${base}/entitlement`)).json()).entitlement?.SourceOperationID).toBe(operationID)
    await page.goto(`${app.baseURL}/admin/subscriptions/${encodeURIComponent(subscriptionID)}`)
    await expect(page.getByText('來源操作')).toBeVisible()
    await expect(page.getByText(operationID, { exact: true })).toBeVisible()
  })

  test('repairs a missing entitlement projection once with a recorded command receipt', async ({ page }) => {
    await signIn(page)
    const { subscriptionID, operationID } = await createPaidSubscription(page, `repair-entitlement-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/jobs/entitlement-refresh`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認刷新權益' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認刷新權益' }).click()
    await expect.poll(async () => (await (await page.request.get(`${app.baseURL}/admin/api/subscriptions/${encodeURIComponent(subscriptionID)}/entitlement`)).json()).entitlement?.SourceOperationID).toBe(operationID)
    expect(count('SELECT COUNT(*) FROM entitlements WHERE subscription_id=?', subscriptionID)).toBe(1)

    const removeProjection = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("DELETE FROM entitlements WHERE subscription_id=?",(sys.argv[2],)); db.commit()'
    execFileSync('python3', ['-c', removeProjection, app.commercePath, subscriptionID])
    expect(count('SELECT COUNT(*) FROM entitlements WHERE subscription_id=?', subscriptionID)).toBe(0)
    await page.goto(`${app.baseURL}/admin/reconciliation-runs/new`)
    await page.getByLabel('核對截止時間（UTC）').fill(new Date().toISOString())
    await page.getByRole('button', { name: '確認執行對帳' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認執行對帳' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const discrepancyID = scalar('SELECT id FROM discrepancies WHERE object_id=? AND kind="entitlement_projection"', subscriptionID)
    expect(scalar('SELECT classification FROM discrepancies WHERE id=?', discrepancyID)).toBe('SAFE_AUTO_REPAIR')
    const detailResponse = await page.request.get(`${app.baseURL}/admin/api/discrepancies/${encodeURIComponent(discrepancyID)}`)
    expect(detailResponse.status()).toBe(200)
    const finding = (await detailResponse.json()).discrepancy.Discrepancy
    await page.goto(`${app.baseURL}/admin/discrepancies/${encodeURIComponent(discrepancyID)}`)
    await page.getByRole('button', { name: '規劃修復' }).click()
    await expect(page.getByLabel('來源修訂版')).toHaveValue(String(finding.SourceRevision))
    await expect(page.getByLabel('來源證據')).toHaveValue(finding.Evidence)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '確認修復' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認修復' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const commandID = scalar('SELECT id FROM admin_commands WHERE target_id=? AND action_id="C34" ORDER BY rowid DESC LIMIT 1', discrepancyID)
    expect(count('SELECT COUNT(*) FROM entitlements WHERE subscription_id=?', subscriptionID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe(1)
    expect(scalar('SELECT status FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe('verified')
    expect(scalar('SELECT expected FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe(finding.Expected)
    expect(scalar('SELECT actual FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe(finding.Actual)
    expect(scalar('SELECT verification FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toContain('absent in reconciliation')
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(scalar('SELECT status FROM discrepancies WHERE id=?', discrepancyID)).toBe('resolved')
    await page.reload()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe(1)
  })

  test('lost C34 repair response recovers the original command without repeating repair', async ({ page }) => {
    await signIn(page)
    const { subscriptionID, operationID } = await createPaidSubscription(page, `lost-repair-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/jobs/entitlement-refresh`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認刷新權益' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認刷新權益' }).click()
    await expect.poll(async () => (await (await page.request.get(`${app.baseURL}/admin/api/subscriptions/${encodeURIComponent(subscriptionID)}/entitlement`)).json()).entitlement?.SourceOperationID).toBe(operationID)

    const removeProjection = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("DELETE FROM entitlements WHERE subscription_id=?",(sys.argv[2],)); db.commit()'
    execFileSync('python3', ['-c', removeProjection, app.commercePath, subscriptionID])
    const capturesBefore = scalar('SELECT COUNT(*) FROM captures WHERE provider_key LIKE ?', '%', app.providerPath)

    await page.goto(`${app.baseURL}/admin/reconciliation-runs/new`)
    await page.getByLabel('核對截止時間（UTC）').fill(new Date().toISOString())
    await page.getByRole('button', { name: '確認執行對帳' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認執行對帳' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const discrepancyID = scalar('SELECT id FROM discrepancies WHERE object_id=? AND kind="entitlement_projection"', subscriptionID)
    expect(scalar('SELECT classification FROM discrepancies WHERE id=?', discrepancyID)).toBe('SAFE_AUTO_REPAIR')
    await page.goto(`${app.baseURL}/admin/discrepancies/${encodeURIComponent(discrepancyID)}`)
    await page.getByRole('button', { name: '規劃修復' }).click()
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()

    let dropped = false
    let droppedCommand: Promise<number> | undefined
    await page.route('**/admin/api/commands', async (route) => {
      if (!dropped && route.request().method() === 'POST') {
        dropped = true
        droppedCommand = commitThenDropResponse(page, route).then((response) => response.status())
        expect(await droppedCommand).toBe(202)
        return
      }
      await route.continue()
    })
    await page.getByRole('button', { name: '確認修復' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認修復' }).click()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    expect(dropped).toBe(true)
    expect(await droppedCommand).toBe(202)

    const commandCount = () => count('SELECT COUNT(*) FROM admin_commands WHERE action_id="C34" AND target_id=?', discrepancyID)
    expect(commandCount()).toBe(1)
    const commandID = scalar('SELECT id FROM admin_commands WHERE action_id="C34" AND target_id=?', discrepancyID)
    const requestKey = scalar('SELECT idempotency_key FROM admin_commands WHERE id=?', commandID)
    expect(requestKey).not.toBe('')
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe(1)
    expect(scalar('SELECT status FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe('verified')
    expect(count('SELECT COUNT(*) FROM entitlements WHERE subscription_id=?', subscriptionID)).toBe(1)
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key LIKE ?', '%', app.providerPath)).toBe(capturesBefore)

    await page.unroute('**/admin/api/commands')
    await page.reload()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    await page.getByRole('button', { name: '用原 request key 查詢' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(commandCount()).toBe(1)
    expect(scalar('SELECT idempotency_key FROM admin_commands WHERE id=?', commandID)).toBe(requestKey)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM entitlements WHERE subscription_id=?', subscriptionID)).toBe(1)
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key LIKE ?', '%', app.providerPath)).toBe(capturesBefore)
  })

  test('C34 waits for original provider evidence and verifies the same command when it arrives', async ({ page }) => {
    await signIn(page)
    const { operationID } = await createAcceptedSubscription(page, `repair-wait-${randomUUID()}`, 'basic')
    const markSubmitted = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("UPDATE payment_operations SET status=\'submitted\' WHERE id=? AND status=\'created\'",(sys.argv[2],)); db.commit()'
    execFileSync('python3', ['-c', markSubmitted, app.commercePath, operationID])
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('submitted')
    const providerKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', operationID)
    const amount = scalar('SELECT amount_minor FROM payment_operations WHERE id=?', operationID)
    const currency = scalar('SELECT currency FROM payment_operations WHERE id=?', operationID)
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath)).toBe('0')

    await page.goto(`${app.baseURL}/admin/reconciliation-runs/new`)
    await page.getByLabel('核對截止時間（UTC）').fill(new Date().toISOString())
    await page.getByRole('button', { name: '確認執行對帳' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認執行對帳' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const discrepancyID = scalar('SELECT id FROM discrepancies WHERE object_id=? AND kind="payment_unknown"', operationID)
    expect(scalar('SELECT classification FROM discrepancies WHERE id=?', discrepancyID)).toBe('EXTERNAL_LOOKUP_REQUIRED')

    await page.goto(`${app.baseURL}/admin/discrepancies/${encodeURIComponent(discrepancyID)}`)
    await page.getByRole('button', { name: '規劃修復' }).click()
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '確認修復' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認修復' }).click()
    await expect(page.getByRole('main').getByText('waiting_verification', { exact: true }).first()).toBeVisible()

    const commandID = scalar('SELECT id FROM admin_commands WHERE action_id="C34" AND target_id=?', discrepancyID)
    const commandCount = () => count('SELECT COUNT(*) FROM admin_commands WHERE action_id="C34" AND target_id=?', discrepancyID)
    expect(commandCount()).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(0)
    expect(scalar('SELECT status FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe('waiting')

    const retryWithoutEvidence = page.waitForResponse((response) => response.url().endsWith(`/admin/api/commands/${encodeURIComponent(commandID)}/resume`) && response.request().method() === 'POST')
    await page.getByRole('button', { name: '重新查證' }).click()
    expect((await retryWithoutEvidence).status()).toBe(200)
    await expect(page.getByRole('main').getByText('waiting_verification', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(0)
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath)).toBe('0')

    const recordCapture = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES(?,?,?,\'succeeded\')",(sys.argv[2],sys.argv[3],sys.argv[4])); db.commit()'
    execFileSync('python3', ['-c', recordCapture, app.providerPath, providerKey, amount, currency])
    await page.getByRole('button', { name: '重新查證' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(commandCount()).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe(1)
    expect(scalar('SELECT status FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe('verified')
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath)).toBe('1')
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('succeeded')
  })

  test('late provider amount mismatch blocks the waiting C34 repair in the browser', async ({ page }) => {
    await signIn(page)
    const { operationID } = await createAcceptedSubscription(page, `repair-late-mismatch-${randomUUID()}`, 'basic')
    const markSubmitted = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("UPDATE payment_operations SET status=\'submitted\' WHERE id=? AND status=\'created\'",(sys.argv[2],)); db.commit()'
    execFileSync('python3', ['-c', markSubmitted, app.commercePath, operationID])
    const providerKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', operationID)
    const expectedMinor = BigInt(scalar('SELECT amount_minor FROM payment_operations WHERE id=?', operationID))
    const providerMinor = (expectedMinor + 1n).toString()
    const currency = scalar('SELECT currency FROM payment_operations WHERE id=?', operationID)

    await page.goto(`${app.baseURL}/admin/reconciliation-runs/new`)
    await page.getByLabel('核對截止時間（UTC）').fill(new Date().toISOString())
    await page.getByRole('button', { name: '確認執行對帳' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認執行對帳' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const discrepancyID = scalar('SELECT id FROM discrepancies WHERE object_id=? AND kind="payment_unknown"', operationID)
    await page.goto(`${app.baseURL}/admin/discrepancies/${encodeURIComponent(discrepancyID)}`)
    await page.getByRole('button', { name: '規劃修復' }).click()
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '確認修復' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認修復' }).click()
    await expect(page.getByRole('main').getByText('waiting_verification', { exact: true }).first()).toBeVisible()
    const commandID = scalar('SELECT id FROM admin_commands WHERE action_id="C34" AND target_id=?', discrepancyID)
    const repairID = scalar('SELECT id FROM repair_operations WHERE discrepancy_id=?', discrepancyID)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(0)

    const recordCapture = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES(?,?,?,\'succeeded\')",(sys.argv[2],sys.argv[3],sys.argv[4])); db.commit()'
    execFileSync('python3', ['-c', recordCapture, app.providerPath, providerKey, providerMinor, currency])
    await page.getByRole('button', { name: '重新查證' }).click()
    await expect(page.getByText('修復未執行，需檢查最新對帳證據')).toBeVisible()
    await expect(page.getByRole('alert').getByText('provider amount mismatch requires manual investigation')).toBeVisible()
    expect(scalar('SELECT status FROM repair_operations WHERE id=?', repairID)).toBe('blocked')
    expect(count('SELECT COUNT(*) FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id="C34" AND target_id=?', discrepancyID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('submitted')
    expect(count('SELECT COUNT(*) FROM allocations WHERE operation_id=?', operationID)).toBe(0)
    expect(scalar('SELECT amount_minor FROM captures WHERE provider_key=?', providerKey, app.providerPath)).toBe(providerMinor)
  })

  test('changed source revision blocks repair and shows the blocked result', async ({ page }) => {
    await signIn(page)
    const { subscriptionID, operationID } = await createPaidSubscription(page, `blocked-repair-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/jobs/entitlement-refresh`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認刷新權益' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認刷新權益' }).click()
    await expect.poll(async () => (await (await page.request.get(`${app.baseURL}/admin/api/subscriptions/${encodeURIComponent(subscriptionID)}/entitlement`)).json()).entitlement?.SourceOperationID).toBe(operationID)
    const changeProjection = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("DELETE FROM entitlements WHERE subscription_id=?",(sys.argv[2],)); db.commit()'
    execFileSync('python3', ['-c', changeProjection, app.commercePath, subscriptionID])
    await page.goto(`${app.baseURL}/admin/reconciliation-runs/new`)
    await page.getByLabel('核對截止時間（UTC）').fill(new Date().toISOString())
    await page.getByRole('button', { name: '確認執行對帳' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認執行對帳' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const discrepancyID = scalar('SELECT id FROM discrepancies WHERE object_id=? AND kind="entitlement_projection"', subscriptionID)
    await page.goto(`${app.baseURL}/admin/discrepancies/${encodeURIComponent(discrepancyID)}`)
    await page.getByRole('button', { name: '規劃修復' }).click()
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()
    const bumpRevision = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("UPDATE subscriptions SET revision=revision+1 WHERE id=?",(sys.argv[2],)); db.commit()'
    execFileSync('python3', ['-c', bumpRevision, app.commercePath, subscriptionID])
    await page.getByRole('button', { name: '確認修復' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認修復' }).click()
    await expect(page.getByText('修復未執行，需檢查最新對帳證據')).toBeVisible()
    expect(count('SELECT COUNT(*) FROM entitlements WHERE subscription_id=?', subscriptionID)).toBe(0)
    expect(count('SELECT COUNT(*) FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe(1)
    expect(scalar('SELECT status FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe('blocked')
    const commandID = scalar('SELECT id FROM admin_commands WHERE target_id=? AND action_id="C34" ORDER BY rowid DESC LIMIT 1', discrepancyID)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
  })

  test('provider amount mismatch stays manual without changing the captured amount', async ({ page }) => {
    await signIn(page)
    const { invoiceID, operationID } = await createAcceptedSubscription(page, `amount-mismatch-${randomUUID()}`, 'basic')
    const expectedMinor = BigInt(scalar('SELECT total_minor FROM invoices WHERE id=?', invoiceID))
    const providerMinor = (expectedMinor + 100n).toString()
    const providerKey = `capture:${invoiceID}`
    const addMismatchedCapture = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES(?,?,\'USD\',\'succeeded\')",(sys.argv[2],sys.argv[3])); db.commit()'
    execFileSync('python3', ['-c', addMismatchedCapture, app.providerPath, providerKey, providerMinor])
    const providerCapturesBefore = scalar('SELECT COUNT(*) FROM captures WHERE provider_key LIKE ?', '%', app.providerPath)
    await page.goto(`${app.baseURL}/admin/reconciliation-runs/new`)
    await page.getByLabel('核對截止時間（UTC）').fill(new Date().toISOString())
    await page.getByRole('button', { name: '確認執行對帳' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認執行對帳' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const discrepancyID = scalar('SELECT id FROM discrepancies WHERE object_id=? AND kind="provider_amount_mismatch"', operationID)
    expect(scalar('SELECT classification FROM discrepancies WHERE id=?', discrepancyID)).toBe('MANUAL_REVIEW')

    await page.goto(`${app.baseURL}/admin/discrepancies/${encodeURIComponent(discrepancyID)}`)
    await page.getByRole('button', { name: '規劃修復' }).click()
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()
    await expect(page.getByText('manual_review', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '確認修復' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認修復' }).click()
    await expect(page.getByText('修復未執行，需檢查最新對帳證據')).toBeVisible()
    expect(scalar('SELECT status FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe('blocked')
    expect(scalar('SELECT verification FROM repair_operations WHERE discrepancy_id=?', discrepancyID)).toBe('manual decision and evidence required')
    expect(scalar('SELECT amount_minor FROM captures WHERE provider_key=?', providerKey, app.providerPath)).toBe(providerMinor)
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', providerKey, app.providerPath)).toBe('1')
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key LIKE ?', '%', app.providerPath)).toBe(providerCapturesBefore)

    await page.getByRole('button', { name: '查看差異' }).click()
    await expect(page).toHaveURL(`${app.baseURL}/admin/discrepancies/${encodeURIComponent(discrepancyID)}`)
    await page.getByRole('button', { name: '記錄人工決議' }).click()
    await page.getByLabel('決議').fill('investigate_provider_amount')
    await page.getByLabel('原因').fill('服務商收款金額與本地帳單不符，交由人工查證')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認記錄決議' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認記錄決議' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM manual_decisions WHERE discrepancy_id=?', discrepancyID)).toBe(1)
    expect(scalar('SELECT amount_minor FROM captures WHERE provider_key=?', providerKey, app.providerPath)).toBe(providerMinor)
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key LIKE ?', '%', app.providerPath)).toBe(providerCapturesBefore)
  })

  test('reconciliation discrepancy detail preserves evidence and manual decision provenance', async ({ page }) => {
    await signIn(page)
    const providerKey = `capture:orphan-${randomUUID()}`
    const insertCapture = 'import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); db.execute("INSERT INTO captures(provider_key,amount_minor,currency,status) VALUES(?,100,\'USD\',\'succeeded\')",(sys.argv[2],)); db.commit()'
    execFileSync('python3', ['-c', insertCapture, app.providerPath, providerKey])
    await page.goto(`${app.baseURL}/admin/reconciliation-runs/new`)
    await page.getByLabel('核對截止時間（UTC）').fill(new Date().toISOString())
    await page.getByRole('button', { name: '確認執行對帳' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認執行對帳' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const discrepancyID = scalar('SELECT id FROM discrepancies WHERE object_id=?', providerKey)
    const response = await page.request.get(`${app.baseURL}/admin/api/discrepancies/${encodeURIComponent(discrepancyID)}`)
    expect(response.status()).toBe(200)
    const detail = (await response.json()).discrepancy
    expect(detail.Discrepancy.Expected).toBe('known payment operation')
    expect(detail.Discrepancy.Actual).toContain('100 USD succeeded')
    expect(detail.Runs).toHaveLength(1)
    const runID = detail.Runs[0].ID
    await page.goto(`${app.baseURL}/admin/reconciliation`)
    await page.getByRole('textbox', { name: 'ID 前綴' }).fill(runID)
    await page.getByRole('button', { name: '套用篩選' }).click()
    await page.getByRole('row').filter({ hasText: runID }).getByRole('button', { name: '詳情' }).click()
    await expect(page.getByRole('heading', { name: '對帳執行詳情' })).toBeVisible()
    const runPage = (await (await page.request.get(`${app.baseURL}/admin/api/reconciliation-runs/${encodeURIComponent(runID)}?limit=100`)).json()).page
    expect(runPage.Findings.find((item: { DiscrepancyID: string }) => item.DiscrepancyID === discrepancyID)?.SnapshotQuality).toBe('recorded')
    await page.route('**/admin/api/discrepancies?**', async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 300))
      await route.continue()
    })
    await page.goto(`${app.baseURL}/admin/discrepancies`)
    const discrepancyRow = page.getByRole('row').filter({ hasText: providerKey })
    await expect(page.getByRole('table')).toBeVisible()
    for (let pageNumber = 1; pageNumber <= 20 && await discrepancyRow.count() === 0; pageNumber++) {
      await page.getByRole('button', { name: '下一頁' }).click()
      await expect(page.getByText(`第 ${pageNumber + 1} 頁`)).toBeVisible()
      await expect(page.getByRole('table')).toBeVisible()
    }
    await discrepancyRow.getByRole('button', { name: '詳情' }).click()
    await expect(page.getByRole('heading', { name: '對帳差異詳情' })).toBeVisible()
    await expect(page.getByText('provider capture list')).toBeVisible()
    await expect(page.getByText('known payment operation')).toBeVisible()
    await page.getByRole('button', { name: '記錄人工決議' }).click()
    await page.getByLabel('決議').fill('investigate_source')
    await page.getByLabel('原因').fill('確認服務商孤兒收款來源')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認記錄決議' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認記錄決議' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM manual_decisions WHERE discrepancy_id=?', discrepancyID)).toBe(1)
    await page.goto(`${app.baseURL}/admin/discrepancies/${encodeURIComponent(discrepancyID)}`)
    await expect(page.getByText('investigate_source')).toBeVisible()
    await expect(page.getByText('確認服務商孤兒收款來源')).toBeVisible()
    await expect(page.getByText('人工決議是審核記錄，不代表資金已調整。')).toBeVisible()
    const originalRun = (await (await page.request.get(`${app.baseURL}/admin/api/reconciliation-runs/${encodeURIComponent(runID)}?limit=100`)).json()).page
    const originalFinding = originalRun.Findings.find((item: { DiscrepancyID: string }) => item.DiscrepancyID === discrepancyID)
    expect(originalFinding.Expected).toBe('known payment operation')
    expect(originalFinding.Actual).toContain('100 USD succeeded')
    expect(originalFinding.CurrentStatus).toBe('investigating')
  })

  test('account migration detail shows readiness thresholds and keeps owners after stop', async ({ page }) => {
    await signIn(page)
    const legacyID = `legacy:browser-${randomUUID()}`
    const customerID = `migration-browser-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/account-migrations/new`)
    for (const [label, value] of [
      ['既有帳戶 ID', legacyID], ['Commerce 客戶 ID', customerID], ['受益人 ID', customerID], ['價格 Cohort', 'default'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('combobox', { name: '是否有歷史資料' }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('沒有', { exact: true }).click()
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認連結帳戶' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認連結帳戶' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/account-migrations`)
    await page.getByRole('row').filter({ hasText: legacyID }).getByRole('button', { name: '詳情' }).click()
    await expect(page.getByRole('heading', { name: '帳戶遷移詳情' })).toBeVisible()
    await expect(page.getByText(customerID, { exact: true }).first()).toBeVisible()
    await page.getByRole('button', { name: '比對報價' }).click()
    for (const [label, value] of [
      ['方案 ID', 'basic'], ['席位數', '0'], ['既有報價金額', '2000'], ['幣別', 'USD'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('button', { name: '確認記錄比對' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認記錄比對' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const base = `${app.baseURL}/admin/api/account-migrations/${encodeURIComponent(legacyID)}`
    const detail = (await (await page.request.get(base)).json()).migration
    expect(detail.Shadows).toHaveLength(1)
    expect(detail.Shadows[0].Matched).toBe(true)
    const legacySubscriptionID = `legacy-sub-${randomUUID()}`
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}`)
    await page.getByRole('button', { name: '比對權益' }).click()
    await expect(page).toHaveURL(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/shadow-entitlements`)
    await expect(page.getByRole('heading', { name: '比對權益' })).toBeVisible()
    await page.getByLabel('訂閱 ID', { exact: true }).fill(legacySubscriptionID)
    await page.getByLabel('既有權益狀態').fill('active')
    await expect(page.getByLabel('訂閱 ID', { exact: true })).toHaveValue(legacySubscriptionID)
    await expect(page.getByLabel('既有權益狀態')).toHaveValue('active')
    await page.getByRole('button', { name: '確認記錄比對' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認記錄比對' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}`)
    await page.getByLabel('訂閱 ID', { exact: true }).fill(legacySubscriptionID)
    await page.getByRole('button', { name: '查詢權益' }).click()
    await expect(page.getByText('Adapter 權益讀取')).toBeVisible()
    const entitlement = (await (await page.request.get(`${base}/entitlements/${encodeURIComponent(legacySubscriptionID)}`)).json()).entitlement
    expect(entitlement.Owner).toBe('legacy')
    expect(entitlement.Status).toBe('active')
    await page.getByLabel('報價 P95 上限（毫秒）').fill('5000')
    await page.getByLabel('未知付款上限').fill('0')
    await page.getByLabel('未結對帳差異上限').fill('0')
    await page.getByRole('button', { name: '計算 Readiness' }).click()
    await expect(page.getByText('報價 Shadow 一致')).toBeVisible()
    const readiness = (await (await page.request.get(`${base}/readiness?max_quote_p95_millis=5000&max_unknown_payments=0&max_open_discrepancies=0`)).json()).readiness
    expect(readiness.QuoteMatches).toBe(true)
    expect(readiness.Ready).toBe(false)
    await page.goto(`${app.baseURL}/admin/quotes/new`)
    await page.getByRole('textbox', { name: '客戶 ID' }).fill(customerID)
    await page.getByRole('textbox', { name: '方案 ID' }).fill('basic')
    await page.getByRole('button', { name: '建立報價' }).click()
    await page.getByRole('button', { name: '前往接受報價' }).click()
    const acceptURL = page.url()
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}`)
    await page.getByRole('button', { name: '停止遷移' }).click()
    await page.getByLabel('停止原因').fill('browser review')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認停止' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認停止' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}`)
    await expect(page.getByText('遷移已停止')).toBeVisible()
    const stopped = (await (await page.request.get(base)).json()).migration
    expect(stopped.Link.Stopped).toBe(true)
    expect(stopped.Link.ReadOwner).toBe('legacy')
    expect(stopped.Link.WriterOwner).toBe('legacy')
    expect(stopped.Events.some((event: { Kind: string }) => event.Kind === 'stopped')).toBe(true)
    await page.goto(acceptURL)
    const blockedPreview = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '預覽接受' }).click()
    const blockedResponse = await blockedPreview
    expect(blockedResponse.status()).toBe(409)
    expect((await blockedResponse.json()).error.code).toBe('ACCOUNT_MIGRATION_STOPPED')
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    await expect(page.getByRole('button', { name: '確認接受並建立付款義務' })).toHaveCount(0)
    expect(count('SELECT COUNT(*) FROM subscriptions WHERE customer_id=?', customerID)).toBe(0)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C02' AND target_id IN (SELECT id FROM quotes WHERE customer_id=?)", customerID)).toBe(0)
    expect(count(`SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id
      WHERE c.action_id='C36' AND json_extract(c.payload_json,'$.legacy_account_id')=?`, legacyID)).toBe(1)
    for (const actionID of ['C37', 'C38', 'C43']) {
      expect(count(`SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id
        WHERE c.action_id=? AND c.target_id=?`, actionID, legacyID)).toBe(1)
    }
  })

  test('historical account provenance review gates read and writer cutover', async ({ page }) => {
    await signIn(page)
    const customerID = `history-cutover-${randomUUID()}`
    const legacyID = `legacy:history-${randomUUID()}`
    const legacyInvoiceID = `old-invoice-${randomUUID()}`
    const { subscriptionID, invoiceID, operationID } = await createPaidSubscription(page, customerID, 'basic')
    await page.goto(`${app.baseURL}/admin/jobs/entitlement-refresh`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認刷新權益' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認刷新權益' }).click()
    await expect.poll(async () => (await (await page.request.get(`${app.baseURL}/admin/api/subscriptions/${encodeURIComponent(subscriptionID)}/entitlement`)).json()).entitlement?.SourceOperationID).toBe(operationID)
    async function confirmPreview(label: string) {
      await page.getByRole('button', { name: '建立預覽' }).click()
      await page.getByRole('button', { name: label }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: label }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    }
    await page.goto(`${app.baseURL}/admin/account-migrations/new`)
    for (const [label, value] of [
      ['既有帳戶 ID', legacyID], ['Commerce 客戶 ID', customerID], ['受益人 ID', customerID], ['價格 Cohort', 'default'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('combobox', { name: '是否有歷史資料' }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('有', { exact: true }).click()
    await confirmPreview('確認連結帳戶')
    const base = `${app.baseURL}/admin/api/account-migrations/${encodeURIComponent(legacyID)}`
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/shadow-quotes`)
    for (const [label, value] of [['方案 ID', 'basic'], ['席位數', '0'], ['既有報價金額', '2000'], ['幣別', 'USD']]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('button', { name: '確認記錄比對' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認記錄比對' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/shadow-entitlements`)
    await page.getByLabel('訂閱 ID', { exact: true }).fill(subscriptionID)
    await page.getByLabel('既有權益狀態').fill('active')
    await page.getByRole('button', { name: '確認記錄比對' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認記錄比對' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/provenance`)
    for (const [label, value] of [
      ['舊帳單 ID', legacyInvoiceID], ['舊訂閱 ID', `old-sub-${randomUUID()}`],
      ['Commerce 訂閱 ID', subscriptionID], ['Commerce 帳單 ID', invoiceID], ['價格版本 ID', 'pro-v1'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await confirmPreview('確認回填')
    let migration = (await (await page.request.get(base)).json()).migration
    expect(migration.Provenance).toHaveLength(1)
    expect(migration.Provenance[0].Status).toBe('manual_review')
    const readinessURL = `${base}/readiness?max_quote_p95_millis=5000&max_unknown_payments=0&max_open_discrepancies=0`
    let readiness = (await (await page.request.get(readinessURL)).json()).readiness
    expect(readiness.ProvenanceComplete).toBe(false)
    expect(readiness.Ready).toBe(false)
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/switch-read`)
    await page.getByLabel('報價 P95 上限（毫秒）').fill('5000')
    await page.getByLabel('未知付款上限').fill('0')
    await page.getByLabel('未結對帳差異上限').fill('0')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/provenance/${encodeURIComponent(legacyInvoiceID)}/resolve`)
    for (const [label, value] of [
      ['Commerce 訂閱 ID', subscriptionID], ['Commerce 帳單 ID', invoiceID], ['價格版本 ID', 'basic-v1'], ['審核決議', 'confirmed_source'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await confirmPreview('確認來源映射')
    migration = (await (await page.request.get(base)).json()).migration
    expect(migration.Provenance[0].Status).toBe('complete')
    expect(migration.Events.some((event: { Kind: string }) => event.Kind === 'provenance_resolved')).toBe(true)
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}`)
    await page.getByRole('button', { name: '查看完整歷史' }).click()
    await expect(page.getByRole('heading', { name: 'Shadow 比對歷史' })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: subscriptionID })).toBeVisible()
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}`)
    await page.getByRole('button', { name: '查看完整記錄' }).click()
    await expect(page.getByRole('heading', { name: '既有帳務來源歷史' })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: legacyInvoiceID })).toBeVisible()
    await page.goto(`${app.baseURL}/admin/reconciliation-runs/new`)
    await page.getByLabel('核對截止時間（UTC）').fill(new Date().toISOString())
    await page.getByRole('button', { name: '確認執行對帳' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認執行對帳' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    readiness = (await (await page.request.get(readinessURL)).json()).readiness
    expect(readiness.ProvenanceComplete).toBe(true)
    expect(readiness.QuoteMatches).toBe(true)
    expect(readiness.EntitlementMatches).toBe(true)
    expect(readiness.Ready).toBe(true)
    const fillLimits = async () => {
      await page.getByLabel('報價 P95 上限（毫秒）').fill('5000')
      await page.getByLabel('未知付款上限').fill('0')
      await page.getByLabel('未結對帳差異上限').fill('0')
    }
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/switch-read`)
    await fillLimits()
    await confirmPreview('確認切換讀取')
    migration = (await (await page.request.get(base)).json()).migration
    expect(migration.Link.ReadOwner).toBe('commerce')
    expect(migration.Link.WriterOwner).toBe('legacy')
    const adapter = (await (await page.request.get(`${base}/entitlements/${encodeURIComponent(subscriptionID)}`)).json()).entitlement
    expect(adapter.Owner).toBe('commerce')
    expect(adapter.Status).toBe('active')
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/switch-writer`)
    await fillLimits()
    await page.getByRole('button', { name: '建立預覽' }).click()
    let droppedWriterResponse = false
    let writerRequestKey = ''
    let writerCommit: Promise<void> | null = null
    await page.route('**/admin/api/commands', async (route) => {
      if (!droppedWriterResponse && route.request().method() === 'POST') {
        droppedWriterResponse = true
        writerRequestKey = route.request().headers()['idempotency-key']
        writerCommit = (async () => {
          const committed = await commitThenDropResponse(page, route)
          expect(committed.ok()).toBe(true)
        })()
        await writerCommit
      } else {
        await route.continue()
      }
    })
    await page.getByRole('button', { name: '確認切換寫入' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認切換寫入' }).click()
    await expect.poll(() => writerCommit !== null).toBe(true)
    await writerCommit
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    expect(writerRequestKey).toBeTruthy()
    const writerCommandID = scalar("SELECT id FROM admin_commands WHERE action_id='C42' AND target_id=?", legacyID)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C42' AND target_id=?", legacyID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', writerCommandID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM account_migration_events WHERE legacy_account_id=? AND kind='writer_cutover'", legacyID)).toBe(1)
    await page.unroute('**/admin/api/commands')
    await page.reload()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    const writerReplay = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST')
    await page.getByRole('button', { name: '用原 request key 查詢' }).click()
    expect((await writerReplay).headers()['idempotency-key']).toBe(writerRequestKey)
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    migration = (await (await page.request.get(base)).json()).migration
    expect(migration.Link.ReadOwner).toBe('commerce')
    expect(migration.Link.WriterOwner).toBe('commerce')
    expect(migration.Events.some((event: { Kind: string }) => event.Kind === 'writer_cutover')).toBe(true)
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id IN ('C41','C42') AND c.target_id=?", legacyID)).toBe(2)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C42' AND target_id=?", legacyID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM account_migration_events WHERE legacy_account_id=? AND kind='writer_cutover'", legacyID)).toBe(1)
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/stop`)
    await page.getByLabel('停止原因').fill('post-cutover review')
    await confirmPreview('確認停止')
    migration = (await (await page.request.get(base)).json()).migration
    expect(migration.Link.Stopped).toBe(true)
    expect(migration.Link.ReadOwner).toBe('commerce')
    expect(migration.Link.WriterOwner).toBe('commerce')
    expect(count("SELECT COUNT(*) FROM account_migration_events WHERE legacy_account_id=? AND kind='stopped'", legacyID)).toBe(1)
    expect(count("SELECT COUNT(*) FROM admin_command_receipts r JOIN admin_commands c ON c.id=r.command_id WHERE c.action_id='C43' AND c.target_id=?", legacyID)).toBe(1)

    await page.goto(`${app.baseURL}/admin/subscriptions/${encodeURIComponent(subscriptionID)}/cancel`)
    const blockedCancel = page.waitForResponse((response) => response.url().endsWith('/admin/api/previews') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '預覽取消' }).click()
    expect((await blockedCancel).status()).toBe(409)
    await expect(page.getByText('帳戶遷移已停止，無法新增取消排程')).toBeVisible()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C05' AND target_id=?", subscriptionID)).toBe(0)
    expect(count("SELECT COUNT(*) FROM subscription_schedules WHERE subscription_id=? AND kind='cancel'", subscriptionID)).toBe(0)
  })

  test('unknown payment blocks account cutover until the original command is verified', async ({ page }) => {
    await signIn(page)
    const customerID = `unknown-cutover-${randomUUID()}`
    const legacyID = `legacy:unknown-${randomUUID()}`
    const { subscriptionID, invoiceID, operationID } = await createAcceptedSubscription(page, customerID, 'basic')
    const accountURL = `${app.baseURL}/admin/api/account-migrations/${encodeURIComponent(legacyID)}`
    const readinessURL = `${accountURL}/readiness?max_quote_p95_millis=5000&max_unknown_payments=0&max_open_discrepancies=100`

    async function confirmPreview(label: string) {
      await page.getByRole('button', { name: '建立預覽' }).click()
      await page.getByRole('button', { name: label }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: label }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    }
    async function recordEntitlement(status: string) {
      await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/shadow-entitlements`)
      await expect(page.locator('.form-page')).toBeVisible()
      const another = page.getByRole('button', { name: '執行另一個操作' })
      if (await another.count()) await another.click()
      await page.getByLabel('訂閱 ID', { exact: true }).fill(subscriptionID)
      await page.getByLabel('既有權益狀態').fill(status)
      await page.getByRole('button', { name: '確認記錄比對' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認記錄比對' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    }
    async function reconcile() {
      await page.goto(`${app.baseURL}/admin/reconciliation-runs/new`)
      await expect(page.locator('.form-page')).toBeVisible()
      const another = page.getByRole('button', { name: '執行另一個操作' })
      if (await another.count()) await another.click()
      await page.getByLabel('核對截止時間（UTC）').fill(new Date().toISOString())
      await page.getByRole('button', { name: '確認執行對帳' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認執行對帳' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    }
    async function fillLimits() {
      await page.getByLabel('報價 P95 上限（毫秒）').fill('5000')
      await page.getByLabel('未知付款上限').fill('0')
      await page.getByLabel('未結對帳差異上限').fill('100')
    }

    await page.goto(`${app.baseURL}/admin/lab/faults/${operationID}`)
    await page.getByRole('combobox', { name: /操作種類/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('付款', { exact: true }).click()
    await page.getByRole('combobox', { name: /故障模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('回應遺失', { exact: true }).click()
    await page.getByRole('button', { name: '確認故障票據' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認故障票據' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/payments/${operationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByText('waiting_verification')).toBeVisible()
    const commandID = scalar('SELECT id FROM admin_commands WHERE action_id=? ORDER BY created_at DESC LIMIT 1', 'C09')
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('unknown')

    await page.goto(`${app.baseURL}/admin/account-migrations/new`)
    for (const [label, value] of [
      ['既有帳戶 ID', legacyID], ['Commerce 客戶 ID', customerID], ['受益人 ID', customerID], ['價格 Cohort', 'default'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await page.getByRole('combobox', { name: '是否有歷史資料' }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('有', { exact: true }).click()
    await confirmPreview('確認連結帳戶')
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/shadow-quotes`)
    for (const [label, value] of [['方案 ID', 'basic'], ['席位數', '0'], ['既有報價金額', '2000'], ['幣別', 'USD']]) {
      await page.getByLabel(label, { exact: true }).fill(value)
    }
    await page.getByRole('button', { name: '確認記錄比對' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認記錄比對' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await recordEntitlement('pending')
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/provenance`)
    for (const [label, value] of [
      ['舊帳單 ID', `old-invoice-${randomUUID()}`],
      ['舊訂閱 ID', `old-sub-${randomUUID()}`],
      ['Commerce 訂閱 ID', subscriptionID],
      ['Commerce 帳單 ID', invoiceID],
      ['價格版本 ID', 'basic-v1'],
    ]) await page.getByLabel(label, { exact: true }).fill(value)
    await confirmPreview('確認回填')
    await reconcile()
    let readiness = (await (await page.request.get(readinessURL)).json()).readiness
    expect(readiness.UnknownPayments).toBe('1')
    expect(readiness.QuoteMatches).toBe(true)
    expect(readiness.EntitlementMatches).toBe(true)
    expect(readiness.ProvenanceComplete).toBe(true)
    expect(readiness.Ready).toBe(false)
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/switch-read`)
    await fillLimits()
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    expect((await (await page.request.get(accountURL)).json()).migration.Link.ReadOwner).toBe('legacy')

    await page.goto(`${app.baseURL}/admin/commands/${commandID}`)
    const resume = page.getByRole('button', { name: '重新查證' })
    await expect(resume).toBeVisible()
    await resume.click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(scalar('SELECT status FROM admin_commands WHERE id=?', commandID)).toBe('succeeded')
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    await page.goto(`${app.baseURL}/admin/jobs/entitlement-refresh`)
    await confirmPreview('確認刷新權益')
    await recordEntitlement('active')
    await reconcile()
    readiness = (await (await page.request.get(readinessURL)).json()).readiness
    expect(readiness.UnknownPayments).toBe('0')
    expect(readiness.Ready).toBe(true)
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/switch-read`)
    await fillLimits()
    await confirmPreview('確認切換讀取')
    await page.goto(`${app.baseURL}/admin/account-migrations/${encodeURIComponent(legacyID)}/switch-writer`)
    await fillLimits()
    await confirmPreview('確認切換寫入')
    const migration = (await (await page.request.get(accountURL)).json()).migration
    expect(migration.Link.ReadOwner).toBe('commerce')
    expect(migration.Link.WriterOwner).toBe('commerce')
  })

  test('C12 applies funded credit to a later invoice once and keeps the command receipt', async ({ page }) => {
    await signIn(page)
    await page.goto(`${app.baseURL}/admin/lab/clock`)
    await page.getByRole('combobox', { name: /模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('實際時間', { exact: true }).click()
    await page.getByRole('button', { name: '確認設定時鐘' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const customerID = `apply-credit-browser-${randomUUID()}`
    const { subscriptionID, invoiceID: sourceInvoiceID } = await createPaidSubscription(page, customerID, 'basic')
    await page.goto(`${app.baseURL}/admin/invoices/${sourceInvoiceID}/reductions/new`)
    await page.getByLabel('減額（最小貨幣單位）', { exact: true }).fill('1000')
    await page.getByLabel('減額理由', { exact: true }).fill('funded credit for renewal')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認減額' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const grantID = scalar('SELECT id FROM credit_grants WHERE source_invoice_id=?', sourceInvoiceID)
    expect(count('SELECT amount_minor FROM credit_grants WHERE id=?', grantID)).toBe(1000)

    const periodEnd = BigInt(scalar('SELECT period_end FROM billing_periods WHERE subscription_id=? AND period_index=0', subscriptionID))
    const renewalTime = new Date(Number((periodEnd + 1_000_000_000n) / 1_000_000n)).toISOString()
    await page.goto(`${app.baseURL}/admin/lab/clock`)
    await page.getByRole('button', { name: '執行另一個操作' }).click()
    await page.getByRole('combobox', { name: /模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('固定時間', { exact: true }).click()
    await page.getByRole('textbox', { name: /固定 UTC 時間/ }).fill(renewalTime)
    await page.getByRole('button', { name: '確認設定時鐘' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    await page.goto(`${app.baseURL}/admin/jobs/renewals`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認續約批次' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認續約批次' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const targetInvoiceID = scalar('SELECT invoice_id FROM billing_periods WHERE subscription_id=? AND period_index=1', subscriptionID)
    const targetTotal = count('SELECT total_minor FROM invoices WHERE id=?', targetInvoiceID)
    expect(targetTotal).toBeGreaterThan(500)

    await page.goto(`${app.baseURL}/admin/credits/${grantID}/apply`)
    await page.getByLabel('目標帳單 ID', { exact: true }).fill(targetInvoiceID)
    await page.getByLabel('抵扣金額（最小貨幣單位）', { exact: true }).fill('1001')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('無法建立預覽')).toBeVisible()
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C12' AND target_id=?", grantID)).toBe(0)
    await page.getByLabel('抵扣金額（最小貨幣單位）', { exact: true }).fill('500')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()
    await expect(page.getByText('grant_available_before_minor', { exact: true })).toBeVisible()
    await expect(page.getByText('將取消 1 筆尚未送出的付款操作。')).toBeVisible()
    await expect(page.getByText('若要收取剩餘金額，請建立新的付款操作。')).toBeVisible()

    const other = await page.context().newPage()
    try {
      await other.goto(`${app.baseURL}/admin/credits/${grantID}/refunds/new`)
      await other.getByLabel('退款金額（最小貨幣單位）', { exact: true }).fill('500')
      await other.getByRole('button', { name: '建立預覽' }).click()
      await other.getByRole('button', { name: '確認預留退款' }).last().click()
      await other.getByRole('dialog').getByRole('button', { name: '確認預留退款' }).click()
      await expect(other.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    } finally {
      await other.close()
    }
    expect(count('SELECT amount_minor FROM refund_operations WHERE grant_id=?', grantID)).toBe(500)
    const staleResponse = page.waitForResponse((response) => response.url().endsWith('/admin/api/commands') && response.request().method() === 'POST')
    await page.getByRole('button', { name: '確認抵扣' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認抵扣' }).click()
    expect((await staleResponse).status()).toBe(409)
    await expect(page.getByText('原預覽已失效', { exact: false }).first()).toBeVisible()
    await expect(page.getByText('來源 grant_reserved_minor', { exact: true })).toBeVisible()
    const staleCommandID = scalar("SELECT id FROM admin_commands WHERE action_id='C12' AND target_id=? ORDER BY rowid DESC LIMIT 1", grantID)
    expect(scalar('SELECT error_code FROM admin_commands WHERE id=?', staleCommandID)).toBe('PREVIEW_STALE')
    expect(count('SELECT COUNT(*) FROM credit_applications WHERE grant_id=?', grantID)).toBe(0)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', staleCommandID)).toBe(0)
    await page.getByRole('button', { name: '確認抵扣' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認抵扣' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const commandID = scalar("SELECT id FROM admin_commands WHERE action_id='C12' AND target_id=? ORDER BY rowid DESC LIMIT 1", grantID)
    const applicationID = scalar('SELECT id FROM credit_applications WHERE request_key=?', `admin:${commandID}`)
    expect(count('SELECT amount_minor FROM credit_applications WHERE id=?', applicationID)).toBe(500)
    expect(scalar('SELECT invoice_id FROM credit_applications WHERE id=?', applicationID)).toBe(targetInvoiceID)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID)).toBe(1)
    expect(scalar("SELECT json_extract(result_refs_json,'$.application_id') FROM admin_command_receipts WHERE command_id=?", commandID)).toBe(applicationID)
    expect(count("SELECT COUNT(*) FROM admin_commands WHERE action_id='C12' AND target_id=?", grantID)).toBe(2)
    expect(count('SELECT SUM(amount_minor) FROM refund_operations WHERE grant_id=?', grantID) + count('SELECT SUM(amount_minor) FROM credit_applications WHERE grant_id=?', grantID)).toBe(1000)

    await page.goto(`${app.baseURL}/admin/commands/${commandID}`)
    await expect(page.getByText('命令詳情', { exact: true })).toBeVisible()
    await expect(page.getByText('succeeded', { exact: true })).toBeVisible()
    await expect(page.getByText(applicationID, { exact: false })).toBeVisible()
    await page.reload()
    await expect(page.getByText('succeeded', { exact: true })).toBeVisible()
    expect(count('SELECT COUNT(*) FROM credit_applications WHERE grant_id=?', grantID)).toBe(1)
    expect(count('SELECT SUM(amount_minor) FROM credit_applications WHERE invoice_id=?', targetInvoiceID)).toBe(500)
    await page.goto(`${app.baseURL}/admin/invoices/${targetInvoiceID}`)
    await expect(page.getByRole('row', { name: /已應用 Credit/ }).getByText('USD 5.00', { exact: true })).toBeVisible()
    await expect(page.getByRole('row', { name: /尚待支付/ }).getByText(`USD ${((targetTotal - 500) / 100).toFixed(2)}`, { exact: true })).toBeVisible()
    await expect(page.getByText('抵扣本帳單的 Credit', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '查看完整歷史' }).click()
    await expect(page).toHaveURL(new RegExp(`/admin/invoices/${targetInvoiceID}/history/applications$`))
    await expect(page.getByRole('row').filter({ hasText: applicationID }).getByText('USD 5.00', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '返回帳單詳情' }).click()
    await page.getByRole('row').filter({ hasText: applicationID }).getByRole('button', { name: sourceInvoiceID }).click()
    await expect(page).toHaveURL(new RegExp(`/admin/invoices/${sourceInvoiceID}$`))
    await expect(page.locator('.ant-card-head-title').getByText('減額更正', { exact: true })).toBeVisible()
    await expect(page.getByText('funded credit for renewal', { exact: true })).toBeVisible()
    const reductionCommandID = scalar("SELECT id FROM admin_commands WHERE action_id='C11' AND target_id=? ORDER BY rowid DESC LIMIT 1", sourceInvoiceID)
    const reductionRow = page.getByRole('row').filter({ hasText: 'funded credit for renewal' })
    await expect(reductionRow.getByText('人工減額命令', { exact: true })).toBeVisible()
    await expect(reductionRow.getByRole('button', { name: reductionCommandID })).toBeVisible()
    await expect(page.getByText('本帳單釋出的 Credit', { exact: true })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: grantID }).getByText('USD 10.00', { exact: true })).toBeVisible()
    await page.locator('.ant-card').filter({ has: page.locator('.ant-card-head-title').getByText('減額更正', { exact: true }) }).getByRole('button', { name: '查看完整歷史' }).click()
    await expect(page).toHaveURL(new RegExp(`/admin/invoices/${sourceInvoiceID}/history/corrections$`))
    const correctionID = scalar('SELECT id FROM corrections WHERE invoice_id=?', sourceInvoiceID)
    await expect(page.getByRole('row').filter({ hasText: correctionID }).getByText('USD 10.00', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '返回帳單詳情' }).click()
    await page.locator('.ant-card').filter({ has: page.locator('.ant-card-head-title').getByText('本帳單釋出的 Credit', { exact: true }) }).getByRole('button', { name: '查看完整歷史' }).click()
    await expect(page).toHaveURL(new RegExp(`/admin/invoices/${sourceInvoiceID}/history/grants$`))
    await expect(page.getByRole('row').filter({ hasText: grantID }).getByText('USD 10.00', { exact: true })).toBeVisible()

    const oldOperationID = scalar("SELECT id FROM payment_operations WHERE invoice_id=? AND status='cancelled' ORDER BY rowid LIMIT 1", targetInvoiceID)
    const oldProviderKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', oldOperationID)
    expect(scalar('SELECT status FROM outbox WHERE id=?', `capture:${oldOperationID}`)).toBe('done')
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', oldProviderKey, app.providerPath)).toBe('0')

    await page.goto(`${app.baseURL}/admin/invoices/${targetInvoiceID}/payments/new`)
    await page.getByRole('textbox', { name: '付款金額（最小貨幣單位）' }).fill(String(targetTotal - 500))
    await page.getByRole('button', { name: '預覽付款' }).click()
    await page.getByRole('button', { name: '確認建立付款' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認建立' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const remainingOperationID = scalar("SELECT id FROM payment_operations WHERE invoice_id=? AND status='created' ORDER BY rowid DESC LIMIT 1", targetInvoiceID)
    expect(remainingOperationID).not.toBe(oldOperationID)
    expect(count('SELECT amount_minor FROM payment_operations WHERE id=?', remainingOperationID)).toBe(targetTotal - 500)
    await page.goto(`${app.baseURL}/admin/payments/${remainingOperationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const remainingProviderKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', remainingOperationID)
    expect(scalar('SELECT amount_minor FROM captures WHERE provider_key=?', remainingProviderKey, app.providerPath)).toBe(String(targetTotal - 500))
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', oldProviderKey, app.providerPath)).toBe('0')
    await page.goto(`${app.baseURL}/admin/invoices/${targetInvoiceID}`)
    await expect(page.getByRole('row', { name: /尚待支付/ }).getByText('USD 0.00', { exact: true })).toBeVisible()

    await page.goto(`${app.baseURL}/admin/credits/${grantID}`)
    const available = page.getByRole('row').filter({ has: page.getByRole('rowheader', { name: '可用額度', exact: true }) })
    await expect(available.getByRole('cell', { name: 'USD 0.00', exact: true })).toBeVisible()
    await expect(page.getByText('目前沒有可用額度', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '抵扣帳單', exact: true })).toBeDisabled()
    await expect(page.getByRole('button', { name: '預留退款', exact: true })).toBeDisabled()
  })

  test('C11 reduces an unpaid invoice cancels the old collection and captures only remainder', async ({ page }) => {
    await signIn(page)
    const { invoiceID, operationID, subscriptionID } = await createAcceptedSubscription(page, `reduction-collection-${randomUUID()}`, 'basic')
    const oldProviderKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', operationID)
    expect(count('SELECT amount_minor FROM payment_operations WHERE id=?', operationID)).toBe(2000)

    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/reductions/new`)
    await page.getByLabel('減額（最小貨幣單位）', { exact: true }).fill('500')
    await page.getByLabel('減額理由', { exact: true }).fill('adjust before collection')
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('將取消 1 筆尚未送出的付款操作。')).toBeVisible()
    await expect(page.getByText('若要收取剩餘金額，請建立新的付款操作。')).toBeVisible()
    await page.getByRole('button', { name: '確認減額' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認減額' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(scalar('SELECT status FROM payment_operations WHERE id=?', operationID)).toBe('cancelled')
    expect(scalar('SELECT status FROM outbox WHERE id=?', `capture:${operationID}`)).toBe('done')
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', oldProviderKey, app.providerPath)).toBe('0')
    expect(count('SELECT COUNT(*) FROM credit_grants WHERE source_invoice_id=?', invoiceID)).toBe(0)

    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}/payments/new`)
    await page.getByRole('textbox', { name: '付款金額（最小貨幣單位）' }).fill('1500')
    await page.getByRole('button', { name: '預覽付款' }).click()
    await page.getByRole('button', { name: '確認建立付款' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認建立' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const remainingOperationID = scalar("SELECT id FROM payment_operations WHERE invoice_id=? AND status='created' ORDER BY rowid DESC LIMIT 1", invoiceID)
    expect(remainingOperationID).not.toBe(operationID)
    expect(count('SELECT amount_minor FROM payment_operations WHERE id=?', remainingOperationID)).toBe(1500)
    await page.goto(`${app.baseURL}/admin/payments/${remainingOperationID}/dispatch`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await page.getByRole('button', { name: '確認送出付款' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認送出付款' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const newProviderKey = scalar('SELECT provider_key FROM payment_operations WHERE id=?', remainingOperationID)
    expect(scalar('SELECT amount_minor FROM captures WHERE provider_key=?', newProviderKey, app.providerPath)).toBe('1500')
    expect(scalar('SELECT COUNT(*) FROM captures WHERE provider_key=?', oldProviderKey, app.providerPath)).toBe('0')
    expect(scalar('SELECT status FROM subscriptions WHERE id=?', subscriptionID)).toBe('active')
    await page.goto(`${app.baseURL}/admin/invoices/${invoiceID}`)
    await expect(page.getByRole('row', { name: /尚待支付/ }).getByText('USD 0.00', { exact: true })).toBeVisible()
  })

  test('C45 keeps the preview membership when another subscription becomes eligible', async ({ page }) => {
    await signIn(page)
    await page.goto(`${app.baseURL}/admin/lab/clock`)
    await page.getByRole('combobox', { name: /模式/ }).click()
    await page.locator('.ant-select-dropdown:visible').getByText('實際時間', { exact: true }).click()
    await page.getByRole('button', { name: '確認設定時鐘' }).click()
    await page.getByRole('dialog').getByRole('button', { name: '確認設定時鐘' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()

    const first = await createPaidSubscription(page, `job-member-first-${randomUUID()}`, 'basic')
    await page.goto(`${app.baseURL}/admin/jobs/entitlement-refresh`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()
    const snapshotJSON = scalar('SELECT source_versions_json FROM admin_previews WHERE action_id=? ORDER BY rowid DESC LIMIT 1', 'C45')
    const previewMembers = (JSON.parse(snapshotJSON) as { items: { subscription_id: string }[] }).items
    expect(previewMembers.some((item) => item.subscription_id === first.subscriptionID)).toBe(true)

    const other = await page.context().newPage()
    const secondSubscriptionID = await (async () => {
      try {
        const second = await createPaidSubscription(other, `job-member-later-${randomUUID()}`, 'basic')
        return second.subscriptionID
      } finally {
        await other.close()
      }
    })()
    expect(previewMembers.some((item) => item.subscription_id === secondSubscriptionID)).toBe(false)

    await page.getByRole('button', { name: '確認刷新權益' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認刷新權益' }).click()
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    const commandID = scalar('SELECT id FROM admin_commands WHERE action_id=? ORDER BY rowid DESC LIMIT 1', 'C45')
    const jobID = `job:${commandID}`
    expect(count('SELECT COUNT(*) FROM admin_job_items WHERE job_id=?', jobID)).toBe(previewMembers.length)
    const jobMembers = scalar('SELECT GROUP_CONCAT(target_id) FROM admin_job_items WHERE job_id=?', jobID).split(',')
    expect(jobMembers).toContain(first.subscriptionID)
    expect(jobMembers).not.toContain(secondSubscriptionID)
    await page.goto(`${app.baseURL}/admin/jobs/${encodeURIComponent(jobID)}`)
    await expect(page.getByRole('main').getByText('批次工作', { exact: true })).toBeVisible()
    await expect(page.getByText(`${previewMembers.length} / ${previewMembers.length}`, { exact: true })).toBeVisible()
    await page.reload()
    await expect(page.getByText(`${previewMembers.length} / ${previewMembers.length}`, { exact: true })).toBeVisible()
  })

  test('a lost batch response recovers the original entitlement refresh job', async ({ page }) => {
    await signIn(page)
    const { subscriptionID } = await createPaidSubscription(page, `job-recovery-${randomUUID()}`, 'basic')
    const beforeCommands = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C45')
    await page.goto(`${app.baseURL}/admin/jobs/entitlement-refresh`)
    await page.getByRole('button', { name: '建立預覽' }).click()
    await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()
    const sources = JSON.parse(scalar('SELECT source_versions_json FROM admin_previews WHERE action_id=? ORDER BY rowid DESC LIMIT 1', 'C45')) as { items: { subscription_id: string }[] }
    expect(sources.items.some((item) => item.subscription_id === subscriptionID)).toBe(true)
    let originalKey = ''
    let dropped = false
    await page.route('**/admin/api/commands', async (route) => {
      if (!dropped && route.request().method() === 'POST') {
        dropped = true
        originalKey = route.request().headers()['idempotency-key']
        await commitThenDropResponse(page, route)
      } else {
        await route.continue()
      }
    })
    await page.getByRole('button', { name: '確認刷新權益' }).last().click()
    await page.getByRole('dialog').getByRole('button', { name: '確認刷新權益' }).click()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    expect(originalKey).toBeTruthy()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C45')).toBe(beforeCommands + 1)
    const commandID = scalar('SELECT id FROM admin_commands WHERE idempotency_key=?', originalKey)
    const jobID = `job:${commandID}`
    expect(count('SELECT COUNT(*) FROM admin_jobs WHERE id=?', jobID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_job_items WHERE job_id=?', jobID)).toBe(sources.items.length)

    await page.reload()
    await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
    const replay = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST')
    await page.getByRole('button', { name: '用原 request key 查詢' }).click()
    expect((await replay).headers()['idempotency-key']).toBe(originalKey)
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C45')).toBe(beforeCommands + 1)
    expect(count('SELECT COUNT(*) FROM admin_jobs WHERE id=?', jobID)).toBe(1)
    expect(count('SELECT COUNT(*) FROM admin_job_items WHERE job_id=?', jobID)).toBe(sources.items.length)
    await page.getByRole('button', { name: '查看逐項進度' }).click()
    await expect(page).toHaveURL(new RegExp(`/jobs/${encodeURIComponent(jobID)}$`))
  })

  test('a lost renewal batch response recovers one invoice and payment obligation', async ({ page }) => {
    const isolated = await startLocalAdmin({ seedDemo: true })
    try {
      await page.goto(`${isolated.baseURL}/admin/login`)
      await page.getByRole('textbox', { name: /帳號/ }).fill(isolated.username)
      await page.getByRole('textbox', { name: /密碼/ }).fill(isolated.password)
      await page.getByRole('button', { name: '登 入' }).click()
      await expect(page.getByRole('heading', { name: '營運概覽' })).toBeVisible()
      const subscriptionID = scalar('SELECT id FROM subscriptions WHERE customer_id=?', 'demo-customer', isolated.commercePath)
      const providerCaptures = Number(scalar('SELECT COUNT(*) FROM captures WHERE ? IS NOT NULL', 'seed', isolated.providerPath))
      const periodEnd = BigInt(scalar('SELECT period_end FROM billing_periods WHERE subscription_id=? AND period_index=0', subscriptionID, isolated.commercePath))
      const renewalAt = new Date(Number((periodEnd + 1_000_000_000n) / 1_000_000n)).toISOString()
      const sessionResponse = await page.request.get(`${isolated.baseURL}/admin/api/session`)
      const session = await sessionResponse.json() as { csrf_token: string }
      await submitClockControl(page, isolated.baseURL, session.csrf_token, 'fixed', renewalAt)
      await page.goto(`${isolated.baseURL}/admin/jobs/renewals`)
      await page.getByRole('button', { name: '建立預覽' }).click()
      await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()
      const sources = JSON.parse(scalar('SELECT source_versions_json FROM admin_previews WHERE action_id=? ORDER BY rowid DESC LIMIT 1', 'C44', isolated.commercePath)) as { items: { subscription_id: string }[] }
      expect(sources.items.some((item) => item.subscription_id === subscriptionID)).toBe(true)
      let originalKey = ''
      let dropped = false
      await page.route('**/admin/api/commands', async (route) => {
        if (!dropped && route.request().method() === 'POST') {
          dropped = true
          originalKey = route.request().headers()['idempotency-key']
          await commitThenDropResponse(page, route)
        } else {
          await route.continue()
        }
      })
      await page.getByRole('button', { name: '確認續約批次' }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: '確認續約批次' }).click()
      await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
      expect(originalKey).toBeTruthy()
      const commandID = scalar('SELECT id FROM admin_commands WHERE idempotency_key=?', originalKey, isolated.commercePath)
      const jobID = `job:${commandID}`
      expect(Number(scalar('SELECT COUNT(*) FROM admin_jobs WHERE id=?', jobID, isolated.commercePath))).toBe(1)
      expect(Number(scalar('SELECT COUNT(*) FROM admin_job_items WHERE job_id=?', jobID, isolated.commercePath))).toBe(sources.items.length)
      expect(Number(scalar('SELECT COUNT(*) FROM billing_periods WHERE subscription_id=?', subscriptionID, isolated.commercePath))).toBe(2)
      const invoiceID = scalar('SELECT invoice_id FROM billing_periods WHERE subscription_id=? AND period_index=1', subscriptionID, isolated.commercePath)
      expect(Number(scalar('SELECT total_minor FROM invoices WHERE id=?', invoiceID, isolated.commercePath))).toBe(2000)
      expect(Number(scalar('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID, isolated.commercePath))).toBe(1)
      expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE ? IS NOT NULL', 'seed', isolated.providerPath))).toBe(providerCaptures)

      await page.reload()
      await expect(page.getByText('原命令的結果尚未確認')).toBeVisible()
      const replay = page.waitForRequest((request) => request.url().endsWith('/admin/api/commands') && request.method() === 'POST')
      await page.getByRole('button', { name: '用原 request key 查詢' }).click()
      expect((await replay).headers()['idempotency-key']).toBe(originalKey)
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      expect(Number(scalar('SELECT COUNT(*) FROM admin_jobs WHERE id=?', jobID, isolated.commercePath))).toBe(1)
      expect(Number(scalar('SELECT COUNT(*) FROM admin_job_items WHERE job_id=?', jobID, isolated.commercePath))).toBe(sources.items.length)
      expect(Number(scalar('SELECT COUNT(*) FROM billing_periods WHERE subscription_id=?', subscriptionID, isolated.commercePath))).toBe(2)
      expect(Number(scalar('SELECT COUNT(*) FROM payment_operations WHERE invoice_id=?', invoiceID, isolated.commercePath))).toBe(1)
      expect(Number(scalar('SELECT COUNT(*) FROM admin_command_receipts WHERE command_id=?', commandID, isolated.commercePath))).toBe(1)
      expect(Number(scalar('SELECT COUNT(*) FROM captures WHERE ? IS NOT NULL', 'seed', isolated.providerPath))).toBe(providerCaptures)
    } finally {
      await isolated.stop()
    }
  })

  test('job detail keeps waiting verification separate from completed progress', async ({ page }) => {
    await signIn(page)
    await page.route('**/admin/api/jobs/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'job:waiting-display', command_id: 'cmd_waiting_display', kind: 'C45', status: 'partial',
          created_at: '2026-09-27T00:00:00Z', updated_at: '2026-09-27T00:00:00Z',
          items: [
            { id: 'item_done', target_type: 'subscription', target_id: 'sub_done', period_key: '0', status: 'succeeded', updated_at: '2026-09-27T00:00:00Z' },
            { id: 'item_waiting', target_type: 'subscription', target_id: 'sub_waiting', period_key: '0', status: 'waiting_verification', error_code: 'PROVIDER_UNKNOWN', updated_at: '2026-09-27T00:00:00Z' },
          ],
        }),
      })
    })
    await page.goto(`${app.baseURL}/admin/jobs/job%3Awaiting-display`)
    await expect(page.getByRole('row', { name: /已完成 \/ 總數/ }).getByText('1 / 2', { exact: true })).toBeVisible()
    await expect(page.getByRole('row', { name: /成功/ }).getByText('1', { exact: true })).toBeVisible()
    await expect(page.getByRole('row', { name: /待查證/ }).getByText('1', { exact: true })).toBeVisible()
    await expect(page.getByRole('row', { name: /sub_waiting/ }).getByText('PROVIDER_UNKNOWN', { exact: true })).toBeVisible()
  })

  test('invoice history page follows the cursor and returns to an earlier page', async ({ page }) => {
    await signIn(page)
    await page.route('**/admin/api/invoices/inv_history_mock', async (route) => {
      await route.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify({
          invoice: {
            ID: 'inv_history_mock', SubscriptionID: 'sub_history_mock', FinalizedAt: '2026-09-27T00:00:00Z', Period: null,
            Balance: { Currency: 'USD', OriginalMinor: '2000', ReductionsMinor: '101', ObligationMinor: '1899', GrossCapturedMinor: '2000', ReleasedMinor: '101', CreditAppliedMinor: '0', NetAppliedMinor: '1899', OutstandingMinor: '0' },
            Lines: [], Payments: [], Corrections: [], CorrectionsTruncated: true, CreditApplications: [], CreditGrants: [], Refunds: [],
          },
          observed_at: '2026-09-27T00:00:00Z',
        }),
      })
    })
    await page.route('**/admin/api/invoices/inv_history_mock/history/corrections*', async (route) => {
      const after = new URL(route.request().url()).searchParams.get('cursor')
      const id = after ? 'corr_older' : 'corr_newer'
      await route.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify({
          items: [{ ID: id, ReductionMinor: '100', PriorObligationMinor: '2000', NewObligationMinor: '1900', Reason: 'history', OriginKind: 'domain_request', OriginID: id, CreatedAt: '2026-09-27T00:00:00Z' }],
          next_cursor: after ? '' : 'next', currency: 'USD', observed_at: '2026-09-27T00:00:00Z',
        }),
      })
    })
    await page.goto(`${app.baseURL}/admin/invoices/inv_history_mock`)
    await expect(page.getByText('僅顯示最近 100 筆減額更正', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: '查看完整歷史' }).click()
    await expect(page).toHaveURL(/\/admin\/invoices\/inv_history_mock\/history\/corrections$/)
    await expect(page.getByRole('row').filter({ hasText: 'corr_newer' })).toBeVisible()
    await page.getByRole('button', { name: '下一頁' }).click()
    await expect(page.getByText('第 2 頁', { exact: true })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: 'corr_older' })).toBeVisible()
    await expect(page.getByRole('row').filter({ hasText: 'corr_newer' })).toHaveCount(0)
    await page.getByRole('button', { name: '上一頁' }).click()
    await expect(page.getByRole('row').filter({ hasText: 'corr_newer' })).toBeVisible()
  })

  test('AI meter price can be quoted and accepted while existing task usage still works', async ({ page }) => {
    await signIn(page)
    const suffix = randomUUID().replaceAll('-', '')
    const meterID = `ai_tokens_${suffix}`
    const planID = `ai_browser_${suffix}`
    const priceID = `ai_price_${suffix}`
    const clockResponse = await page.request.get(`${app.baseURL}/admin/api/lab/clock`)
    expect(clockResponse.status()).toBe(200)
    const clock = await clockResponse.json() as { business_time: string }
    const effective = new Date(Date.parse(clock.business_time) - 60_000).toISOString().replace(/\.\d{3}Z$/, 'Z')
    async function publish(route: string, fields: Array<[string, string]>, confirm: string) {
      await page.goto(`${app.baseURL}${route}`)
      for (const [label, value] of fields) await page.getByLabel(label, { exact: true }).fill(value)
      await page.getByRole('button', { name: '建立預覽' }).click()
      await expect(page.getByText('操作預覽', { exact: true })).toBeVisible()
      if (route === '/admin/prices/metered/new' || route === '/admin/catalog-selections/new') {
        await expect(page.getByText(effective, { exact: true }).first()).toBeVisible()
      }
      await page.getByRole('button', { name: confirm }).last().click()
      await page.getByRole('dialog').getByRole('button', { name: confirm }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    }
    await publish('/admin/meters/new', [
      ['計量表 ID', meterID], ['事件來源', 'ai_gateway'],
      ['計量單位', 'token'], ['Schema 版本', '1'],
    ], '確認註冊')
    await publish('/admin/prices/metered/new', [
      ['價格版本 ID', priceID], ['方案 ID', planID], ['版本號', '1'],
      ['固定金額（最小單位）', '3000'], ['每席金額（最小單位）', '0'],
      ['計量表 ID', meterID], ['包含用量', '100'],
      ['超額費率分子', '1'], ['超額費率分母', '5'],
      ['生效起點（UTC）', effective],
    ], '確認發布價格')
    await publish('/admin/catalog-selections/new', [
      ['方案 ID', planID], ['Cohort', 'default'],
      ['生效時間（UTC）', effective], ['價格版本 ID', priceID],
    ], '確認選價')
    const pro = await createPaidSubscription(page, `tasks-customer-${suffix}`, 'pro', '5')
    const aiCustomerID = `ai-customer-${suffix}`
    const ai = await createPaidSubscription(page, aiCustomerID, planID)
    expect(scalar('SELECT price_version_id FROM quotes WHERE id=?', ai.quoteID)).toBe(priceID)
    expect(scalar('SELECT amount_minor FROM quotes WHERE id=?', ai.quoteID)).toBe('3000')
    expect(scalar('SELECT total_minor FROM invoices WHERE id=?', ai.invoiceID)).toBe('3000')
    await page.goto(`${app.baseURL}/admin/quotes?customer_id=${encodeURIComponent(aiCustomerID)}`)
    await expect(page.getByRole('row').filter({ hasText: ai.quoteID }).getByText('USD 30.00', { exact: true })).toBeVisible()

    const usageClock = await page.request.get(`${app.baseURL}/admin/api/lab/clock`)
    expect(usageClock.status()).toBe(200)
    const occurredAt = (await usageClock.json() as { business_time: string }).business_time
    async function recordUsage(subscriptionID: string, source: string, meter: string, quantity: string) {
      const eventID = `usage_${randomUUID()}`
      await page.goto(`${app.baseURL}/admin/usage-events/new`)
      const subscriptionField = page.getByLabel('訂閱 ID', { exact: true })
      await expect(subscriptionField).toBeVisible()
      if (await subscriptionField.isDisabled()) await page.getByRole('button', { name: '記錄另一筆事件' }).click()
      for (const [label, value] of [
        ['訂閱 ID', subscriptionID], ['Meter ID', meter], ['來源', source],
        ['事件 ID', eventID], ['發生時間（UTC）', occurredAt], ['數量', quantity],
      ]) await page.getByLabel(label, { exact: true }).fill(value)
      await page.getByRole('button', { name: '檢查並記錄' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認記錄' }).click()
      await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
      return eventID
    }
    const taskEvent = await recordUsage(pro.subscriptionID, 'worker', 'tasks', '7')
    const tokenEvent = await recordUsage(ai.subscriptionID, 'ai_gateway', meterID, '105')
    expect(scalar('SELECT quantity FROM usage_events WHERE event_id=?', taskEvent)).toBe('7')
    expect(scalar('SELECT meter_id FROM usage_events WHERE event_id=?', taskEvent)).toBe('tasks')
    expect(scalar('SELECT quantity FROM usage_events WHERE event_id=?', tokenEvent)).toBe('105')
    expect(scalar('SELECT meter_id FROM usage_events WHERE event_id=?', tokenEvent)).toBe(meterID)
  })

  test('C26 replays the same usage event but rejects a changed event payload', async ({ page }) => {
    await signIn(page)
    const subscription = await createPaidSubscription(page, `usage-replay-${randomUUID()}`, 'pro', '5')
    const clockResponse = await page.request.get(`${app.baseURL}/admin/api/lab/clock`)
    expect(clockResponse.status()).toBe(200)
    const occurredAt = (await clockResponse.json() as { business_time: string }).business_time
    const eventID = `usage-replay-${randomUUID()}`
    const quantity = '9007199254740993'
    await page.goto(`${app.baseURL}/admin/usage-events/new`)
    async function fillUsage(quantity: string) {
      for (const [label, value] of [
        ['訂閱 ID', subscription.subscriptionID], ['Meter ID', 'tasks'],
        ['來源', 'worker'], ['事件 ID', eventID],
        ['發生時間（UTC）', occurredAt], ['數量', quantity],
      ]) await page.getByLabel(label, { exact: true }).fill(value)
    }
    async function submitUsage(quantity: string) {
      await fillUsage(quantity)
      await page.getByRole('button', { name: '檢查並記錄' }).click()
      await page.getByRole('dialog').getByRole('button', { name: '確認記錄' }).click()
    }
    await submitUsage(quantity)
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM usage_events WHERE source=? AND event_id=?', 'worker', eventID)).toBe(1)
    await page.getByRole('button', { name: '記錄另一筆事件' }).click()
    await submitUsage(quantity)
    await expect(page.getByRole('main').getByText('succeeded', { exact: true }).first()).toBeVisible()
    expect(count('SELECT COUNT(*) FROM usage_events WHERE source=? AND event_id=?', 'worker', eventID)).toBe(1)
    await page.getByRole('button', { name: '記錄另一筆事件' }).click()
    await submitUsage('9007199254740994')
    await expect(page.getByRole('main').getByText('failed', { exact: true }).first()).toBeVisible()
    await expect(page.getByText('DOMAIN_REJECTED', { exact: true })).toBeVisible()
    expect(count('SELECT COUNT(*) FROM usage_events WHERE source=? AND event_id=?', 'worker', eventID)).toBe(1)
    expect(scalar('SELECT quantity FROM usage_events WHERE event_id=?', eventID)).toBe(quantity)

    await page.getByRole('button', { name: '記錄另一筆事件' }).click()
    const commandsBefore = count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C26')
    await fillUsage('9223372036854775808')
    await page.getByRole('button', { name: '檢查並記錄' }).click()
    await expect(page.getByText('數量不可超過 int64 上限')).toBeVisible()
    await expect(page.getByRole('dialog')).toHaveCount(0)
    expect(count('SELECT COUNT(*) FROM admin_commands WHERE action_id=?', 'C26')).toBe(commandsBefore)
  })
  test('completed previewed actions replay their original receipt after preview expiry', async ({ page }) => {
    await signIn(page)
    const sessionResponse = await page.request.get(`${app.baseURL}/admin/api/session`)
    const session = await sessionResponse.json() as { csrf_token: string }
    const script = `
import json, sqlite3, sys
db = sqlite3.connect(sys.argv[1])
rows = db.execute('''SELECT c.action_id,c.id,c.idempotency_key,c.target_id,c.preview_id,c.payload_json
  FROM admin_commands c WHERE c.actor_id='local-admin' AND c.status='succeeded'
  AND COALESCE(c.preview_id,'')<>''
  AND EXISTS(SELECT 1 FROM admin_command_receipts r WHERE r.command_id=c.id)
  ORDER BY c.rowid''').fetchall()
selected = {}
for row in rows:
    selected.setdefault(row[0], row)
db.executemany("UPDATE admin_previews SET expires_at='2000-01-01T00:00:00Z' WHERE id=?", [(row[4],) for row in selected.values()])
db.commit()
print(json.dumps(list(selected.values())))
`
    const rows = JSON.parse(execFileSync('python3', ['-c', script, app.commercePath], { encoding: 'utf8' })) as Array<[string, string, string, string, string, string]>
    const expected = 'C02 C03 C04 C05 C06 C07 C08 C09 C11 C12 C13 C14 C15 C16 C18 C19 C20 C21 C22 C24 C25 C27 C28 C29 C30 C31 C32 C34 C35 C36 C39 C40 C41 C42 C43 C44 C45 C46 C47 C48 C49'.split(' ')
    expect(rows.map(([action]) => action).sort()).toEqual(expected.sort())
    const commandsBefore = count('SELECT COUNT(*) FROM admin_commands')
    const receiptsBefore = count('SELECT COUNT(*) FROM admin_command_receipts')
    for (const [action, commandID, key, targetID, previewID, payloadJSON] of rows) {
      const headers = { Origin: app.baseURL, 'X-CSRF-Token': session.csrf_token, 'Idempotency-Key': key }
      const original = { action_id: action, target_id: targetID, preview_id: previewID, payload: JSON.parse(payloadJSON) }
      const replay = await page.request.post(`${app.baseURL}/admin/api/commands`, { headers, data: original })
      expect(replay.status(), `${action} replay: ${await replay.text()}`).toBe(200)
      expect((await replay.json()).id).toBe(commandID)
      const changedIntent = await page.request.post(`${app.baseURL}/admin/api/commands`, {
        headers,
        data: { ...original, preview_id: 'prev_different_intent' },
      })
      expect(changedIntent.status(), `${action} changed intent: ${await changedIntent.text()}`).toBe(409)
      expect((await changedIntent.json()).error.code).toBe('IDEMPOTENCY_CONFLICT')
    }
    expect(count('SELECT COUNT(*) FROM admin_commands')).toBe(commandsBefore)
    expect(count('SELECT COUNT(*) FROM admin_command_receipts')).toBe(receiptsBefore)
  })
})
