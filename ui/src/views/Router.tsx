import { useParams } from '@tanstack/react-router'
import { useMemo, useState } from 'react'

import { GraphPage } from '@/components/GraphPage'
import { ScopeCard } from '@/components/ScopeCard'
import { AppLink } from '@/components/ui/link'
import { Segmented } from '@/components/ui/segmented'
import { buildRouterScene } from '@/graph/scenes'
import { useData, useSvc } from '@/lib/context'

export function RouterView() {
  const data = useData()
  const svc = useSvc()
  const params = useParams({ strict: false }) as { _splat?: string }
  const focus = params._splat ?? ''
  const [proto, setProto] = useState<'all' | 'http' | 'ws' | 'sse'>('all')
  const scene = useMemo(() => buildRouterScene(data, focus, proto, svc), [data, focus, proto, svc])
  const label = focus ? '/' + focus : data.repo
  const shown = data.routes.filter(
    (r) =>
      (proto === 'all' ? true : proto === 'http' ? !r.pr : r.pr === proto) &&
      (!svc || r.svc === svc) &&
      (focus ? r.p === '/' + focus || r.p.startsWith('/' + focus + '/') : true),
  )
  const count = shown.length
  const changed = shown.filter((r) => r.chg).length
  const changeMeta =
    data.change && changed > 0 ? ` · ${changed} changed since ${data.change.ref}` : ''
  const rpcServices = (data.services ?? []).filter((s) => !svc || s.svc === svc)
  const rpcCount = rpcServices.reduce((n, s) => n + s.methods.length, 0)

  return (
    <GraphPage
      scene={scene}
      card={
        <>
          <ScopeCard
            title={label}
            badge="router"
            meta={`${count} under this path${changeMeta} · click a box to open it`}
            note="each column is one path segment. open a group to walk deeper, a route to see its middleware."
          />
          <div className="border-line bg-panel/20 flex items-center gap-2 border-b px-4 py-1.5">
            <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">protocol</span>
            <Segmented
              value={proto}
              onChange={(v) => setProto(v as typeof proto)}
              options={[
                { value: 'all', label: 'all' },
                { value: 'http', label: 'http' },
                { value: 'ws', label: 'ws' },
                { value: 'sse', label: 'sse' },
              ]}
            />
            <span className="text-faint ml-auto text-[10px]">
              {count} {count === 1 ? 'route' : 'routes'}
            </span>
          </div>
          {rpcCount > 0 && (
            <AppLink
              href="/rpc"
              className="border-line hover:bg-panel flex flex-wrap items-center gap-x-2 border-b px-4 py-1.5 text-[11px]"
            >
              <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">rpc</span>
              <span className="text-ink font-semibold">
                {rpcServices.length} {rpcServices.length === 1 ? 'service' : 'services'}
              </span>
              <span className="text-mut">{rpcCount} methods</span>
              <span className="text-acc ml-auto">open gRPC surface ›</span>
            </AppLink>
          )}
        </>
      }
    />
  )
}
