import { randomUUID } from 'node:crypto'
import { expect, type Page } from '@playwright/test'

export async function submitClockControl(
  page: Page,
  baseURL: string,
  csrfToken: string,
  mode: 'fixed' | 'real',
  valueUTC?: string,
) {
  const payload = mode === 'fixed' ? { mode, value_utc: valueUTC } : { mode }
  const headers = { Origin: baseURL, 'X-CSRF-Token': csrfToken }
  const previewResponse = await page.request.post(`${baseURL}/admin/api/previews`, {
    headers,
    data: { action_id: 'C46', target_id: '', payload },
  })
  expect(previewResponse.ok(), `${previewResponse.status()} ${await previewResponse.text()}`).toBe(true)
  const preview = (await previewResponse.json()) as { preview_id: string }
  const response = await page.request.post(`${baseURL}/admin/api/commands`, {
    headers: { ...headers, 'Idempotency-Key': randomUUID() },
    data: { action_id: 'C46', target_id: '', payload, preview_id: preview.preview_id },
  })
  expect(response.ok(), `${response.status()} ${await response.text()}`).toBe(true)
  return (await response.json()) as { id: string }
}
