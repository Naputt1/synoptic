import { AppLink } from '@/components/ui/link'
import type { Step } from '@/lib/types'

export function TraceRows({
  steps,
  depth = 0,
  onFn,
  limit = 3,
}: {
  steps: Step[]
  depth?: number
  onFn?: (key: string) => string
  limit?: number
}) {
  const list = steps.slice(0, depth === 0 ? limit : 2)
  return (
    <div className={depth === 0 ? 'space-y-1' : 'border-line/60 mt-1 space-y-1 border-l pl-3'}>
      {list.map((s, i) => {
        const branch = s.b?.length || s.e?.length
        return (
          <div key={`${depth}:${i}:${s.l ?? 0}:${s.t}`}>
            <div className="flex items-start gap-2">
              <span className="text-faint mt-[1px] w-3 shrink-0 text-right font-mono text-[9px]">
                {depth === 0 ? i + 1 : ''}
              </span>
              <span className="text-faint mt-[1px] w-7 shrink-0 text-[8px] tracking-[0.08em] uppercase">
                {s.k === 'ret' ? 'return' : s.k}
              </span>
              <div className="min-w-0">
                <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
                  {s.fk && onFn ? (
                    <AppLink
                      href={onFn(s.fk)}
                      className={`text-acc text-left font-mono text-[10.5px] leading-relaxed break-words hover:underline`}
                    >
                      {s.t}
                    </AppLink>
                  ) : (
                    <div
                      className={`font-mono text-[10.5px] leading-relaxed break-words ${
                        s.k === 'ret' || s.k === 'exit' ? 'text-warn' : 'text-ink'
                      }`}
                    >
                      {s.t}
                    </div>
                  )}
                  {(s.k === 'ret' || s.k === 'exit') && s.rk ? (
                    <span
                      className={`shrink-0 rounded border px-1 text-[8px] tracking-[0.08em] uppercase ${
                        s.rk === 'error'
                          ? 'border-bad/40 text-bad'
                          : s.rk === 'nil'
                            ? 'border-acc/40 text-acc'
                            : 'border-line text-mut'
                      }`}
                    >
                      {s.rk}
                    </span>
                  ) : null}
                </div>
                {(s.cd || s.s) && (
                  <div className="text-faint font-mono text-[9px] break-words">{s.cd || s.s}</div>
                )}
                {s.l ? <div className="text-faint font-mono text-[8.5px]">:{s.l}</div> : null}
              </div>
            </div>
            {branch && s.b?.length ? <TraceRows steps={s.b} depth={depth + 1} onFn={onFn} /> : null}
            {branch && s.e?.length ? (
              <>
                <div className="text-faint mt-1 pl-5 text-[9px] tracking-[0.12em] uppercase">
                  else
                </div>
                <TraceRows steps={s.e} depth={depth + 1} onFn={onFn} />
              </>
            ) : null}
          </div>
        )
      })}
      {steps.length > list.length && depth === 0 ? (
        <div className="text-faint pl-5 text-[9.5px] italic">
          +{steps.length - list.length} more steps in the full flow
        </div>
      ) : null}
    </div>
  )
}
