import { useRouterState } from '@tanstack/react-router'
import { Search } from 'lucide-react'
import { useMemo } from 'react'

import { Button } from '@/components/ui/button'
import { AppLink } from '@/components/ui/link'
import { Segmented } from '@/components/ui/segmented'
import { useData, useSvc, useSvcSwitch } from '@/lib/context'
import { routeHref, stageHref } from '@/lib/data'
import { goto } from '@/lib/nav'

export function Topbar({ onSearch }: { onSearch: () => void }) {
  const data = useData()
  const svc = useSvc()
  const { setSvc } = useSvcSwitch()
  const scope = useMemo(() => {
    if (!svc) {
      return {
        routes: data.stats.routes,
        streams: data.stats.streams ?? 0,
        services: data.stats.services ?? 0,
        rpcs: data.stats.rpcs ?? 0,
        documented: data.stats.documented,
      }
    }
    const routes = data.routes.filter((r) => r.svc === svc)
    const services = (data.services ?? []).filter((s) => s.svc === svc)
    return {
      routes: routes.length,
      streams: routes.filter((r) => r.pr || r.st).length,
      services: services.length,
      rpcs: services.reduce((n, s) => n + s.methods.length, 0),
      documented: routes.filter((r) => r.ch.some((st) => st.r === 'handler' && st.d)).length,
    }
  }, [svc, data])
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const parts = pathname.split('/').filter(Boolean)
  const level =
    parts[0] === 'rpc'
      ? ''
      : parts[0] === 'f'
        ? ''
        : parts[0] === 'r'
          ? parts[2] === 's'
            ? 'step'
            : 'route'
          : 'router'
  const ri = parts[0] === 'r' ? Number(parts[1]) : -1
  const blind = (data.diag?.skipped ?? 0) + (data.diag?.unresolved ?? 0)
  const go = (lv: string) => {
    if (lv === 'router') goto('/')
    else if (lv === 'route' && ri >= 0) goto(routeHref(ri))
    else if (lv === 'step' && ri >= 0) {
      const r = data.routes[ri]
      const si = r && parts[2] === 's' ? Number(parts[3]) : 0
      if (r && r.ch[si]) goto(stageHref(ri, si))
    }
  }

  return (
    <header className="border-line flex flex-wrap items-center gap-x-5 gap-y-2 border-b px-4 py-2">
      <div className="flex items-baseline gap-2">
        <span className="text-faint text-[10px] tracking-[0.18em] uppercase">route synoptic</span>
        <span className="text-ink text-[13px]">{data.repo}</span>
      </div>
      <div className="text-mut hidden gap-x-3 text-[10.5px] sm:flex">
        <span>
          <b className="text-ink font-semibold">{scope.routes}</b> routes
        </span>
        {scope.streams > 0 && (
          <span>
            <b className="text-ink font-semibold">{scope.streams}</b> stream
            {scope.streams === 1 ? '' : 's'}
          </span>
        )}
        {scope.services > 0 && (
          <span>
            <b className="text-ink font-semibold">{scope.services}</b> rpc ·{' '}
            <b className="text-ink font-semibold">{scope.rpcs}</b>{' '}
            {scope.rpcs === 1 ? 'method' : 'methods'}
          </span>
        )}
        <span>
          <b className="text-ink font-semibold">{data.stats.middleware}</b> middleware
        </span>
        <span>
          <b className="text-ink font-semibold">{scope.documented}</b>/{scope.routes} documented
        </span>
        <span>
          <b className="text-ink font-semibold">{data.stats.funcs}</b> flows
        </span>
      </div>
      <div className="ml-auto flex flex-wrap items-center gap-3">
        {(data.svcs?.length ?? 0) > 1 && (
          <div className="flex items-center gap-1.5">
            <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">service</span>
            <Segmented
              value={svc || 'all'}
              onChange={(v) => setSvc(v === 'all' ? '' : v)}
              options={[
                { value: 'all', label: 'all', title: `${data.stats.routes} routes` },
                ...(data.svcs ?? []).map((s) => ({
                  value: s.n,
                  label: s.n,
                  title: `${s.routes} routes · ${s.rpcs} rpc · ${s.e}`,
                })),
              ]}
            />
          </div>
        )}
        <div className="flex items-center gap-1.5">
          <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">level</span>
          <Segmented
            value={level}
            onChange={go}
            options={[
              { value: 'router', label: 'router' },
              { value: 'route', label: 'route' },
              { value: 'step', label: 'step' },
            ]}
          />
        </div>
        {scope.services > 0 && (
          <div className="flex items-center gap-1.5">
            <span className="text-faint text-[9.5px] tracking-[0.14em] uppercase">surface</span>
            <Segmented
              value={parts[0] === 'rpc' ? 'rpc' : 'http'}
              onChange={(v) => goto(v === 'rpc' ? '/rpc' : '/')}
              options={[
                { value: 'http', label: 'http', title: `${scope.routes} routes` },
                {
                  value: 'rpc',
                  label: `rpc ${scope.rpcs}`,
                  title: `${scope.services} services · ${scope.rpcs} methods`,
                },
              ]}
            />
          </div>
        )}
        <Button variant="default" size="sm" onClick={onSearch}>
          <Search className="size-3" /> search
          <kbd className="border-line text-faint ml-1 rounded-[3px] border px-1 text-[9.5px]">
            ⌘K
          </kbd>
        </Button>
        {blind > 0 && (
          <AppLink
            href="/diag"
            className="border-line text-faint hover:border-warn/45 hover:text-ink flex items-center gap-1 rounded-[3px] border px-1.5 py-0.5 text-[10px] transition-colors"
            title="files the loader skipped and in-module calls it could not resolve"
          >
            <b className="text-warn font-semibold">{blind}</b> blind
            {blind === 1 ? ' spot' : ' spots'}
          </AppLink>
        )}
      </div>
    </header>
  )
}
