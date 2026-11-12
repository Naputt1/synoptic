import { Badge } from '@/components/ui/badge'
import { AppLink } from '@/components/ui/link'
import { useData, useSvc } from '@/lib/context'

function kindTone(k: string): 'default' | 'info' | 'warn' | 'accent' {
  if (k === 'bidi') return 'accent'
  if (k === 'server stream') return 'info'
  if (k === 'client stream') return 'warn'
  return 'default'
}

export function RPCView() {
  const data = useData()
  const svc = useSvc()
  const services = (data.services ?? []).map((s, si) => ({ s, si }))
  const shown = services.filter(({ s }) => !svc || s.svc === svc)
  if (services.length === 0) {
    return (
      <div className="text-faint p-6 text-[12px]">no gRPC services were found in this module</div>
    )
  }
  if (shown.length === 0) {
    return (
      <div className="text-faint p-6 text-[12px]">
        no gRPC services in {svc} — switch the service filter back to all
      </div>
    )
  }
  return (
    <div className="h-full overflow-auto p-4">
      <div className="mb-3 flex items-baseline gap-2">
        <span className="text-ink text-[13px]">RPC</span>
        <span className="text-faint text-[10.5px]">
          {shown.length} {shown.length === 1 ? 'service' : 'services'} ·{' '}
          {shown.reduce((n, { s }) => n + s.methods.length, 0)} methods
          {svc ? ` · ${svc}` : ''}
        </span>
      </div>
      <div className="grid gap-3 lg:grid-cols-2">
        {shown.map(({ s, si }) => (
          <section
            key={s.n}
            className="border-line bg-panel/30 overflow-hidden rounded-[6px] border"
          >
            <header className="border-line border-b px-3 py-2">
              <div className="flex items-baseline gap-2">
                <span className="text-ink truncate text-[12.5px]">{s.n}</span>
                {s.p && <span className="text-faint text-[10px]">{s.p}</span>}
                <span className="text-faint ml-auto shrink-0 text-[10px]">
                  {s.methods.length} methods
                </span>
              </div>
              {s.f && (
                <div className="text-faint mt-0.5 text-[10px]">
                  {s.f}
                  {s.l ? `:${s.l}` : ''}
                </div>
              )}
              {(s.mw ?? []).length > 0 && (
                <div className="mt-2 flex flex-wrap gap-1">
                  {s.mw!.map((mw) => (
                    <Badge key={mw.n} variant="plain">
                      {mw.n}
                    </Badge>
                  ))}
                </div>
              )}
            </header>
            <div className="divide-line/60 divide-y">
              {s.methods.map((m, mi) => (
                <AppLink
                  key={m.n}
                  href={`/rpc/${si}/${mi}`}
                  className="hover:bg-panel2 flex w-full flex-col gap-1 px-3 py-2 text-left"
                >
                  <div className="flex items-baseline gap-2">
                    <Badge variant={kindTone(m.k)}>{m.k}</Badge>
                    <span className="text-ink truncate text-[11.5px]">{m.n}</span>
                    <span className="text-faint ml-auto shrink-0 text-[9.5px]">{m.p}</span>
                  </div>
                  <div className="text-mut flex min-w-0 items-baseline gap-1.5 text-[10.5px]">
                    <span className="truncate">{m.rq || 'request'}</span>
                    <span className="text-faint">→</span>
                    <span className="truncate">{m.rs || 'response'}</span>
                  </div>
                  {m.d && <div className="text-mut line-clamp-2 text-[10.5px]">{m.d}</div>}
                </AppLink>
              ))}
            </div>
          </section>
        ))}
      </div>
    </div>
  )
}
