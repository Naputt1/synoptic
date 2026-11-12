import { ScopeCard } from '@/components/ScopeCard'
import { Badge } from '@/components/ui/badge'
import { AppLink } from '@/components/ui/link'
import { useData } from '@/lib/context'
import { fnHref } from '@/lib/data'

const reasonTone: Record<string, 'warn' | 'info' | 'default' | 'bad'> = {
  read: 'warn',
  parse: 'bad',
  generated: 'info',
  build: 'default',
}

export function DiagnosticsView() {
  const data = useData()
  const diag = data.diag
  const skipped = (diag.skip ?? []).slice(0, 20)
  const unresolved = (diag.unres ?? []).slice(0, 20)
  const truncated = Object.entries(data.flows)
    .filter(([, f]) => f.trunc)
    .slice(0, 20)
  const reasons = [
    { k: 'read', n: diag.read ?? 0 },
    { k: 'parse', n: diag.parse ?? 0 },
    { k: 'generated', n: diag.generated ?? 0 },
    { k: 'build', n: diag.build ?? 0 },
  ].filter((r) => r.n > 0)
  const honestBase =
    diag.resolved + diag.unresolved + (diag.dyn ?? 0) + (diag.amb ?? 0) + (diag.unk ?? 0)
  const honestPct = honestBase > 0 ? (diag.resolved / honestBase) * 100 : 100
  const callReasons = (
    [
      { k: 'resolved', n: diag.resolved, tone: 'info', hint: 'target is an in-module function' },
      {
        k: 'blind',
        n: diag.unresolved,
        tone: 'bad',
        hint: 'in-module target the index should hold but does not',
      },
      {
        k: 'dynamic',
        n: diag.dyn ?? 0,
        tone: 'warn',
        hint: 'call through a function value, so there is no static target',
      },
      {
        k: 'ambiguous',
        n: diag.amb ?? 0,
        tone: 'warn',
        hint: 'interface value with more than one indexed implementation',
      },
      {
        k: 'unknown',
        n: diag.unk ?? 0,
        tone: 'default',
        hint: 'receiver type not provable (for example tx := db.Begin(); tx.Exec())',
      },
      {
        k: 'external',
        n: diag.ext ?? 0,
        tone: 'default',
        hint: 'target package is outside this module',
      },
      { k: 'convert', n: diag.conv ?? 0, tone: 'default', hint: 'type conversion, not a call' },
      {
        k: 'builtin',
        n: diag.why?.builtin ?? 0,
        tone: 'default',
        hint: 'language builtin such as len',
      },
    ] as const
  ).filter((r) => r.n > 0)

  return (
    <div className="flex h-full flex-col overflow-auto">
      <ScopeCard
        title="diagnostics"
        badge="diag"
        meta={`${diag.parsed} files parsed · ${diag.skipped} skipped · ${diag.resolved}/${diag.calls} calls resolved · ${honestPct.toFixed(1)}% honest · ${diag.truncated ?? 0} flows truncated`}
        note={`${diag.unresolved} blind ${diag.unresolved === 1 ? 'call' : 'calls'} — in-module targets the index should hold. External calls, conversions and builtins are not blind spots; unknown means the receiver type is not provable, so no target can be named.`}
      />
      <div className="grid gap-3 p-4 lg:grid-cols-2">
        <section className="border-line bg-panel/40 rounded-[6px] border">
          <div className="border-line text-mut flex items-baseline justify-between border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
            <span>files</span>
            <span className="text-faint tracking-normal normal-case">
              {diag.parsed} parsed · {diag.skipped} skipped
            </span>
          </div>
          <div className="border-line/60 flex flex-wrap gap-1.5 border-b px-3 py-2">
            {reasons.length === 0 && (
              <span className="text-faint text-[11px]">no file was skipped</span>
            )}
            {reasons.map((r) => (
              <span key={r.k} className="flex items-center gap-1">
                <Badge variant={reasonTone[r.k] ?? 'default'}>{r.k}</Badge>
                <b className="text-ink text-[11px] font-semibold">{r.n}</b>
              </span>
            ))}
          </div>
          <div className="divide-line/60 divide-y">
            {skipped.length === 0 && (
              <div className="text-faint px-3 py-2 text-[11px]">no skipped files</div>
            )}
            {skipped.map((f) => (
              <div key={f.p} className="flex items-baseline gap-2 px-3 py-1.5 text-[11px]">
                <code className="text-ink min-w-0 flex-1 truncate" title={f.p}>
                  {f.p}
                </code>
                <Badge variant={reasonTone[f.r] ?? 'default'}>{f.r}</Badge>
              </div>
            ))}
          </div>
          {diag.skipped > skipped.length && (
            <div className="text-faint border-line/60 border-t px-3 py-1.5 text-[10px]">
              showing {skipped.length} of {diag.skipped}
            </div>
          )}
        </section>

        <section className="border-line bg-panel/40 rounded-[6px] border">
          <div className="border-line text-mut flex items-baseline justify-between border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
            <span>calls</span>
            <span className="text-faint tracking-normal normal-case">
              {diag.resolved}/{diag.calls} resolved · {honestPct.toFixed(1)}% honest
            </span>
          </div>
          <div className="border-line/60 flex flex-wrap gap-1.5 border-b px-3 py-2">
            {callReasons.map((r) => (
              <span key={r.k} className="flex items-center gap-1" title={r.hint}>
                <Badge variant={r.tone}>{r.k}</Badge>
                <b className="text-ink text-[11px] font-semibold">{r.n}</b>
              </span>
            ))}
          </div>
          <div className="text-faint border-line/60 border-b px-3 py-1.5 text-[10px] leading-relaxed">
            honest = resolved / (resolved + blind + dynamic + ambiguous + unknown). unknown means
            the receiver type is not provable (for example <code>tx := db.Begin(); tx.Exec()</code>
            ), not a blind in-module gap. External calls, conversions and builtins are excluded.
          </div>
          <div className="divide-line/60 divide-y">
            {unresolved.length === 0 && (
              <div className="text-faint px-3 py-2 text-[11px]">
                no blind call: every in-module call the index should hold was found
              </div>
            )}
            {unresolved.map((c, i) => (
              <div key={`${c.fn}:${c.e}:${i}`} className="px-3 py-1.5 text-[11px]">
                <div className="flex items-baseline gap-2">
                  <span className="text-ink min-w-0 flex-1 truncate">{c.fn}</span>
                  <code className="text-warn truncate text-[10.5px]" title={c.e}>
                    {c.e}
                  </code>
                  {c.r && <Badge variant="bad">{c.r}</Badge>}
                </div>
                {c.f && (
                  <div className="text-faint mt-0.5 truncate text-[10px]">
                    {c.f}:{c.l ?? 0}
                  </div>
                )}
              </div>
            ))}
          </div>
        </section>

        <section className="border-line bg-panel/40 rounded-[6px] border lg:col-span-2">
          <div className="border-line text-mut flex items-baseline justify-between border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
            <span>flows</span>
            <span className="text-faint tracking-normal normal-case">
              {diag.truncated ?? truncated.length} truncated at depth/limit
            </span>
          </div>
          <div className="divide-line/60 divide-y">
            {truncated.length === 0 && (
              <div className="text-faint px-3 py-2 text-[11px]">
                every baked flow reaches its end
              </div>
            )}
            {truncated.map(([key, f]) => (
              <AppLink
                key={key}
                href={fnHref(key)}
                className="hover:bg-panel2 flex items-baseline gap-2 px-3 py-1.5 text-[11px]"
              >
                <span className="text-ink min-w-0 flex-1 truncate">{f.n}</span>
                {f.f && (
                  <span className="text-faint shrink-0 text-[10px]">
                    {f.f}:{f.l ?? 0}
                  </span>
                )}
                <Badge variant="warn">truncated</Badge>
                <span className="text-faint shrink-0 text-[9px]">open ›</span>
              </AppLink>
            ))}
          </div>
          {diag.truncated !== undefined && diag.truncated > truncated.length && (
            <div className="text-faint border-line/60 border-t px-3 py-1.5 text-[10px]">
              showing {truncated.length} of {diag.truncated}; the rest were skipped at the 1400-flow
              cap
            </div>
          )}
        </section>
      </div>
    </div>
  )
}
