import { BaseEdge, EdgeLabelRenderer, type EdgeProps, getSmoothStepPath } from '@xyflow/react'
import { useState } from 'react'

export function ExitEdge({
  id,
  label,
  style,
  markerEnd,
  sourceX,
  sourceY,
  targetX,
  targetY,
  sourcePosition,
  targetPosition,
}: EdgeProps) {
  const [open, setOpen] = useState(false)
  const [path] = getSmoothStepPath({
    sourceX,
    sourceY,
    targetX,
    targetY,
    sourcePosition,
    targetPosition,
    borderRadius: 8,
  })
  const text = typeof label === 'string' && label ? label : 'exits'
  // the corner where the horizontal stub meets the return line: the
  // leftmost point of the drawn path, taken at the stub's own height.
  const pts = [...path.matchAll(/(-?[\d.]+),(-?[\d.]+)/g)].map((m) => ({ x: +m[1], y: +m[2] }))
  const railX = pts.length ? Math.min(...pts.map((p) => p.x)) : Math.min(sourceX, targetX) - 20
  const vertical = pts.filter((p) => Math.abs(p.x - railX) < 0.5)
  const jointX = railX
  const jointY = vertical.length
    ? vertical.reduce((best, p) =>
        Math.abs(p.y - sourceY) < Math.abs(best.y - sourceY) ? p : best,
      ).y
    : sourceY

  return (
    <>
      <BaseEdge id={id} path={path} style={style} markerEnd={markerEnd} />
      <g
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        style={{ cursor: 'help', pointerEvents: 'all' }}
      >
        <circle cx={jointX} cy={jointY} r={9} fill="transparent" />
        <circle
          cx={jointX}
          cy={jointY}
          r={open ? 4.5 : 3.5}
          fill="#0b1e23"
          stroke="#e0a33e"
          strokeWidth={open ? 2 : 1.4}
        />
      </g>
      <EdgeLabelRenderer>
        <div
          style={{ transform: `translate(-100%,-50%) translate(${jointX - 12}px,${jointY}px)` }}
          className={`pointer-events-none absolute ${open ? 'border-warn/45 bg-panel2 w-[184px] rounded-[4px] border px-2.5 py-1.5 text-[10.5px] leading-relaxed shadow-[0_6px_20px_-8px_rgba(0,0,0,0.9)]' : 'border-warn/45 bg-panel2 text-warn rounded-[3px] border px-1.5 py-0.5 font-mono text-[9px] leading-none'}`}
        >
          {open ? (
            <>
              <div className="text-faint mb-1 font-mono text-[9px] tracking-[0.14em] uppercase">
                early return
              </div>
              <div className="text-warn font-mono">{text}</div>
              <div className="text-mut mt-1 text-[10px]">
                this stage can leave before the next one runs
              </div>
            </>
          ) : (
            <>early</>
          )}
        </div>
      </EdgeLabelRenderer>
    </>
  )
}
