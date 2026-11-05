import { useState } from 'react'
import type { Input, Route } from '../lib/types'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { statusBadgeTone, statusTone } from '../lib/status'

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

const kindTone: Record<Input['k'], string> = {
  path: 'border-line text-mut',
  query: 'border-info/40 text-info',
  ctx: 'border-acc/40 text-acc',
  header: 'border-warn/45 text-warn',
  cookie: 'border-warn/45 text-warn',
  form: 'border-line2 text-mut',
}

const accessTone: Record<Input['a'], string> = {
  read: 'border-acc/40 text-acc',
  set: 'border-info/40 text-info',
  'read+set': 'border-acc/40 text-acc',
  unused: 'border-line text-faint',
  undeclared: 'border-bad/45 text-bad',
  dynamic: 'border-line2 text-mut',
  bound: 'border-line text-mut',
}

export function Inputs({ items, flush }: { items: Input[]; flush?: boolean }) {
  const warnings = items.filter((x) => x.a === 'unused' || x.a === 'undeclared').length
  return (
    <div className={flush ? '' : 'border-t border-line/60'}>
      <div className="flex items-baseline gap-2 px-3 py-2">
        <span className="text-[9.5px] uppercase tracking-[0.14em] text-faint">inputs</span>
        <span className="text-[10px] text-faint">
          {items.length} detected
          {warnings > 0 && <span className="text-warn"> · {warnings} warning{warnings > 1 ? 's' : ''}</span>}
        </span>
      </div>
      {items.length ? (
        <div className="divide-y divide-line/40 border-t border-line/40">
          {items.map((x) => (
            <div
              key={`${x.k}:${x.n}:${x.e ?? ''}`}
              className="grid grid-cols-[48px_minmax(0,1fr)_auto] items-baseline gap-2 px-3 py-1.5"
            >
              <span
                className={[
                  'w-fit rounded-[3px] border px-1 text-[9px] uppercase tracking-[0.08em]',
                  kindTone[x.k],
                ].join(' ')}
              >
                {x.k}
              </span>
              <div className="min-w-0">
                <div className="flex flex-wrap items-baseline gap-x-2">
                  <code className="text-[11px] text-ink">{x.n}</code>
                  {x.t && <span className="text-[10px] text-mut">{x.t}</span>}
                  {x.rq && (
                    <span className="text-[9px] uppercase tracking-[0.1em] text-warn">required</span>
                  )}
                </div>
                <div className="mt-0.5 flex flex-wrap gap-x-2 text-[10px] text-faint">
                  {x.sb && (
                    <span>
                      set by {x.sb}
                      {x.si ? ` → ${x.si}` : ''}
                      {x.sa ? ` · ${x.sa}` : ''}
                    </span>
                  )}
                  {x.rb && (
                    <span>
                      read by {x.rb}
                      {x.ri ? ` → ${x.ri}` : ''}
                      {x.ra ? ` · ${x.ra}` : ''}
                    </span>
                  )}
                  {x.a === 'unused' && <span>never read</span>}
                  {x.a === 'undeclared' && <span>not in the route path</span>}
                </div>
              </div>
              <span
                className={[
                  'rounded-[3px] border px-1 text-[9px] uppercase tracking-[0.08em]',
                  accessTone[x.a],
                ].join(' ')}
              >
                {x.a}
              </span>
            </div>
          ))}
        </div>
      ) : (
        <div className="border-t border-line/40 px-3 py-2 text-[11px] text-faint">no inputs read</div>
      )}
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

      <Inputs items={d.in ?? []} />

      <div className="grid grid-cols-1 gap-px border-t border-line/60 bg-line/40 sm:grid-cols-2">
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
                    statusBadgeTone[statusTone(x.c)],
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
