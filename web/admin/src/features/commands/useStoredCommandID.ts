import { useState } from 'react'

function storageKey(actorID: string, scope: string, targetID: string) {
  return `billforge:admin:command:${actorID}:${scope}:${targetID}`
}

function read(key: string): string | null {
  try {
    return sessionStorage.getItem(key)
  } catch {
    return null
  }
}

export function useStoredCommandID(actorID: string, scope: string, targetID = '') {
  const key = storageKey(actorID, scope, targetID)
  const [stored, setStored] = useState(() => ({ key, id: read(key) }))
  const commandID = stored.key === key ? stored.id : read(key)

  const setCommandID = (id: string | null) => {
    try {
      if (id) sessionStorage.setItem(key, id)
      else sessionStorage.removeItem(key)
    } catch {
      // The current page can still show the command when storage is unavailable.
    }
    setStored({ key, id })
  }

  return [commandID, setCommandID] as const
}
