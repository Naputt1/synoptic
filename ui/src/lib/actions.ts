export type ToastTone = 'ok' | 'bad'

export function toast(message: string, tone: ToastTone = 'ok') {
  window.dispatchEvent(new CustomEvent('synoptic:toast', { detail: { message, tone } }))
}

function legacyCopy(text: string): boolean {
  try {
    const area = document.createElement('textarea')
    area.value = text
    area.setAttribute('readonly', '')
    area.style.position = 'fixed'
    area.style.top = '-1000px'
    area.style.opacity = '0'
    document.body.appendChild(area)
    area.select()
    const ok = document.execCommand('copy')
    area.remove()
    return ok
  } catch {
    return false
  }
}

/**
 * The page is usually opened from disk, where navigator.clipboard is not
 * available. Fall back to the selection API so copy actions still work.
 */
export async function copyText(text: string): Promise<boolean> {
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // fall through to the textarea path
    }
  }
  return legacyCopy(text)
}

export async function copyValue(text: string, label: string) {
  const ok = await copyText(text)
  toast(ok ? `${label} copied` : `could not copy ${label}`, ok ? 'ok' : 'bad')
}

export function absoluteLink(href: string): string {
  const base = window.location.href.split('#')[0]
  return `${base}#${href.startsWith('/') ? href : `/${href}`}`
}

export function showSource(line: number, lines?: number[], startLine?: number) {
  window.dispatchEvent(
    new CustomEvent('synoptic:source', { detail: { line, lines, startLine } }),
  )
}

export function openSearch() {
  window.dispatchEvent(new CustomEvent('synoptic:search'))
}
