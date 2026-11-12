import { useSearch } from '@tanstack/react-router'
import { useMemo } from 'react'

import { DetailSheet } from '@/components/DetailSheet'
import { GraphPage } from '@/components/GraphPage'
import { ScopeCard } from '@/components/ScopeCard'
import { buildFlowScene } from '@/graph/flowscene'
import { useData } from '@/lib/context'
import { flowOf } from '@/lib/data'
import { useSourceSheet } from '@/lib/source'
import type { FuncFlow } from '@/lib/types'
import { Inputs } from '@/panes/DocPanel'

export function FnView() {
  const data = useData()
  const search = useSearch({ strict: false }) as { k?: string; ri?: string; si?: string }
  const key = search.k ?? ''
  const flow: FuncFlow | undefined = flowOf(data, key)
  const parentRi = Number(search.ri)
  const parentSi = Number(search.si)
  const parentOk =
    search.ri !== undefined &&
    search.si !== undefined &&
    Number.isInteger(parentRi) &&
    Number.isInteger(parentSi)
  const origin = useMemo(
    () => (parentOk ? { ri: parentRi, si: parentSi } : undefined),
    [parentOk, parentRi, parentSi],
  )
  const parent = parentOk ? data.routes[parentRi] : undefined
  const nextStage = useMemo(
    () =>
      parent && parentSi + 1 < parent.ch.length
        ? { label: parent.ch[parentSi + 1].n, href: `/r/${parentRi}/s/${parentSi + 1}` }
        : undefined,
    [parent, parentRi, parentSi],
  )
  const prevStage = useMemo(
    () =>
      parent && parentSi > 0
        ? { label: parent.ch[parentSi - 1].n, href: `/r/${parentRi}/s/${parentSi - 1}` }
        : undefined,
    [parent, parentRi, parentSi],
  )
  const scene = useMemo(
    () =>
      flow
        ? buildFlowScene(flow, key, nextStage, prevStage, flow.in, origin)
        : { nodes: [], edges: [], key: '' },
    [flow, key, nextStage, prevStage, origin],
  )

  const { sheet, setSheet, focus, openSource } = useSourceSheet()

  const rpc = useMemo(() => {
    const [si, mi] = (flow?.rpc ?? '').split('/').map(Number)
    const svc = data.services?.[si]
    const m = svc?.methods[mi]
    if (!svc || !m) return undefined
    const name = svc.n.includes('.') ? svc.n.slice(svc.n.lastIndexOf('.') + 1) : svc.n
    return { label: `${name}/${m.n}`, href: `/rpc/${si}/${mi}` }
  }, [flow, data])

  if (!flow)
    return <div className="text-faint p-6 text-[12px]">this function is not in the module</div>

  return (
    <GraphPage
      scene={scene}
      card={
        <ScopeCard
          title={flow.n}
          badge="func"
          meta={
            flow.f
              ? `${flow.f}:${flow.l}${parent ? ` · ${parent.m} ${parent.p} · stage ${parentSi + 1}` : ''}`
              : key
          }
          doc={flow.d}
          note={!flow.d ? 'no doc comment on this one — the flow below is what it does' : undefined}
          onSource={openSource}
          sourceLabel="source"
          rpc={rpc}
        />
      }
    >
      {flow.in && flow.in.length > 0 && <Inputs items={flow.in} flush />}
      <DetailSheet
        open={sheet}
        onOpenChange={setSheet}
        title={flow.n}
        subtitle={flow.f ? `${flow.f}:${flow.l}` : key}
        doc={flow.d}
        src={flow.s}
        inputs={flow.in}
        focus={focus.line}
        focusLines={focus.lines}
      />
    </GraphPage>
  )
}
