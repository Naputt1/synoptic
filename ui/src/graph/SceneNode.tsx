import { Handle, type NodeProps, Position } from '@xyflow/react'
import { motion, useReducedMotion } from 'motion/react'
import { memo } from 'react'

import { AppLink } from '../components/ui/link'
import { absoluteLink } from '../lib/actions'
import { goto } from '../lib/nav'
import { statusBadgeTone, type StatusTone, statusTone } from '../lib/status'
import { CardMenu } from './NodeMenu'
import type { SData, SNode } from './scenes'

const returnTone: Record<
  StatusTone,
  { idle: string; done: string; cur: string; text: string; hover: string }
> = {
  ok: {
    idle: 'border-acc/45 bg-panel/50',
    done: 'border-acc/60 bg-acc/[0.06]',
    cur: 'border-acc/90 bg-acc/[0.12] shadow-[0_0_18px_-4px_rgba(88,224,183,0.55)]',
    text: 'text-acc',
    hover: 'hover:border-acc/80 hover:bg-acc/[0.08]',
  },
  info: {
    idle: 'border-info/45 bg-panel/50',
    done: 'border-info/60 bg-info/[0.06]',
    cur: 'border-info/90 bg-info/[0.12] shadow-[0_0_18px_-4px_rgba(159,198,255,0.55)]',
    text: 'text-info',
    hover: 'hover:border-info/80 hover:bg-info/[0.08]',
  },
  warn: {
    idle: 'border-warn/45 bg-panel/50',
    done: 'border-warn/55 bg-warn/[0.06]',
    cur: 'border-warn/85 bg-warn/[0.12] shadow-[0_0_18px_-4px_rgba(224,163,62,0.55)]',
    text: 'text-warn',
    hover: 'hover:border-warn/80 hover:bg-warn/[0.08]',
  },
  bad: {
    idle: 'border-bad/45 bg-panel/50',
    done: 'border-bad/60 bg-bad/[0.06]',
    cur: 'border-bad/90 bg-bad/[0.12] shadow-[0_0_18px_-4px_rgba(224,106,106,0.55)]',
    text: 'text-bad',
    hover: 'hover:border-bad/80 hover:bg-bad/[0.08]',
  },
  mut: {
    idle: 'border-line2 bg-panel/50',
    done: 'border-line2 bg-panel2',
    cur: 'border-line2 bg-panel2 shadow-[0_0_14px_-4px_rgba(125,156,153,0.45)]',
    text: 'text-mut',
    hover: 'hover:border-line2 hover:bg-panel2',
  },
}

const kindBadge: Record<string, string> = {
  root: 'server',
  group: 'group',
  route: 'route',
  stage: 'stage',
  call: 'call',
  request: 'in',
  response: 'out',
  exit: 'exit',
  loop: 'loop',
  note: 'control',
}

const methodTone: Record<string, string> = {
  GET: 'text-info',
  POST: 'text-acc',
  PUT: 'text-warn',
  PATCH: 'text-warn',
  DELETE: 'text-bad',
  ANY: 'text-mut',
}

function Tag({ t }: { t: string }) {
  const label = t === 'db' ? 'database' : t === 'cache' ? 'cache' : t === 'net' ? 'outbound' : t
  const kind = t.split(/[:× ]/)[0]
  const tone =
    t === 'db'
      ? 'text-warn border-warn/40'
      : t === 'cache'
        ? 'text-info border-info/40'
        : t === 'error'
          ? 'text-bad border-bad/40'
          : t === 'nil'
            ? 'text-acc border-acc/40'
            : t === 'void'
              ? 'text-mut border-line'
              : t === 'service'
                ? 'text-acc border-acc/40'
                : t === 'auth'
                  ? 'text-info border-info/40'
                  : kind === 'ctx'
                    ? 'text-info border-info/40'
                    : kind === 'query'
                      ? 'text-acc border-acc/40'
                      : kind === 'path' || kind === 'cookie' || kind === 'form'
                        ? 'text-warn border-warn/40'
                        : 'text-mut border-line'
  return (
    <span
      className={`rounded-[3px] border px-1 py-px text-[10px] tracking-[0.1em] uppercase ${tone}`}
    >
      {label}
    </span>
  )
}

function SceneNodeInner({ data }: NodeProps<SNode>) {
  const d = data as SData
  const reduce = useReducedMotion()
  const isBanner = d.kind === 'request' || d.kind === 'response'
  const isExit = d.kind === 'exit'
  const isRoot = d.kind === 'root'
  const isRoute = d.kind === 'route'
  const isStage = d.kind === 'stage'
  const isReturn = d.kind === 'return'
  const isInput = d.kind === 'input'
  const isService = d.kind === 'service'
  const isExternal = d.kind === 'module' && Boolean(d.external)
  const dim = Boolean(d.dim)
  const selected = Boolean(d.selected)
  const clickable = Boolean(d.href)
  const href = typeof d.href === 'string' ? d.href : undefined
  const isLabel = d.kind === 'label'

  if (isLabel) {
    return (
      <div className="text-faint px-0.5 text-[9.5px] tracking-[0.18em] uppercase">{d.title}</div>
    )
  }

  if (isInput) {
    const access = d.access as string | undefined
    const tone =
      access === 'set'
        ? 'border-acc/45 text-acc'
        : access === 'read'
          ? 'border-info/45 text-info'
          : 'border-warn/45 text-warn'
    const w = (d.w as number) ?? 380
    const wide = w >= 200
    return (
      <CardMenu data={d}>
        <motion.div
          initial={reduce ? false : { opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{
            duration: 0.2,
            delay: Math.min((d.index ?? 0) * 0.018, 0.4),
            ease: [0.22, 0.7, 0.3, 1],
          }}
          style={{ width: w, height: (d.h as number) ?? 44 }}
          role="button"
          tabIndex={0}
          aria-label={`${d.title} input group`}
          className={`bg-panel relative cursor-pointer rounded-[4px] border border-dashed ${wide ? 'flex items-center justify-between gap-3 px-3' : 'px-2 py-1'} ${tone}`}
          title={`${d.title} · ${access ?? ''}${d.sub ? ' · ' + d.sub : ''}`}
          onContextMenu={(e) => e.stopPropagation()}
        >
          <Handle type="target" position={Position.Left} id="l" />
          <Handle type="source" position={Position.Right} id="r" />
          <Handle type="target" position={Position.Top} id="t" />
          <Handle type="source" position={Position.Bottom} id="b" />
          <div className="min-w-0 truncate text-[10.5px] leading-tight">{d.title}</div>
          <div
            className={`truncate text-[8.5px] tracking-[0.08em] uppercase opacity-80 ${wide ? 'shrink-0' : 'mt-0.5'}`}
          >
            {d.sub}
          </div>
        </motion.div>
      </CardMenu>
    )
  }

  if (isReturn) {
    const tone = returnTone[statusTone(d.badge as string | undefined)]
    return (
      <CardMenu data={d}>
        <motion.div
          initial={reduce ? false : { opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{
            duration: 0.2,
            delay: Math.min((d.index ?? 0) * 0.018, 0.4),
            ease: [0.22, 0.7, 0.3, 1],
          }}
          style={{ width: (d.w as number) ?? 32, height: (d.h as number) ?? 32 }}
          className={`relative ${clickable ? 'cursor-pointer' : ''}`}
          title={d.title}
          onContextMenu={(e) => e.stopPropagation()}
        >
          <Handle type="target" position={Position.Top} id="t" />
          <Handle type="source" position={Position.Bottom} id="b" />
          <Handle type="target" position={Position.Left} id="l" />
          <Handle type="source" position={Position.Right} id="r" />
          <Handle type="target" position={Position.Right} id="ri" style={{ top: '50%' }} />
          <Handle type="source" position={Position.Left} id="lo" style={{ top: '50%' }} />
          <div
            className={`flex size-full items-center justify-center rounded-full border border-dashed transition-colors ${tone.hover} ${tone.idle}`}
          >
            <span className={`text-[10px] leading-none font-semibold tabular-nums ${tone.text}`}>
              {String(d.badge || '↩')}
            </span>
          </div>
        </motion.div>
      </CardMenu>
    )
  }

  // Dependency graph: a small dot with the package name underneath. Everything
  // else lives in the detail sheet, so the graph stays as light as Vitest's.
  if (d.kind === 'module' || d.kind === 'service') {
    const ext = isExternal
    const size = (d.w as number) ?? (isService ? 20 : ext ? 12 : 14)
    const dot = isService
      ? 'border-acc bg-acc/90 shadow-[0_0_14px_-2px_rgba(88,224,183,0.9)]'
      : d.unused
        ? 'border-warn/80 bg-warn/75'
        : ext
          ? 'border-info/50 bg-info/45'
          : 'border-acc/70 bg-acc/55'
    const label = isService
      ? 'text-acc font-semibold'
      : ext
        ? 'text-info/90'
        : d.unused
          ? 'text-warn/90'
          : 'text-ink'
    return (
      <CardMenu data={d}>
        <motion.div
          initial={reduce ? false : { opacity: 0, scale: 0.6 }}
          animate={{ opacity: dim ? 0.12 : 1, scale: 1 }}
          transition={{
            duration: dim ? 0.14 : 0.24,
            delay: dim ? 0 : Math.min((d.index ?? 0) * 0.008, 0.25),
            ease: [0.22, 0.7, 0.3, 1],
          }}
          style={{ width: size, height: size }}
          title={d.full}
          role="button"
          tabIndex={0}
          aria-label={`${d.title}${isService ? ' service' : ext ? ' third-party module' : ' package'}`}
          onContextMenu={(e) => e.stopPropagation()}
          className={`group relative ${clickable ? 'cursor-pointer' : ''}`}
        >
          <Handle
            type="target"
            position={Position.Left}
            id="l"
            style={{ opacity: 0, pointerEvents: 'none' }}
          />
          <Handle
            type="source"
            position={Position.Right}
            id="r"
            style={{ opacity: 0, pointerEvents: 'none' }}
          />
          <Handle
            type="target"
            position={Position.Top}
            id="t"
            style={{ opacity: 0, pointerEvents: 'none' }}
          />
          <Handle
            type="source"
            position={Position.Bottom}
            id="b"
            style={{ opacity: 0, pointerEvents: 'none' }}
          />
          <span
            aria-hidden
            className={`absolute inset-0 rounded-full border transition-transform group-hover:scale-125 ${dot} ${
              selected ? 'ring-ink/80 ring-2 ring-offset-2 ring-offset-[#071417]' : ''
            }`}
          />
          <span
            className={`bg-canvas/70 absolute top-full left-1/2 mt-1 max-w-[170px] -translate-x-1/2 truncate rounded-[3px] px-1 text-[10.5px] leading-[15px] ${label} ${
              selected ? 'bg-canvas text-ink' : ''
            }`}
          >
            {d.title}
          </span>
        </motion.div>
      </CardMenu>
    )
  }

  return (
    <CardMenu data={d}>
      <motion.div
        initial={reduce ? false : { opacity: 0 }}
        animate={{ opacity: dim ? 0.14 : 1 }}
        transition={{
          duration: dim ? 0.14 : 0.24,
          delay: dim ? 0 : Math.min((d.index ?? 0) * 0.018, 0.5),
          ease: [0.22, 0.7, 0.3, 1],
        }}
        style={{ width: (d.w as number) ?? 380, height: (d.h as number) ?? 84 }}
        title={d.full}
        onContextMenu={(e) => e.stopPropagation()}
        onAuxClick={(e) => {
          if (e.button !== 1 || !d.jump || !href) return
          if ((e.target as Element).closest?.('[data-open]')) return
          window.open(absoluteLink(href), '_blank')
        }}
        className={[
          'relative flex flex-col justify-center overflow-hidden rounded-[6px] border px-2.5 py-2 text-ink',
          isBanner
            ? 'border-dashed border-acc/35 bg-transparent text-acc/90'
            : isExit
              ? 'border-dashed border-warn/45 bg-warn/[0.04]'
              : isRoot
                ? 'border-line2 bg-panel2'
                : 'border-line bg-panel',
          clickable ? 'cursor-pointer transition-colors hover:border-acc/60 hover:bg-panel2' : '',
          selected ? 'ring-1 ring-acc/60' : '',
        ].join(' ')}
      >
        <Handle type="target" position={Position.Top} id="t" />
        <Handle type="source" position={Position.Bottom} id="b" />
        <Handle type="target" position={Position.Bottom} id="bi" style={{ left: '75%' }} />
        <Handle type="target" position={Position.Left} id="l" />
        <Handle type="source" position={Position.Right} id="r" />
        <Handle type="target" position={Position.Right} id="ri" style={{ top: '50%' }} />
        <Handle type="source" position={Position.Left} id="lo" style={{ top: '50%' }} />

        <span className="border-line2 bg-canvas absolute top-[13px] -left-[19px] size-[7px] rounded-full border" />

        <div className="flex items-center gap-1.5">
          {d.index !== undefined && d.index > 0 && !isBanner && (
            <span className="border-line text-faint rounded-[3px] border px-1 text-[10px]">
              {d.index}
            </span>
          )}
          {isRoute && d.badge && (
            <span
              className={`text-[10px] font-semibold tracking-[0.08em] ${methodTone[d.badge] ?? 'text-mut'}`}
            >
              {d.badge}
            </span>
          )}
          <span
            className={`min-w-0 flex-1 truncate ${isBanner ? 'text-[11px] tracking-[0.06em]' : 'text-[12px]'}`}
          >
            {d.kind === 'loop' && <span className="text-warn mr-1">↺</span>}
            {d.title}
          </span>
          {d.badge && !isRoute && !isExit && (
            <span className="border-line text-mut shrink-0 rounded-[3px] border px-1 text-[9.5px] tracking-[0.1em] uppercase">
              {d.badge}
            </span>
          )}
          {href && (
            <AppLink
              href={href}
              data-open
              aria-label={`open ${d.title}`}
              className="nopan nodrag text-faint hover:border-line hover:bg-panel2 hover:text-acc shrink-0 cursor-pointer rounded-[3px] border border-transparent px-1 py-0.5 text-[10px] transition-colors"
              title="open this function"
              onClick={(e) => e.stopPropagation()}
            >
              open ›
            </AppLink>
          )}
        </div>

        {d.impls?.length ? (
          <select
            className="nodrag nopan border-line bg-canvas text-ink mt-1 w-full cursor-pointer rounded-[3px] border px-1 py-0.5 text-[10px] outline-none"
            defaultValue=""
            aria-label={`choose one of ${d.impls.length} implementations of ${d.title}`}
            title={`${d.impls.length} implementations`}
            onPointerDown={(e) => e.stopPropagation()}
            onClick={(e) => e.stopPropagation()}
            onChange={(e) => {
              const to = e.currentTarget.value
              if (to) goto(to)
            }}
          >
            <option value="">choose implementation…</option>
            {d.impls.map((im) => (
              <option key={im.href} value={im.href}>
                {im.r ? `${im.r}.` : ''}
                {im.n} · {im.p}
              </option>
            ))}
          </select>
        ) : null}

        {!d.impls?.length && (d.sub || (d.tags && d.tags.length > 0) || isStage) && (
          <div className="mt-1 flex flex-wrap items-center gap-1.5">
            {d.sub && (
              <span className="text-mut min-w-0 flex-1 truncate text-[10.5px]" title={d.sub}>
                {d.sub}
              </span>
            )}
            {(d.tags ?? []).map((t) => (
              <Tag key={t} t={t} />
            ))}
            {d.ms && (
              <span className="text-info ml-auto shrink-0 text-[10.5px] tabular-nums">{d.ms}</span>
            )}
            {isStage && !d.badge && (
              <span className="text-faint text-[9.5px]">{kindBadge[d.kind]}</span>
            )}
          </div>
        )}

        {typeof d.share === 'number' && d.share > 0 && (
          <span className="bg-line pointer-events-none absolute inset-x-[7px] bottom-[5px] h-[2px] rounded-full">
            <span
              className="bg-acc/70 block h-full rounded-full"
              style={{ width: `${Math.min(100, Math.max(2, Math.round(d.share * 100)))}%` }}
            />
          </span>
        )}

        {isExit && (
          <div className="mt-1 flex items-center gap-1.5 self-start">
            <span className="text-mut text-[10px]">returns here</span>
            {d.rk && <Tag t={d.rk} />}
            {d.badge && (!d.rk || d.badge !== 'exits') && (
              <span
                className={`rounded-[3px] border px-1 text-[10px] leading-[14px] ${statusBadgeTone[statusTone(d.badge as string | undefined)]}`}
              >
                {d.badge}
              </span>
            )}
          </div>
        )}
      </motion.div>
    </CardMenu>
  )
}

export const SceneNode = memo(SceneNodeInner)
