import { useEffect, useState, type MouseEvent } from 'react'
import { createPortal } from 'react-dom'

import { statusBadgeTone, statusTone } from '../lib/status'
import type { JErr } from '../lib/types'

type At = { x: number; y: number }

const WIDTH = 330
const KINDS: Record<JErr['k'], string> = {
  response: 'response',
  error: 'error',
  log: 'log',
  panic: 'panic',
}

/**
 * Hover preview for the messages a status circle or exit badge can emit.
 * Portalled to the body so React Flow's node clipping cannot cut it off.
 */
export function useErrTip(errs?: JErr[]) {
  const [at, setAt] = useState<At | null>(null)
  const active = Boolean(errs?.length)

  useEffect(() => {
    if (!active) setAt(null)
  }, [active])

  const bind = active
    ? {
        onMouseEnter: (e: MouseEvent) => setAt({ x: e.clientX, y: e.clientY }),
        onMouseMove: (e: MouseEvent) => setAt({ x: e.clientX, y: e.clientY }),
        onMouseLeave: () => setAt(null),
      }
    : {}

  const node =
    active && at && errs
      ? createPortal(<Tip errs={errs} at={at} />, document.body)
      : null

  return { bind, node }
}

function Tip({ errs, at }: { errs: JErr[]; at: At }) {
  const below = at.y < 230
  const left = Math.max(10, Math.min(at.x + 14, window.innerWidth - WIDTH - 10))
  const style = {
    width: WIDTH,
    left,
    top: below ? at.y + 16 : at.y - 12,
    transform: below ? undefined : 'translateY(-100%)',
  }

  return (
    <div
      role="tooltip"
      style={style}
      className="border-line2 bg-panel pointer-events-none fixed z-[90] overflow-hidden rounded-[6px] border shadow-[0_18px_50px_-18px_rgba(0,0,0,0.85)]"
    >
      <div className="border-line flex items-center justify-between border-b px-2.5 py-1.5">
        <span className="text-faint text-[9px] tracking-[0.14em] uppercase">
          runtime messages
        </span>
        <span className="text-mut text-[9.5px] tabular-nums">{errs.length}</span>
      </div>
      <div className="flex flex-col">
        {errs.slice(0, 5).map((e, i) => (
          <div key={`${e.f}:${e.l}:${i}`} className="flex flex-col gap-0.5 px-2.5 py-1.5">
            <div className="flex items-center gap-1.5">
              {e.c?.length ? (
                <span
                  className={`rounded-[3px] border px-1 text-[9px] leading-[13px] tabular-nums ${statusBadgeTone[statusTone(e.c[0])]}`}
                >
                  {e.c.join(' · ')}
                </span>
              ) : (
                <span className="border-line2 text-mut rounded-[3px] border px-1 text-[9px] leading-[13px] uppercase">
                  {KINDS[e.k]}
                </span>
              )}
              <span className="text-ink min-w-0 flex-1 truncate font-mono text-[11px]">
                {e.m}
              </span>
            </div>
            <span className="text-faint truncate pl-0.5 font-mono text-[9.5px]">
              {e.f}:{e.l}
            </span>
          </div>
        ))}
      </div>
      {errs.length > 5 && (
        <div className="border-line text-faint border-t px-2.5 py-1 text-[9.5px]">
          +{errs.length - 5} more on this step
        </div>
      )}
    </div>
  )
}
