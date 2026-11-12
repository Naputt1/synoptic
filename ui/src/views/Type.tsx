import { useSearch } from '@tanstack/react-router'

import { ScopeCard } from '@/components/ScopeCard'
import { AppLink } from '@/components/ui/link'
import { useData } from '@/lib/context'
import { numParam } from '@/lib/params'
import type { SynType } from '@/lib/types'

export function TypeView() {
  const data = useData()
  const search = useSearch({ strict: false }) as {
    k?: string
    i?: string
    ri?: string
    si?: string
  }
  const key = search.k ?? ''
  const t: SynType | undefined = data.types?.[key]
  if (!t) return <div className="text-faint p-6 text-[12px]">this type is not in the module</div>
  const iface = search.i ? data.types?.[search.i] : undefined
  const ri = numParam(search.ri)
  const si = numParam(search.si)
  const origin =
    ri !== undefined && data.routes[ri] ? `&ri=${ri}${si !== undefined ? `&si=${si}` : ''}` : ''
  const methodHref = (mk?: string) => {
    if (!mk) return
    const sk = t.k === 'struct' ? t.key : undefined
    const ik = t.k === 'interface' ? t.key : iface?.key
    const q = [`k=${encodeURIComponent(mk)}`]
    if (ik) q.push(`i=${encodeURIComponent(ik)}`)
    if (sk) q.push(`t=${encodeURIComponent(sk)}`)
    return `/f?${q.join('&')}${origin}`
  }
  // resolve a field's written type back to a catalog entry, so fields with a
  // struct or interface type can be opened like methods
  const fieldType = (raw: string): SynType | undefined => {
    const norm = raw.replace(/^\*+/, '').trim()
    const base = norm.match(/([A-Za-z_]\w*)$/)?.[1]
    if (!base) return undefined
    const qual = norm.includes('.') ? norm.slice(0, norm.lastIndexOf('.')).split('.').pop() : ''
    if (qual) {
      return Object.values(data.types ?? {}).find(
        (x) => x.n === base && (x.pkg === qual || x.pkg.endsWith('/' + qual)),
      )
    }
    const same = data.types?.[`${t.pkg}|${base}`]
    if (same) return same
    const matches = Object.values(data.types ?? {}).filter((x) => x.n === base)
    return matches.length === 1 ? matches[0] : undefined
  }
  return (
    <div className="flex h-full flex-col overflow-auto">
      <ScopeCard
        title={t.n}
        badge={t.k}
        meta={t.f ? `${t.f}:${t.l ?? 1}` : t.pkg}
        note={
          t.k === 'interface'
            ? 'interface · implemented by the structs below'
            : 'struct · fields and methods'
        }
      />
      <div className="grid gap-3 p-4 lg:grid-cols-2">
        {t.k === 'struct' && (
          <section className="border-line bg-panel/40 rounded-[6px] border">
            <div className="border-line text-mut border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
              fields
            </div>
            <div className="divide-line/60 divide-y">
              {(t.fields ?? []).length === 0 && (
                <div className="text-faint px-3 py-2 text-[11px]">no fields</div>
              )}
              {(t.fields ?? []).map((f) => {
                const ft = fieldType(f.t)
                const href = ft ? `/t?k=${encodeURIComponent(ft.key)}${origin}` : undefined
                const body = (
                  <>
                    <span className="text-ink">{f.n}</span>
                    <span className="text-info truncate">{f.t}</span>
                    {f.e && (
                      <span className="text-faint text-[9px] tracking-[0.1em] uppercase">
                        embedded
                      </span>
                    )}
                    {ft && <span className="text-faint ml-auto shrink-0 text-[9px]">open ›</span>}
                  </>
                )
                const cls = `flex w-full items-baseline gap-2 px-3 py-1.5 text-left text-[11px] ${ft ? 'hover:bg-panel2' : ''}`
                return href ? (
                  <AppLink key={f.n + f.t} href={href} className={cls}>
                    {body}
                  </AppLink>
                ) : (
                  <div key={f.n + f.t} className={cls}>
                    {body}
                  </div>
                )
              })}
            </div>
          </section>
        )}
        <section className="border-line bg-panel/40 rounded-[6px] border">
          <div className="border-line text-mut border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
            methods
          </div>
          <div className="divide-line/60 divide-y">
            {(t.methods ?? []).length === 0 && (
              <div className="text-faint px-3 py-2 text-[11px]">no methods</div>
            )}
            {(t.methods ?? []).map((m) => {
              // flows are baked from routes, so a method in the catalog does
              // not always have one. Linking it would land on a dead page.
              const traced = Boolean(m.k && data.flows[m.k])
              const href = traced ? methodHref(m.k) : undefined
              const body = (
                <>
                  <span className="text-ink">{m.n}</span>
                  <span className="text-mut min-w-0 flex-1 truncate">{m.sig}</span>
                  {traced && <span className="text-faint shrink-0 text-[9px]">open ›</span>}
                </>
              )
              const cls = `flex w-full items-baseline gap-2 px-3 py-1.5 text-left text-[11px] ${traced ? 'hover:bg-panel2' : ''}`
              return href ? (
                <AppLink key={m.n + (m.k ?? '')} href={href} className={cls}>
                  {body}
                </AppLink>
              ) : (
                <div key={m.n + (m.k ?? '')} className={cls}>
                  {body}
                </div>
              )
            })}
          </div>
        </section>
        {t.k === 'interface' && (
          <section className="border-line bg-panel/40 rounded-[6px] border">
            <div className="border-line text-mut border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
              implemented by
            </div>
            <div className="divide-line/60 divide-y">
              {(t.impls ?? []).length === 0 && (
                <div className="text-faint px-3 py-2 text-[11px]">no in-module implementation</div>
              )}
              {(t.impls ?? []).map((ik) => {
                const it = data.types?.[ik]
                if (!it) return null
                return (
                  <AppLink
                    key={ik}
                    href={`/t?k=${encodeURIComponent(ik)}&i=${encodeURIComponent(t.key)}${origin}`}
                    className="hover:bg-panel2 flex w-full items-baseline gap-2 px-3 py-1.5 text-left text-[11px]"
                  >
                    <span className="text-ink">{it.n}</span>
                    <span className="text-mut min-w-0 flex-1 truncate">{it.pkg}</span>
                    <span className="text-faint text-[9px]">open ›</span>
                  </AppLink>
                )
              })}
            </div>
          </section>
        )}
        {t.k === 'struct' && (t.impls ?? []).length > 0 && (
          <section className="border-line bg-panel/40 rounded-[6px] border">
            <div className="border-line text-mut border-b px-3 py-1.5 text-[10px] tracking-[0.14em] uppercase">
              implements
            </div>
            <div className="divide-line/60 divide-y">
              {(t.impls ?? []).map((ik) => {
                const it = data.types?.[ik]
                if (!it) return null
                return (
                  <AppLink
                    key={ik}
                    href={`/t?k=${encodeURIComponent(ik)}${origin}`}
                    className="hover:bg-panel2 flex w-full items-baseline gap-2 px-3 py-1.5 text-left text-[11px]"
                  >
                    <span className="text-ink">{it.n}</span>
                    <span className="text-mut min-w-0 flex-1 truncate">{it.pkg}</span>
                  </AppLink>
                )
              })}
            </div>
          </section>
        )}
      </div>
    </div>
  )
}
