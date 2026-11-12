import { useParams } from '@tanstack/react-router'
import { useMemo } from 'react'

import { DetailSheet } from '@/components/DetailSheet'
import { GraphPage } from '@/components/GraphPage'
import { ScopeCard } from '@/components/ScopeCard'
import { buildFlowScene } from '@/graph/flowscene'
import { useData } from '@/lib/context'
import { flowOf, touchTags } from '@/lib/data'
import { useSourceSheet } from '@/lib/source'
import { Inputs } from '@/panes/DocPanel'

export function FlowView() {
  const data = useData()
  const params = useParams({ strict: false }) as { ri: string; si: string }
  const ri = Number(params.ri)
  const si = Number(params.si)
  const r = data.routes[ri]
  const st = r?.ch[si]
  const flow = st?.fk ? flowOf(data, st.fk) : undefined
  const key = st?.fk ?? 'none'
  const nextStage = useMemo(
    () =>
      r && si + 1 < r.ch.length
        ? { label: r.ch[si + 1].n, href: '/r/' + ri + '/s/' + (si + 1) }
        : undefined,
    [r, ri, si],
  )
  const prevStage = useMemo(
    () =>
      r && si > 0 ? { label: r.ch[si - 1].n, href: '/r/' + ri + '/s/' + (si - 1) } : undefined,
    [r, ri, si],
  )
  const scene = useMemo(
    () =>
      flow
        ? buildFlowScene(flow, key, nextStage, prevStage, st?.in, { ri, si })
        : { nodes: [], edges: [], key: '' },
    [flow, key, nextStage, prevStage, st?.in, ri, si],
  )

  const { sheet, setSheet, focus, openSource } = useSourceSheet()

  if (!r || !st) return <div className="text-faint p-6 text-[12px]">no such step</div>

  return (
    <GraphPage
      scene={scene}
      card={
        <ScopeCard
          title={st.n}
          badge={st.r ?? st.k}
          meta={st.f ? `${st.f}:${st.l}` : undefined}
          doc={st.d}
          note={
            !st.d
              ? flow
                ? 'no doc comment on this one — the flow below is what it does'
                : 'no doc comment, and the body is outside this repo'
              : undefined
          }
          tags={[
            ...touchTags(st),
            ...(st.chg ? ['changed'] : []),
            ...(flow?.trunc ? ['truncated'] : []),
          ]}
          onSource={openSource}
          sourceLabel="source"
        />
      }
    >
      {st.in && st.in.length > 0 && <Inputs items={st.in} flush />}
      <DetailSheet
        open={sheet}
        onOpenChange={setSheet}
        title={st.n}
        subtitle={st.f ? `${st.f}:${st.l}` : undefined}
        doc={st.d}
        src={st.s ?? flow?.s}
        calls={st.c}
        stage={st}
        inputs={st.in}
        focus={focus.line}
        focusLines={focus.lines}
      />
    </GraphPage>
  )
}
