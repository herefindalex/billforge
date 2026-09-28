import { randomBytes } from 'node:crypto'
import { spawn, spawnSync, type ChildProcess } from 'node:child_process'
import { appendFileSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:net'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../../..')

async function availablePort(): Promise<number> {
  const server = createServer()
  await new Promise<void>((done) => server.listen(0, '127.0.0.1', done))
  const address = server.address()
  if (!address || typeof address === 'string') throw new Error('Could not reserve local port')
  const port = address.port
  await new Promise<void>((done) => server.close(() => done()))
  return port
}

export type LocalAdmin = {
  baseURL: string
  commercePath: string
  providerPath: string
  username: string
  password: string
  fileUsername: string
  filePassword: string
  internalToken: string
  logOutput: () => string
  restart: () => Promise<void>
  stop: () => Promise<void>
}

export async function startLocalAdmin(options: {
  seedDemo?: boolean
  omitFilePassword?: boolean
  capabilities?: string
  processCredentials?: { username: string; password: string }
} = {}): Promise<LocalAdmin> {
  const directory = mkdtempSync(join(tmpdir(), 'billforge-web-e2e-'))
  const binary = join(directory, 'lab')
  const commercePath = join(directory, 'commerce.db')
  const providerPath = join(directory, 'provider.db')
  const fileUsername = 'adminqa'
  const filePassword = `${randomBytes(16).toString('base64url')}#=!`
  const internalToken = `${randomBytes(24).toString('base64url')}:internal`
  const username = options.processCredentials?.username ?? fileUsername
  const password = options.processCredentials?.password ?? filePassword
  const envPath = join(directory, 'admin.env')
  const envContent = `BILLFORGE_ADMIN_USERNAME=${fileUsername}\n` +
    (options.omitFilePassword ? '' : `BILLFORGE_ADMIN_PASSWORD=${JSON.stringify(filePassword)}\n`)
  writeFileSync(envPath, envContent, { mode: 0o600 })

  const build = spawnSync('go', ['build', '-tags', 'admin_ui', '-o', binary, './cmd/lab'], {
    cwd: root,
    encoding: 'utf8',
  })
  if (build.status !== 0) {
    rmSync(directory, { recursive: true, force: true })
    throw new Error(`Go build failed: ${build.stderr || build.error?.message}`)
  }
  if (options.seedDemo) {
    const seed = spawnSync(binary, ['demo', commercePath, providerPath], { cwd: root, encoding: 'utf8' })
    if (seed.status !== 0) {
      rmSync(directory, { recursive: true, force: true })
      throw new Error(`Legacy CLI seed failed: ${seed.stderr || seed.error?.message}`)
    }
    const before = spawnSync('python3', ['-c', "import sqlite3,sys; db=sqlite3.connect(sys.argv[1]); print(db.execute(\"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='admin_schema_versions'\").fetchone()[0])", commercePath], { encoding: 'utf8' })
    if (before.status !== 0 || before.stdout.trim() !== '0') {
      rmSync(directory, { recursive: true, force: true })
      throw new Error('CLI seed unexpectedly contains an admin schema')
    }
  }

  const port = await availablePort()
  const baseURL = `http://127.0.0.1:${port}`
  let child: ChildProcess | null = null
  let output = ''

  async function launch(): Promise<void> {
    child = spawn(binary, ['admin', '--env-file', envPath, commercePath, providerPath, `127.0.0.1:${port}`], {
      cwd: root,
      env: {
        ...process.env,
        BILLFORGE_INTERNAL_TOKEN: internalToken,
        BILLFORGE_ADMIN_USERNAME: options.processCredentials?.username,
        BILLFORGE_ADMIN_PASSWORD: options.processCredentials?.password,
        BILLFORGE_ADMIN_CAPABILITIES: options.capabilities,
      },
      stdio: ['ignore', 'pipe', 'pipe'],
    })
    child.stdout?.on('data', (chunk: Buffer) => { output += chunk.toString() })
    child.stderr?.on('data', (chunk: Buffer) => { output += chunk.toString() })

    const deadline = Date.now() + 20_000
    while (Date.now() < deadline) {
      if (child.exitCode !== null || child.signalCode !== null) throw new Error(`Admin exited early: ${output}`)
      try {
        const response = await fetch(`${baseURL}/admin/api/session`)
        if (response.status === 401) return
      } catch { /* server still starting */ }
      await new Promise((done) => setTimeout(done, 100))
    }
    throw new Error(`Admin did not become ready: ${output}`)
  }

  async function shutdown(): Promise<void> {
    const active = child
    if (!active || active.exitCode !== null || active.signalCode !== null) return
    const exited = new Promise<void>((done) => active.once('exit', () => done()))
    active.kill('SIGINT')
    await Promise.race([exited, new Promise<void>((done) => setTimeout(done, 5_000))])
    if (active.exitCode === null && active.signalCode === null) {
      active.kill('SIGKILL')
      await exited
    }
  }

  try {
    await launch()
  } catch (error) {
    await shutdown()
    rmSync(directory, { recursive: true, force: true })
    throw error
  }

  return {
    baseURL,
    commercePath,
    providerPath,
    username,
    password,
    fileUsername,
    filePassword,
    internalToken,
    logOutput: () => output,
    async restart() {
      await shutdown()
      await launch()
    },
    async stop() {
      await shutdown()
      try {
        const auditPath = process.env.BILLFORGE_E2E_ACTION_AUDIT
        if (auditPath) {
          const result = spawnSync('python3', ['-c', `import json, sqlite3, sys
db = sqlite3.connect('file:' + sys.argv[1] + '?mode=ro', uri=True)
print(json.dumps([row[0] for row in db.execute('SELECT DISTINCT action_id FROM admin_commands ORDER BY action_id')]))`, commercePath], { encoding: 'utf8' })
          if (result.status !== 0) throw new Error(`Could not inspect E2E action IDs: ${result.stderr || result.error?.message}`)
          appendFileSync(auditPath, JSON.stringify({ actions: JSON.parse(result.stdout) }) + '\n')
        }
      } finally {
        rmSync(directory, { recursive: true, force: true })
      }
    },
  }
}
