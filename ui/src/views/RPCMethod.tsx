import { useParams } from '@tanstack/react-router'
import { useMemo } from 'react'

import { DetailSheet } from '@/components/DetailSheet'
import { GraphPage } from '@/components/GraphPage'
import { RpcCell } from '@/components/RpcCell'
import { ScopeCard } from '@/components/ScopeCard'
import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { AppLink } from '@/components/ui/link'
import { buildFlowScene } from '@/graph/flowscene'
import type { Scene } from '@/graph/scenes'
import { useData } from '@/lib/context'
import { flowOf } from '@/lib/data'
import { useSourceSheet } from '@/lib/source'
import type { FuncFlow } from '@/lib/types'

const emptyScene: Scene = { nodes: [], edges: [], key: '' }

export function RPCMethodView() {
  const data = useData()
  const params = useParams({ strict: false }) as { si: string; mi: string }
  const si = Number(params.si)
  const mi = Number(params.mi)
  const svc = data.services?.[si]
  const method = svc?.methods[mi]
  const key = method?.fk ?? ''
  const flow: FuncFlow | undefined = key ? flowOf(data, key) : undefined
  const scene = useMemo(
    () => (flow ? buildFlowScene(flow, key, undefined, undefined, flow.in, undefined) : emptyScene),
    [flow, key],
  )

  const { sheet, setSheet, focus, openSource } = useSourceSheet()

  if (!svc || !method)
    return <div className="text-faint p-6 text-[12px]">this RPC method is not indexed</div>

  return (
    <GraphPage
      scene={scene}
      card={
        <ScopeCard
          title={`${svc.n} / ${method.n}`}
          badge={method.k}
          meta={method.f ? `${method.f}:${method.l} · ${svc.n}` : svc.n}
          doc={method.d}
          note={!method.d ? 'no doc comment on this method' : undefined}
          onSource={flow ? openSource : undefined}
          sourceLabel="source"
        />
      }
    >
      <div className="grid gap-3 p-3 md:grid-cols-4">
        <RpcCell label="service" value={svc.n} />
        <RpcCell label="kind" value={method.k} />
        <RpcCell label="request" value={method.rq} />
        <RpcCell label="response" value={method.rs} />
      </div>
      {(svc.mw ?? []).length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5 px-3 pb-3">
          <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">interceptors</span>
          {svc.mw!.map((mw) => (
            <Badge key={mw.n} variant="plain">
              {mw.n}
            </Badge>
          ))}
        </div>
      )}
      <div className="border-line flex flex-wrap items-center gap-2 border-t px-3 py-2">
        <code className="text-mut min-w-0 flex-1 truncate text-[10.5px]">{method.curl}</code>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            if (method.curl) navigator.clipboard?.writeText(method.curl)
            window.dispatchEvent(
              new CustomEvent('synoptic:toast', { detail: { message: 'grpcurl copied' } }),
            )
          }}
        >
          copy
        </Button>
        {method.fk && (
          <AppLink
            href={`/f?k=${encodeURIComponent(method.fk)}`}
            className={buttonVariants({ variant: 'ghost', size: 'sm' })}
          >
            open flow
          </AppLink>
        )}
      </div>
      {flow && (
        <DetailSheet
          open={sheet}
          onOpenChange={setSheet}
          title={flow.n}
          subtitle={flow.f ? `${flow.f}:${flow.l}` : flow.n}
          doc={flow.d}
          src={flow.s}
          focus={focus.line}
          focusLines={focus.lines}
        />
      )}
    </GraphPage>
  )
}
