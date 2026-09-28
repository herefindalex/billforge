import { useEffect, useState } from 'react'

export function isPreviewExpired(expiresAt: string): boolean {
  const deadline = Date.parse(expiresAt)
  return !Number.isFinite(deadline) || Date.now() >= deadline
}

export function canConfirmPreview(preview: { expires_at: string; blocking_reasons?: string[] }): boolean {
  return !preview.blocking_reasons?.length && !isPreviewExpired(preview.expires_at)
}

export function usePreviewExpired(expiresAt?: string): boolean {
  const [expired, setExpired] = useState(() => expiresAt ? isPreviewExpired(expiresAt) : false)

  useEffect(() => {
    if (!expiresAt) {
      setExpired(false)
      return
    }
    const deadline = Date.parse(expiresAt)
    let timer: number | undefined
    const refresh = () => {
      const remaining = deadline - Date.now()
      const next = !Number.isFinite(deadline) || remaining <= 0
      setExpired(next)
      if (!next) timer = window.setTimeout(refresh, Math.min(remaining, 60_000))
    }
    refresh()
    return () => window.clearTimeout(timer)
  }, [expiresAt])

  return expiresAt ? expired || isPreviewExpired(expiresAt) : false
}
