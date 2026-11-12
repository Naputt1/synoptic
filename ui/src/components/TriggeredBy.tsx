import { Badge } from '@/components/ui/badge'
import { AppLink } from '@/components/ui/link'
import { routeHref } from '@/lib/data'
import type { StreamEvent } from '@/lib/types'

export function TriggeredBy({
  event,
  isSse,
  onFn,
}: {
  event?: StreamEvent
  isSse: boolean
  onFn?: (key: string) => string
}) {
  const origins = event?.or ?? []
  if (!origins.length) {
    return isSse ? (
      <div className="mt-4">
        <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">
          triggered by
        </div>
        <p className="text-faint text-[10.5px] italic">
          no in-repo publisher resolved for this event.
        </p>
      </div>
    ) : null
  }
  return (
    <div className="mt-4">
      <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">triggered by</div>
      <div className="divide-line/40 border-line/40 divide-y border-y">
        {origins.map((o, i) => (
          <div
            key={`${o.n}:${o.f ?? ''}:${o.l ?? 0}:${i}`}
            className="flex items-start gap-2 py-1.5"
          >
            <Badge variant={o.k === 'route' ? 'info' : 'plain'}>{o.k ?? 'background'}</Badge>
            <div className="min-w-0 flex-1">
              {o.k === 'route' && o.p ? (
                <AppLink
                  href={routeHref(o.ri ?? 0)}
                  className="text-acc text-left font-mono text-[10.5px] break-words hover:underline"
                >
                  {o.m} {o.p}
                </AppLink>
              ) : o.fk && onFn ? (
                <AppLink
                  href={onFn(o.fk)}
                  className="text-acc text-left font-mono text-[10.5px] break-words hover:underline"
                >
                  {o.n}
                </AppLink>
              ) : (
                <div className="text-ink font-mono text-[10.5px] break-words">{o.n}</div>
              )}
              {o.k === 'route' && o.fk && onFn ? (
                <AppLink
                  href={onFn(o.fk)}
                  className="text-acc mt-0.5 font-mono text-[9px] hover:underline"
                >
                  method ›
                </AppLink>
              ) : null}
              {o.via ? <div className="text-faint text-[9px]">via {o.via}</div> : null}
              {o.f ? (
                <div className="text-faint font-mono text-[9px] break-all">
                  {o.f}
                  {o.l ? `:${o.l}` : ''}
                  {o.c && o.c > 1 ? ` · ${o.c} calls` : ''}
                </div>
              ) : null}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
