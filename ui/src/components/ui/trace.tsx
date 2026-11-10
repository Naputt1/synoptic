import { Dialog } from '@base-ui/react/dialog'
import { ClipboardPaste, X } from 'lucide-react'
import { useEffect, useId, useMemo, useRef, useState } from 'react'

import { errHref } from '@/lib/data'
import { matchErrors, type ErrMatch } from '@/lib/errors'
import { goto } from '@/lib/nav'
import { statusBadgeTone, statusTone } from '@/lib/status'
import type { Data } from '@/lib/types'

import { AppLink } from './link'

/**
 * Trace a message from a real log back to the line that writes it. Kept apart
 * from route search so a paste never has to compete with a query.
 */
export function TraceDialog({
  data,
  open,
  onOpenChange,
  svc = '',
}: {
  data: Data
  open: boolean
  onOpenChange: (v: boolean) => void
  /** prefer sites reached by one service of a multi-entry module */
  svc?: string
}) {
  const [paste, setPaste] = useState('')
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLTextAreaElement>(null)
  const listId = useId()
  const matches = useMemo(
    () => matchErrors(data.errs, paste, { routes: data.routes, svc }),
    [data.errs, paste, data.routes, svc],
  )
  const activeIndex = matches.length ? Math.min(active, matches.length - 1) : -1

  useEffect(() => {
    setActive(0)
  }, [paste])

  useEffect(() => {
    if (!open) {
      setPaste('')
      setActive(0)
    }
  }, [open])

  useEffect(() => {
    if (activeIndex < 0) return
    document.getElementById(`${listId}-${activeIndex}`)?.scrollIntoView({ block: 'nearest' })
  }, [activeIndex, listId])

  const traceHref = (m: ErrMatch): string | undefined => {
    const ref = m.site.r?.[0]
    if (!ref || ref.k === 'rpc' || ref.si !== undefined) return errHref(m.site, ref)
    const route = data.routes[ref.ri]
    const stage = route?.ch.findIndex((s) => s.fk === m.site.fn) ?? -1
    return errHref(m.site, stage >= 0 ? { ...ref, si: stage } : ref)
  }

  const openMatch = (m?: ErrMatch) => {
    const href = m ? traceHref(m) : undefined
    if (!href) return
    onOpenChange(false)
    goto(href)
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'ArrowDown' && matches.length) {
      e.preventDefault()
      setActive((i) => Math.min(i + 1, matches.length - 1))
    } else if (e.key === 'ArrowUp' && matches.length) {
      e.preventDefault()
      setActive((i) => Math.max(i - 1, 0))
    } else if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      openMatch(matches[activeIndex] ?? matches[0])
    }
  }

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="sheet-overlay fixed inset-0 z-50 bg-black/55 backdrop-blur-[1px]" />
        <Dialog.Popup
          data-slot="trace-dialog"
          initialFocus={inputRef}
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) onOpenChange(false)
          }}
          className="fixed inset-0 z-50 flex items-start justify-center pt-[12vh] outline-none"
        >
          <Dialog.Title className="sr-only">Trace a runtime error</Dialog.Title>
          <Dialog.Description className="sr-only">
            Paste a runtime error, log line, panic or stack trace to find the source line that
            writes the message.
          </Dialog.Description>
          <div className="sheet-content border-line bg-panel relative w-[min(720px,92vw)] overflow-hidden rounded-[8px] border shadow-[0_30px_80px_-30px_rgba(0,0,0,0.9)]">
            <div className="border-line bg-panel2/40 flex items-start gap-2 border-b px-3 py-2">
              <ClipboardPaste className="text-faint mt-1 size-3.5 shrink-0" />
              <textarea
                ref={inputRef}
                value={paste}
                onChange={(e) => setPaste(e.target.value)}
                onKeyDown={onKeyDown}
                rows={paste.includes('\n') ? 4 : 2}
                placeholder="paste a runtime error, log line, panic or stack trace"
                aria-label="Runtime error text"
                aria-controls={listId}
                className="text-ink placeholder:text-faint max-h-40 min-h-[36px] w-full resize-none bg-transparent font-mono text-[11.5px] leading-[1.55] outline-none"
              />
              {paste && (
                <button
                  type="button"
                  aria-label="Clear pasted error"
                  onClick={() => {
                    setPaste('')
                    inputRef.current?.focus()
                  }}
                  className="text-faint hover:text-ink mt-0.5 shrink-0"
                >
                  <X className="size-3.5" />
                </button>
              )}
              <kbd className="border-line text-faint mt-0.5 shrink-0 rounded-[3px] border px-1 text-[10px]">
                esc
              </kbd>
            </div>
            <div className="text-faint flex items-center justify-between px-3 pt-2 text-[10px] tracking-[0.12em] uppercase">
              <span>
                {paste.trim()
                  ? `${matches.length} ${matches.length === 1 ? 'runtime message' : 'runtime messages'} matched`
                  : 'runtime messages'}
              </span>
              {matches.length > 0 && (
                <span className="text-faint/80 tracking-normal normal-case">
                  enter opens the first · ↑↓ picks
                </span>
              )}
            </div>
            <div
              id={listId}
              role="listbox"
              aria-label="Runtime messages"
              className="max-h-[46vh] overflow-auto p-1.5"
            >
              {!paste.trim() && (
                <div className="text-faint px-2 py-6 text-center text-[12px]">
                  paste what the log printed
                  <div className="text-faint/80 mt-1 text-[10.5px]">
                    matches are literal messages written in source; a stack frame or route path
                    helps pick the right site
                  </div>
                </div>
              )}
              {paste.trim() && matches.length === 0 && (
                <div className="text-faint px-2 py-6 text-center text-[12px]">
                  no literal message in this repo matches that text
                  <div className="text-faint/80 mt-1 text-[10.5px]">
                    only messages written in source are indexed; try the message alone
                  </div>
                </div>
              )}
              {matches.map((m, idx) => {
                const site = m.site
                const ref = site.r?.[0]
                const route = ref && ref.k !== 'rpc' ? data.routes[ref.ri] : undefined
                const where =
                  ref?.k === 'rpc'
                    ? site.fn.split('|').pop()
                    : route
                      ? `${route.m} ${route.p}`
                      : site.fn.split('|').pop()
                const href = traceHref(m)
                return (
                  <AppLink
                    key={m.msg}
                    href={href ?? '#'}
                    id={`${listId}-${idx}`}
                    role="option"
                    aria-selected={idx === activeIndex}
                    onMouseEnter={() => setActive(idx)}
                    onMouseDown={(e) => {
                      if (e.button === 0) e.preventDefault()
                    }}
                    onClick={(e) => {
                      if (!href) e.preventDefault()
                      else onOpenChange(false)
                    }}
                    className={`text-ink flex cursor-pointer items-start gap-2 rounded-[4px] px-2 py-1.5 ${
                      idx === activeIndex ? 'bg-acc/10' : 'hover:bg-acc/10'
                    }`}
                  >
                    <span
                      className={`mt-0.5 w-12 shrink-0 rounded-[3px] border px-1 text-center text-[9px] leading-[14px] tabular-nums ${
                        m.codes.length
                          ? statusBadgeTone[statusTone(m.codes[0])]
                          : 'border-line2 text-mut uppercase'
                      }`}
                    >
                      {m.codes.length ? m.codes.join(' ') : m.kind}
                    </span>
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-mono text-[11.5px]">{m.msg}</span>
                      <span className="text-faint block truncate text-[9.5px]">
                        {where} · {site.f}:{site.l}
                        {m.sites.length > 1 ? ` · +${m.sites.length - 1} sites` : ''}
                      </span>
                    </span>
                    <span className="text-faint mt-0.5 shrink-0 text-[9px] tracking-[0.08em] uppercase">
                      {m.why}
                    </span>
                  </AppLink>
                )
              })}
            </div>
            <div className="border-line text-faint border-t px-3 py-1.5 text-[10px]">
              ctrl J opens this from anywhere · ⌘K searches routes
            </div>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
