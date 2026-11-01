import { useState } from 'react'
import type { Route } from '../lib/types'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'

function Cell({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 border-t border-line/60 px-3 py-2 first:border-t-0">
      <div className="text-[9.5px] uppercase tracking-[0.14em] text-faint">{label}</div>
      <div className="mt-1 text-[11.5px] text-ink">{children}</div>
    </div>
  )
}

function Rows({ items, empty }: { items: { n: string; t?: string; rq?: boolean }[]; empty: string }) {
  if (!items.length) return <span className="text-faint">{empty}</span>
  return (
    <div className="space-y-0.5">
      {items.map((f) => (
        <div key={f.n} className="flex items-baseline gap-2">
          <code className="text-ink">{f.n}</code>
          {f.t && <span className="text-[10.5px] text-mut">{f.t}</span>}
          {f.rq && (
            <span className="rounded-[3px] border border-warn/40 px-1 text-[9px] uppercase tracking-[0.1em] text-warn">
              required
            </span>
          )}
        </div>
      ))}
    </div>
  )
}

export function DocPanel({ r }: { r: Route }) {
  const d = r.doc
  const [copied, setCopied] = useState(false)
  if (!d) return null

  const copy = () => {
    void navigator.clipboard?.writeText(d.curl ?? '')
    setCopied(true)
    window.setTimeout(() => setCopied(false), 1400)
  }

  return (
    <div className="border-t border-line bg-panel/40">
      <div className="flex items-center gap-2 px-4 py-2">
        <span className="text-[9.5px] uppercase tracking-[0.14em] text-faint">documentation</span>
        {d.auth && (
          <Badge variant="warn" title={d.aw ? `guarded by ${d.aw}` : undefined}>
            auth required
          </Badge>
        )}
        {d.owner && (
          <span className="ml-auto text-[10px] text-faint">
            {d.owner} · {d.commit} · {d.age}
          </span>
        )}
      </div>

      <div className="grid grid-cols-1 gap-px bg-line/40 sm:grid-cols-2 xl:grid-cols-4">
        <Cell label="path params">
          <Rows items={d.path ?? []} empty="none — the path is fixed" />
        </Cell>
        <Cell label="query">
          <Rows items={d.query ?? []} empty="no query keys read" />
        </Cell>
        <Cell label="body">
          {d.body?.length ? (
            <Rows items={d.body} empty="" />
          ) : (
            <span className="text-faint">
              {d.bt ? `type ${d.bt} — fields not expanded` : 'no body read'}
            </span>
          )}
        </Cell>
        <Cell label="responses">
          {d.resp?.length ? (
            <div className="flex flex-wrap gap-1.5">
              {d.resp.map((x) => (
                <span
                  key={x.c + (x.k ?? []).join('')}
                  className={[
                    'rounded-[3px] border px-1.5 py-px font-mono text-[10.5px]',
                    x.c.startsWith('2')
                      ? 'border-acc/40 text-acc'
                      : x.c.startsWith('4')
                        ? 'border-warn/45 text-warn'
                        : x.c.startsWith('5')
                          ? 'border-bad/45 text-bad'
                          : 'border-line text-mut',
                  ].join(' ')}
                >
                  {x.c}
                  {x.k?.length ? <span className="text-faint"> · {x.k.join(', ')}</span> : null}
                </span>
              ))}
            </div>
          ) : (
            <span className="text-faint">no literal response read</span>
          )}
        </Cell>
      </div>

      <div className="grid grid-cols-1 gap-px border-t border-line/60 bg-line/40 lg:grid-cols-[1fr_1.4fr]">
        <Cell label="data touched">
          {d.tables?.length ? (
            <div className="flex flex-wrap gap-1.5">
              {d.tables.map((t) => (
                <span key={t} className="rounded-[3px] border border-warn/35 px-1.5 py-px font-mono text-[10.5px] text-warn">
                  {t}
                </span>
              ))}
            </div>
          ) : (
            <span className="text-faint">no literal table or model read</span>
          )}
        </Cell>
        <Cell label="example">
          <div className="relative">
            <pre className="overflow-x-auto rounded-[4px] bg-canvas/70 p-2 text-[10.5px] leading-relaxed text-mut">
              {d.curl}
            </pre>
            <Button variant="ghost" size="sm" className="absolute right-1 top-1 h-5 px-1.5 text-[10px]" onClick={copy}>
              {copied ? 'copied' : 'copy'}
            </Button>
          </div>
        </Cell>
      </div>
    </div>
  )
}
