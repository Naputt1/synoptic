import { Badge } from '@/components/ui/badge'
import { AppLink } from '@/components/ui/link'
import type { Effect } from '@/lib/types'

export function EffectRows({ items, onFn }: { items: Effect[]; onFn?: (key: string) => string }) {
  return (
    <div className="divide-line/40 border-line/40 divide-y border-y">
      {items.map((x, i) => (
        <div key={`${x.k}:${x.n}:${x.l ?? 0}:${i}`} className="flex items-start gap-2 py-1.5">
          <Badge variant={x.k === 'publish' ? 'accent' : x.k === 'write' ? 'warn' : 'info'}>
            {x.k}
          </Badge>
          <div className="min-w-0 flex-1">
            {x.fk && onFn ? (
              <AppLink
                href={onFn(x.fk)}
                className="text-acc text-left font-mono text-[10.5px] break-words hover:underline"
              >
                {x.n}
              </AppLink>
            ) : (
              <code className="text-ink font-mono text-[10.5px] break-words">{x.n}</code>
            )}
            {x.s && x.s !== x.n ? (
              <div className="text-faint font-mono text-[9px] break-words">{x.s}</div>
            ) : null}
          </div>
          {x.l ? <span className="text-faint font-mono text-[8.5px]">:{x.l}</span> : null}
        </div>
      ))}
    </div>
  )
}
