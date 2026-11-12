import { Code2 } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { AppLink } from '@/components/ui/link'

export function ScopeCard({
  title,
  badge,
  meta,
  doc,
  note,
  tags,
  onSource,
  sourceLabel,
  rpc,
}: {
  title: string
  badge?: string
  meta?: string
  doc?: string
  note?: string
  tags?: string[]
  onSource?: () => void
  sourceLabel?: string
  rpc?: { label: string; href: string }
}) {
  return (
    <div className="border-line bg-panel/40 flex flex-wrap items-start gap-x-4 gap-y-1 border-b px-4 py-2">
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-ink truncate text-[14px]">{title}</span>
          {badge && <Badge variant="accent">{badge}</Badge>}
          {(tags ?? []).map((t) => (
            <Badge key={t} variant={t === 'db' ? 'warn' : t === 'cache' ? 'info' : 'default'}>
              {t === 'db' ? 'database' : t === 'cache' ? 'cache' : t === 'net' ? 'outbound' : t}
            </Badge>
          ))}
        </div>
        {meta && <div className="text-faint mt-0.5 truncate text-[10.5px]">{meta}</div>}
        {doc && <p className="text-ink/80 mt-1 line-clamp-2 text-[11.5px] leading-[1.5]">{doc}</p>}
        {note && <p className="text-faint mt-1 text-[11px] leading-[1.5] italic">{note}</p>}
      </div>
      {rpc && (
        <AppLink
          href={rpc.href}
          className={buttonVariants({ variant: 'outline', size: 'sm' })}
          title={`${rpc.label} on the RPC surface`}
        >
          rpc · {rpc.label}
        </AppLink>
      )}
      {onSource && (
        <Button variant="outline" size="sm" onClick={onSource}>
          <Code2 className="size-3" /> {sourceLabel ?? 'source'}
        </Button>
      )}
    </div>
  )
}
