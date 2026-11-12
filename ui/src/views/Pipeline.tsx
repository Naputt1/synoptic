import { useParams } from '@tanstack/react-router'
import { useEffect, useMemo, useState } from 'react'

import { DetailSheet } from '@/components/DetailSheet'
import { GraphPage } from '@/components/GraphPage'
import { ScopeCard } from '@/components/ScopeCard'
import { StreamBoard } from '@/components/StreamBoard'
import { Segmented } from '@/components/ui/segmented'
import { SceneCanvas } from '@/graph/SceneCanvas'
import { buildPipelineScene } from '@/graph/scenes'
import { useData } from '@/lib/context'
import { flowOf, msOf, timingOf, touchTags } from '@/lib/data'
import { DocPanel } from '@/panes/DocPanel'

export function PipelineView() {
  const data = useData()
  const params = useParams({ strict: false }) as { ri: string }
  const ri = Number(params.ri)
  const r = data.routes[ri]
  const timing = r ? timingOf(data, r) : undefined
  const over = timing
  const scene = useMemo(
    () => (r ? buildPipelineScene(r, ri, over, data.flows) : { nodes: [], edges: [], key: '' }),
    [r, ri, over, data.flows],
  )
  const [sheet, setSheet] = useState<number | null>(null)
  const [streamView, setStreamView] = useState<'exchanges' | 'canvas'>('exchanges')

  useEffect(() => {
    setStreamView('exchanges')
  }, [ri])

  if (!r) return <div className="text-faint p-6 text-[12px]">no such route</div>

  const handler = r.ch[r.ch.length - 1]
  const mw = r.ch.filter((s) => (s.r ?? '') === 'middleware').length
  const tags = [...new Set(r.ch.flatMap((s) => touchTags(s, false)))]
  if (r.new) tags.push('new')
  else if (r.chg) tags.push('changed')
  const current = sheet !== null ? r.ch[sheet] : undefined
  const flow = current?.fk ? flowOf(data, current.fk) : undefined
  const slow = timing && timing.slow >= 0 ? timing.st.find((x) => x.i === timing.slow) : undefined
  const slowName = slow ? r.ch[slow.i]?.n : undefined
  const meta =
    `${r.f}:${r.l} · ${r.ch.length} step${r.ch.length === 1 ? '' : 's'} · ${mw} middleware first` +
    (over ? ` · ${msOf(over.total)} per request · ${over.spans} spans` : '')
  const note = over
    ? slow && slowName
      ? `slowest stage: ${slowName} — ${msOf(slow.m)} median, ${msOf(slow.p95)} p95, ${Math.round(slow.sh * 100)}% of the time`
      : 'the trace matched this route but not its stages'
    : handler?.d
      ? undefined
      : 'no doc comment on the handler — open it, the flow is the description'

  const card = (
    <ScopeCard
      title={`${r.m} ${r.p}`}
      badge={r.pr ? r.pr.toUpperCase() : 'route'}
      meta={meta}
      doc={over ? undefined : handler?.d}
      note={note}
      tags={tags}
      onSource={handler ? () => setSheet(r.ch.length - 1) : undefined}
      sourceLabel="handler"
    />
  )
  const sheets = (
    <DetailSheet
      open={sheet !== null}
      onOpenChange={(v) => !v && setSheet(null)}
      title={current?.n ?? ''}
      subtitle={current?.f ? `${current.f}:${current.l}` : undefined}
      doc={current?.d}
      src={current?.s ?? flow?.s}
      calls={current?.c}
      stage={current}
      inputs={current?.in}
    />
  )

  if (r.pr && r.st) {
    return (
      <div className="flex h-full flex-col">
        {card}
        <div className="border-line bg-panel/30 flex flex-wrap items-center gap-2 border-b px-3 py-1.5">
          <Segmented
            value={streamView}
            options={[
              { value: 'exchanges', label: 'events' },
              { value: 'canvas', label: 'flow' },
            ]}
            onChange={(v) => setStreamView(v as 'exchanges' | 'canvas')}
          />
          <span className="text-faint text-[10px]">
            pick an event to read its request, replies and calls
          </span>
        </div>
        <div className="relative min-h-0 flex-1">
          {streamView === 'exchanges' ? (
            <StreamBoard route={r} flows={data.flows} ri={ri} />
          ) : (
            <SceneCanvas scene={scene} />
          )}
        </div>
        <div className="border-line bg-panel/30 max-h-[26vh] shrink-0 overflow-auto border-t">
          <DocPanel r={r} />
        </div>
        {sheets}
      </div>
    )
  }

  return (
    <GraphPage scene={scene} card={card}>
      <DocPanel r={r} />
      {sheets}
    </GraphPage>
  )
}
