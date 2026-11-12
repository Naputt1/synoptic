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
  const blind = diag.skipped + diag.unresolved
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

  return (
    <div className="flex h-full flex-col overflow-auto">
      <ScopeCard
        title="diagnostics"
        badge="diag"
        meta={`${diag.parsed} files parsed · ${diag.skipped} skipped · ${diag.resolved}/${diag.calls} in-module calls resolved · ${diag.truncated ?? 0} flows truncated`}
        note={
          blind > 0
            ? 'what the analyzer could not read or resolve. anything declared in a skipped file is invisible by design.'
            : 'nothing was skipped and every in-module call resolved.'
        }
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
              {diag.resolved}/{diag.calls} resolved · {diag.unresolved} blind
            </span>
          </div>
          <div className="divide-line/60 divide-y">
            {unresolved.length === 0 && (
              <div className="text-faint px-3 py-2 text-[11px]">
                no in-module call was left unresolved
              </div>
            )}
            {unresolved.map((c, i) => (
              <div key={`${c.fn}:${c.e}:${i}`} className="px-3 py-1.5 text-[11px]">
                <div className="flex items-baseline gap-2">
                  <span className="text-ink min-w-0 flex-1 truncate">{c.fn}</span>
                  <code className="text-warn truncate text-[10.5px]" title={c.e}>
                    {c.e}
                  </code>
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
