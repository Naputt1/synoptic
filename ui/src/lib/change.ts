import type { Change } from './types'

const sha = /^[0-9a-f]{7,40}$/i

/** Shorten a commit id, but leave branch and tag names alone. */
export function refLabel(ref?: string): string {
  if (!ref) return ''
  return sha.test(ref) ? ref.slice(0, 7) : ref
}

export function changeTitle(c: Change): string {
  if (c.mode === 'pr' && c.pr) return `PR #${c.pr}`
  if (c.to) return `${c.ref}..${refLabel(c.to)}`
  return `vs ${c.ref}`
}

export function changeCounts(c: Change) {
  return {
    added: c.added?.length ?? 0,
    removed: c.removed?.length ?? 0,
    modified: c.modified?.length ?? 0,
    commits: (c.commits?.length ?? 0) + (c.commitsMore ?? 0),
    files: c.files,
  }
}

/** The head side in a form that reads well next to an arrow. */
export function headLabel(c: Change): string {
  if (c.to) return refLabel(c.to)
  return 'working tree'
}

export function changeMode(c: Change): string {
  if (c.mode === 'pr') return `PR #${c.pr ?? ''}`
  if (c.mode === 'range') return 'commit range'
  return 'working tree'
}
