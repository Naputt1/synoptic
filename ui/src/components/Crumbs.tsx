import { useRouterState, useSearch } from '@tanstack/react-router'

import { Button } from '@/components/ui/button'
import { AppLink } from '@/components/ui/link'
import { useData, useSvc } from '@/lib/context'
import { routeHref, stageHref } from '@/lib/data'
import { numParam } from '@/lib/params'

type Crumb = { label: string; href?: string }

function useCrumbs(): Crumb[] {
  const data = useData()
  const pathname = useRouterState({ select: (s) => s.location.pathname })
  const search = useSearch({ strict: false }) as {
    k?: string
    i?: string
    t?: string
    ri?: string
    si?: string
  }
  const parts = pathname.split('/').filter(Boolean)
  const crumbs: Crumb[] = [{ label: data.repo, href: '/' }]

  const routeChain = (ri: number, si?: number, skipStage?: string) => {
    const r = data.routes[ri]
    if (!r) return
    const segs = r.p.split('/').filter(Boolean).slice(0, 2)
    segs.forEach((s, i) => crumbs.push({ label: s, href: '/g/' + segs.slice(0, i + 1).join('/') }))
    crumbs.push({ label: `${r.m} ${r.p}`, href: routeHref(ri) })
    if (si !== undefined) {
      const st = r.ch[si]
      const same =
        st && (st.n === skipStage || Boolean(skipStage && skipStage.endsWith('.' + st.n)))
      if (st && !same) {
        const fk = (st.fk ?? '').split('|')
        const self = fk.length >= 3 ? data.types?.[`${fk[0]}|${fk[1]}`] : undefined
        const origin = `&ri=${ri}&si=${si}`
        if (self) {
          const impl = self.impls?.length === 1 ? data.types?.[self.impls[0]] : undefined
          if (impl)
            crumbs.push({ label: impl.n, href: `/t?k=${encodeURIComponent(impl.key)}${origin}` })
          crumbs.push({ label: self.n, href: `/t?k=${encodeURIComponent(self.key)}${origin}` })
        }
        crumbs.push({ label: st.n, href: stageHref(ri, si) })
      }
    }
  }

  if (parts[0] === 'g') {
    const segs = parts.slice(1)
    segs.forEach((s, i) => crumbs.push({ label: s, href: '/g/' + segs.slice(0, i + 1).join('/') }))
  }

  if (parts[0] === 'r') {
    const ri = Number(parts[1])
    routeChain(ri, parts[2] === 's' ? Number(parts[3]) : undefined)
  }

  if (parts[0] === 't') {
    const key = search.k ?? ''
    const iface = search.i ? data.types?.[search.i] : undefined
    const self = data.types?.[key]
    const ri = numParam(search.ri)
    const si = numParam(search.si)
    const origin =
      ri !== undefined && data.routes[ri] ? `&ri=${ri}${si !== undefined ? `&si=${si}` : ''}` : ''
    if (ri !== undefined && data.routes[ri]) {
      routeChain(ri, si)
    }
    if (iface)
      crumbs.push({ label: iface.n, href: `/t?k=${encodeURIComponent(iface.key)}${origin}` })
    if (self)
      crumbs.push({
        label: self.n,
        href: `/t?k=${encodeURIComponent(self.key)}${search.i ? `&i=${encodeURIComponent(search.i)}` : ''}${origin}`,
      })
  }

  if (parts[0] === 'rpc') {
    const svc = data.services?.[Number(parts[1])]
    crumbs.push({ label: 'rpc', href: '/rpc' })
    if (svc) {
      const method = svc.methods[Number(parts[2])]
      crumbs.push({ label: svc.n, href: '/rpc' })
      if (method) crumbs.push({ label: method.n, href: '#' })
    }
  }

  if (parts[0] === 'f') {
    const f = data.flows[search.k ?? '']
    const iface = search.i ? data.types?.[search.i] : undefined
    const st = search.t ? data.types?.[search.t] : undefined
    // keep the route and stage that opened this function, so back navigation
    // has the whole chain instead of restarting at the repo
    const ri = numParam(search.ri)
    const si = numParam(search.si)
    const origin =
      ri !== undefined && data.routes[ri] ? `&ri=${ri}${si !== undefined ? `&si=${si}` : ''}` : ''
    if (ri !== undefined && data.routes[ri]) {
      routeChain(ri, si, f?.n)
    }
    if (iface)
      crumbs.push({ label: iface.n, href: `/t?k=${encodeURIComponent(iface.key)}${origin}` })
    if (st)
      crumbs.push({
        label: st.n,
        href: `/t?k=${encodeURIComponent(st.key)}${iface ? `&i=${encodeURIComponent(iface.key)}` : ''}${origin}`,
      })
    if (f && crumbs[crumbs.length - 1]?.label !== f.n) crumbs.push({ label: f.n, href: '#' })
  }

  return crumbs
}

export function Crumbs() {
  const crumbs = useCrumbs()
  const svc = useSvc()
  return (
    <div className="border-line text-mut flex flex-wrap items-center gap-x-1.5 gap-y-1 border-b px-4 py-1.5 text-[11px]">
      <Button
        variant="ghost"
        size="sm"
        className="mr-1 h-5 px-1.5"
        onClick={() => window.history.back()}
      >
        back
      </Button>
      {crumbs.map((c, i) => (
        <span key={i} className="flex items-center gap-1.5">
          {i > 0 && <span className="text-faint">›</span>}
          {c.href && i < crumbs.length - 1 ? (
            <AppLink href={c.href} className="hover:bg-panel hover:text-ink rounded-[3px] px-1">
              {c.label}
            </AppLink>
          ) : (
            <span className={i === crumbs.length - 1 ? 'text-ink' : ''}>{c.label}</span>
          )}
        </span>
      ))}
      {svc && (
        <span
          className="border-line text-faint ml-1 rounded-[3px] border px-1 py-px text-[10px]"
          title="routes and rpc services are filtered to this entry point"
        >
          service {svc}
        </span>
      )}
    </div>
  )
}
