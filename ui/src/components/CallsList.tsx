import { AppLink } from '@/components/ui/link'
import type { Call } from '@/lib/types'

export function CallsList({ calls }: { calls?: Call[] }) {
  if (!calls || calls.length === 0) return null
  return (
    <div className="mt-4">
      <div className="text-faint text-[9.5px] tracking-[0.14em] uppercase">calls in this repo</div>
      <div className="mt-1.5 flex flex-col gap-1">
        {calls.map((c, i) => (
          <div key={i} className="flex items-baseline gap-2 text-[11.5px]">
            <span className="text-ink">{c.e}</span>
            {c.f &&
              (c.k ? (
                <AppLink
                  href={`/f?k=${encodeURIComponent(c.k)}`}
                  className="text-faint hover:text-acc truncate text-[10px]"
                >
                  {c.f}
                  {c.l ? `:${c.l}` : ''}
                </AppLink>
              ) : (
                <span className="text-faint truncate text-[10px]">
                  {c.f}
                  {c.l ? `:${c.l}` : ''}
                </span>
              ))}
            {c.d && <span className="text-faint truncate text-[10px]">{c.d}</span>}
          </div>
        ))}
      </div>
    </div>
  )
}
