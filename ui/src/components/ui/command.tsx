import { Dialog } from '@base-ui/react/dialog'
import { Search } from 'lucide-react'
import { useEffect, useId, useMemo, useRef, useState } from 'react'

import { routeHref } from '@/lib/data'
import { goto } from '@/lib/nav'
import type { Data, Route } from '@/lib/types'

import { AppLink } from './link'

type Via = 'path' | 'stage' | 'file' | 'stream' | 'doc'

type Hit = { r: Route; i: number; via: Via; score: number }

const VIA_RANK: Record<Via, number> = { path: 0, stage: 1, file: 2, stream: 3, doc: 4 }

function fields(r: Route) {
  const stream = r.st
    ? [
        r.pr ?? '',
        ...(r.st.events ?? []),
        ...(r.st.send ?? []).flatMap((m) => [
          m.n ?? '',
          m.t ?? '',
          m.e ?? '',
          ...(m.f ?? []).map((f) => f.n),
        ]),
        ...(r.st.recv ?? []).flatMap((m) => [
          m.n ?? '',
          m.t ?? '',
          m.e ?? '',
          ...(m.f ?? []).map((f) => f.n),
        ]),
      ]
        .join(' ')
        .toLowerCase()
    : ''
  return {
    path: (r.m + ' ' + r.p + ' ' + (r.pr ?? '')).toLowerCase(),
    stage: r.ch
      .map((s) => s.n)
      .join(' ')
      .toLowerCase(),
    file: r.f.toLowerCase(),
    stream,
    doc: r.ch
      .map((s) => s.d ?? '')
      .join(' ')
      .toLowerCase(),
  }
}

/** Every token must appear somewhere; the best field decides the label and the rank. */
function search(routes: Route[], q: string): Hit[] {
  const tokens = q.trim().toLowerCase().split(/\s+/).filter(Boolean)
  const hits: Hit[] = []
  routes.forEach((r, i) => {
    const f = fields(r)
    const all = f.path + ' ' + f.stage + ' ' + f.file + ' ' + f.stream + ' ' + f.doc
    if (!tokens.every((t) => all.includes(t))) return
    let via: Via = 'doc'
    for (const v of ['path', 'stage', 'file', 'stream', 'doc'] as Via[]) {
      if (tokens.every((t) => f[v].includes(t))) {
        via = v
        break
      }
    }
    // a path hit that starts a segment beats a hit buried mid-word
    const clean = tokens.join(' ')
    const bonus = f.path.startsWith(clean) ? -2 : f.path.includes('/' + clean) ? -1 : 0
    hits.push({ r, i, via, score: VIA_RANK[via] + bonus })
  })
  return hits.sort((a, b) => a.score - b.score || a.r.p.localeCompare(b.r.p))
}

function Highlight({ text, tokens }: { text: string; tokens: string[] }) {
  const t = tokens.find((x) => text.toLowerCase().includes(x))
  if (!t) return <>{text}</>
  const at = text.toLowerCase().indexOf(t)
  return (
    <>
      {text.slice(0, at)}
      <mark className="bg-acc/20 text-ink">{text.slice(at, at + t.length)}</mark>
      {text.slice(at + t.length)}
    </>
  )
}

export function SearchDialog({
  data,
  open,
  onOpenChange,
  svc = '',
}: {
  data: Data
  open: boolean
  onOpenChange: (v: boolean) => void
  /** limit hits to one service of a multi-entry module */
  svc?: string
}) {
  const [q, setQ] = useState('')
  const [active, setActive] = useState(0)
  const inputRef = useRef<HTMLInputElement>(null)
  const listId = useId()
  const tokens = useMemo(() => q.trim().toLowerCase().split(/\s+/).filter(Boolean), [q])
  const hits = useMemo(() => (q.trim() ? search(data.routes, q) : []), [q, data.routes])
  const all = useMemo(
    () => (svc ? hits.filter((h) => data.routes[h.i]?.svc === svc) : hits),
    [hits, svc, data.routes],
  )
  const pool = useMemo(
    () => (svc ? data.routes.filter((r) => r.svc === svc).length : data.routes.length),
    [svc, data.routes],
  )
  const results = all.slice(0, 14)
  const activeIndex = results.length ? Math.min(active, results.length - 1) : -1

  useEffect(() => {
    setActive(0)
  }, [q])

  useEffect(() => {
    if (!open) {
      setQ('')
      setActive(0)
    }
  }, [open])

  const pick = (ri: number) => {
    onOpenChange(false)
    goto(routeHref(ri))
  }

  const onKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActive((i) => Math.min(i + 1, results.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActive((i) => Math.max(i - 1, 0))
    } else if (e.key === 'Enter') {
      e.preventDefault()
      const hit = results[activeIndex]
      if (hit) pick(hit.i)
    }
  }

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Backdrop className="sheet-overlay fixed inset-0 z-50 bg-black/55 backdrop-blur-[1px]" />
        <Dialog.Popup
          data-slot="search-dialog"
          initialFocus={inputRef}
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) onOpenChange(false)
          }}
          className="fixed inset-0 z-50 flex items-start justify-center pt-[12vh] outline-none"
        >
          <Dialog.Title className="sr-only">Search routes</Dialog.Title>
          <Dialog.Description className="sr-only">
            Search method, protocol, path, stage, event, file or doc text.
          </Dialog.Description>
          <div className="sheet-content border-line bg-panel relative w-[min(680px,92vw)] overflow-hidden rounded-[8px] border shadow-[0_30px_80px_-30px_rgba(0,0,0,0.9)]">
            <div className="border-line text-mut flex items-center gap-2 border-b px-3 py-2">
              <Search className="size-3.5" />
              <input
                ref={inputRef}
                value={q}
                onChange={(e) => setQ(e.target.value)}
                onKeyDown={onKeyDown}
                placeholder="method, protocol, path, stage, event, file or doc text — every word must match"
                aria-label="Search routes"
                role="combobox"
                aria-expanded
                aria-autocomplete="list"
                aria-controls={listId}
                aria-activedescendant={activeIndex >= 0 ? `${listId}-${activeIndex}` : undefined}
                className="text-ink placeholder:text-faint w-full bg-transparent text-[12.5px] outline-none"
              />
              <kbd className="border-line rounded-[3px] border px-1 text-[10px]">esc</kbd>
            </div>
            <div
              id={listId}
              role="listbox"
              aria-label="Routes"
              className="max-h-[46vh] overflow-auto p-1.5"
            >
              {q.trim() && results.length === 0 && (
                <div className="text-faint px-2 py-6 text-center text-[12px]">
                  no route matches “{q.trim()}”
                </div>
              )}
              {!q.trim() && (
                <div className="text-faint px-2 py-2 text-[11px]">
                  type to search {pool} routes{svc ? ` in ${svc}` : ''} · ctrl+shift+E traces a log
                  error
                </div>
              )}
              {q.trim() && results.length > 0 && (
                <div className="text-faint px-2 pt-0.5 pb-1 text-[10px] tracking-[0.12em] uppercase">
                  {all.length} {all.length === 1 ? 'match' : 'matches'}
                  {all.length > results.length ? ` — showing ${results.length}` : ''}
                </div>
              )}
              {results.map(({ r, i, via }, idx) => (
                <AppLink
                  key={i}
                  href={routeHref(i)}
                  id={`${listId}-${idx}`}
                  role="option"
                  aria-selected={idx === activeIndex}
                  onMouseEnter={() => setActive(idx)}
                  onMouseDown={(e) => {
                    if (e.button === 0) e.preventDefault()
                  }}
                  onClick={() => onOpenChange(false)}
                  className={`text-ink flex cursor-pointer items-center gap-2 rounded-[4px] px-2 py-1.5 text-[12px] ${
                    idx === activeIndex ? 'bg-acc/10' : ''
                  }`}
                >
                  <span className={`w-12 shrink-0 text-[10px] ${r.pr ? 'text-acc' : 'text-info'}`}>
                    {r.pr ? r.pr.toUpperCase() : r.m}
                  </span>
                  <span className="min-w-0 flex-1 truncate font-mono">
                    <Highlight text={r.p} tokens={tokens} />
                  </span>
                  <span className="text-faint shrink-0 text-[10px]">{r.ch.at(-1)?.n}</span>
                  <span
                    className={`w-9 shrink-0 text-right text-[9px] tracking-[0.1em] uppercase ${
                      via === 'path' || via === 'stream'
                        ? 'text-acc'
                        : via === 'stage'
                          ? 'text-info'
                          : 'text-faint'
                    }`}
                  >
                    {via}
                  </span>
                </AppLink>
              ))}
            </div>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
