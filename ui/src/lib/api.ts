import type { Data, Refs } from './types'

async function errorFrom(res: Response): Promise<Error> {
  let body = ''
  try {
    body = (await res.text()).trim()
  } catch {
    // the status line below still says what happened
  }
  return new Error(body || `request failed (${res.status} ${res.statusText})`)
}

/** Null means this page is static, or the API is not reachable. */
export async function fetchRefs(): Promise<Refs | null> {
  try {
    const res = await fetch('/api/refs')
    if (!res.ok) return null
    return (await res.json()) as Refs
  } catch {
    return null
  }
}

async function post(url: string, body: unknown): Promise<Data> {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw await errorFrom(res)
  return (await res.json()) as Data
}

export function compareRefs(base: string, to: string): Promise<Data> {
  return post('/api/compare', { base, to })
}

export function comparePR(pr: string): Promise<Data> {
  return post('/api/pr', { pr })
}
