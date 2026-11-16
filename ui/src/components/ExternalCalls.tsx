import { Badge } from '@/components/ui/badge'
import { AppLink } from '@/components/ui/link'
import { tagLabel } from '@/lib/data'

type ExtCall = {
  g: string
  n: string
  fk?: string
  f?: string
  l?: number
  ev?: string
  op?: string
  tb?: string
}

export function ExternalCalls({
  items,
  onFn,
}: {
  items: ExtCall[]
  onFn?: (key: string) => string
}) {
  const groups = ['db', 'cache', 'net']
    .map((g) => ({ g, items: items.filter((x) => x.g === g) }))
    .filter((x) => x.items.length > 0)
  return (
    <div className="space-y-2.5">
      {groups.map((group) => (
        <div key={group.g}>
          <div className="mb-1 flex items-center gap-2">
            <Badge variant={group.g === 'db' ? 'warn' : group.g === 'cache' ? 'accent' : 'info'}>
              {tagLabel[group.g] ?? group.g}
            </Badge>
            <span className="text-faint text-[10px]">{group.items.length}</span>
          </div>
          <div className="divide-line/40 border-line/40 divide-y border-y">
            {group.items.map((x, i) => (
              <div key={`${x.g}:${x.n}:${x.f ?? ''}:${x.l ?? 0}:${i}`} className="py-1.5">
                <div className="flex flex-wrap items-baseline gap-1.5">
                  {x.fk && onFn ? (
                    <AppLink
                      href={onFn(x.fk)}
                      className="text-acc text-left font-mono text-[10.5px] leading-relaxed break-words hover:underline"
                    >
                      {x.n}
                    </AppLink>
                  ) : (
                    <code className="text-ink font-mono text-[10.5px] leading-relaxed break-words">
                      {x.n}
                    </code>
                  )}
                  {x.op ? (
                    <span className="border-warn/35 text-warn rounded-[3px] border px-1 font-mono text-[9px] tracking-[0.08em]">
                      {x.op.toUpperCase()}
                    </span>
                  ) : null}
                  {x.tb ? (
                    <span className="border-warn/35 text-warn rounded-[3px] border px-1 font-mono text-[9.5px]">
                      {x.tb}
                    </span>
                  ) : null}
                </div>
                {x.f && (
                  <div className="text-faint mt-0.5 font-mono text-[9px] break-all">
                    {x.f}
                    {x.l ? `:${x.l}` : ''}
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}
