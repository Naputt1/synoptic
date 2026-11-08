import { Command as Cmdk } from 'cmdk'
import { Search } from 'lucide-react'
import { useMemo, useState } from 'react'

import { routeHref } from '@/lib/data'
import { goto } from '@/lib/nav'
import type { Data, Route } from '@/lib/types'

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
}: {
  data: Data
  open: boolean
  onOpenChange: (v: boolean) => void
}) {
  const [q, setQ] = useState('')
  const tokens = useMemo(() => q.trim().toLowerCase().split(/\s+/).filter(Boolean), [q])
  const all = useMemo(() => (q.trim() ? search(data.routes, q) : []), [q, data.routes])
  const results = all.slice(0, 14)

  const pick = (ri: number) => {
    onOpenChange(false)
    setQ('')
    goto(routeHref(ri))
  }

  if (!open) return null

  return (
    <div
      data-slot="search-dialog"
      className="fixed inset-0 z-50 flex items-start justify-center pt-[12vh]"
      onMouseDown={() => onOpenChange(false)}
    >
      <div className="absolute inset-0 bg-black/55 backdrop-blur-[1px]" />
      <Cmdk
        shouldFilter={false}
        loop
        onMouseDown={(e) => e.stopPropagation()}
        className="sheet-content border-line bg-panel relative z-10 w-[min(680px,92vw)] overflow-hidden rounded-[8px] border shadow-[0_30px_80px_-30px_rgba(0,0,0,0.9)]"
      >
        <div className="border-line text-mut flex items-center gap-2 border-b px-3 py-2">
          <Search className="size-3.5" />
          <Cmdk.Input
            autoFocus
            value={q}
            onValueChange={setQ}
            placeholder="method, protocol, path, stage, event, file or doc text — every word must match"
            className="text-ink placeholder:text-faint w-full bg-transparent text-[12.5px] outline-none"
          />
          <kbd className="border-line rounded-[3px] border px-1 text-[10px]">esc</kbd>
        </div>
        <Cmdk.List className="max-h-[46vh] overflow-auto p-1.5">
          {q.trim() && results.length === 0 && (
            <div className="text-faint px-2 py-6 text-center text-[12px]">
              no route matches “{q.trim()}”
            </div>
          )}
          {!q.trim() && (
            <div className="text-faint px-2 py-2 text-[11px]">
              type to search {data.routes.length} routes
            </div>
          )}
          {q.trim() && results.length > 0 && (
            <div className="text-faint px-2 pt-0.5 pb-1 text-[10px] tracking-[0.12em] uppercase">
              {all.length} {all.length === 1 ? 'match' : 'matches'}
              {all.length > results.length ? ` — showing ${results.length}` : ''}
            </div>
          )}
          {results.map(({ r, i, via }) => (
            <Cmdk.Item
              key={i}
              value={String(i)}
              onSelect={() => pick(i)}
              className="text-ink data-[selected=true]:bg-acc/10 flex cursor-pointer items-center gap-2 rounded-[4px] px-2 py-1.5 text-[12px]"
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
            </Cmdk.Item>
          ))}
        </Cmdk.List>
      </Cmdk>
    </div>
  )
}
