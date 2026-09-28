import { existsSync } from 'node:fs'
import { defineConfig } from '@playwright/test'

const executablePath = process.env.BILLFORGE_CHROME_BIN
  ?? (existsSync('/usr/bin/google-chrome') ? '/usr/bin/google-chrome' : undefined)

export default defineConfig({
  testDir: './e2e',
  timeout: 120_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  reporter: 'list',
  use: {
    browserName: 'chromium',
    headless: true,
    actionTimeout: 10_000,
    launchOptions: executablePath ? { executablePath } : {},
    trace: 'retain-on-failure',
  },
})
