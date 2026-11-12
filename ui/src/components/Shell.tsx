import { Outlet } from '@tanstack/react-router'
import { useEffect, useState } from 'react'

import { SearchDialog } from '@/components/ui/command'
import { SvcCtx, useData } from '@/lib/context'

import { Crumbs } from './Crumbs'
import { Topbar } from './Topbar'

export function Shell() {
  const data = useData()
  const [searchOpen, setSearchOpen] = useState(false)
  const [svc, setSvc] = useState('')
  const [toast, setToast] = useState<{ id: number; text: string; tone: 'ok' | 'bad' } | null>(null)

  useEffect(() => {
    let hide = 0
    const onToast = (e: Event) => {
      const detail = (e as CustomEvent<{ message: string; tone?: 'ok' | 'bad' }>).detail
      setToast({ id: Date.now(), text: detail.message, tone: detail.tone ?? 'ok' })
      window.clearTimeout(hide)
      hide = window.setTimeout(() => setToast(null), 1800)
    }
    const onSearch = () => setSearchOpen(true)
    window.addEventListener('synoptic:toast', onToast)
    window.addEventListener('synoptic:search', onSearch)
    return () => {
      window.clearTimeout(hide)
      window.removeEventListener('synoptic:toast', onToast)
      window.removeEventListener('synoptic:search', onSearch)
    }
  }, [])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const typing = (e.target as HTMLElement | null)?.closest('input,textarea')
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setSearchOpen((v) => !v)
        return
      }
      if (
        document.querySelector(
          '[data-slot="context-menu-content"], [data-slot="search-dialog"], [role="dialog"]',
        )
      ) {
        if (e.key === 'Escape' && searchOpen) {
          e.preventDefault()
          setSearchOpen(false)
        }
        return
      }
      if (typing) return
      if (e.key === '/') {
        e.preventDefault()
        setSearchOpen(true)
      } else if (e.key === 'Escape') {
        window.history.back()
      }
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [searchOpen])

  return (
    <SvcCtx.Provider value={{ svc, setSvc }}>
      <div className="bg-canvas flex h-full flex-col">
        <Topbar onSearch={() => setSearchOpen(true)} />
        <Crumbs />
        <div className="relative min-h-0 flex-1">
          <Outlet />
        </div>
        <div className="border-line text-faint shrink-0 border-t px-4 py-1 text-[10px] leading-4">
          click any box to open it · right-click for actions · esc goes back · ⌘K searches
        </div>
        {toast && (
          <div
            key={toast.id}
            role="status"
            aria-live="polite"
            className={`toast-in pointer-events-none fixed inset-x-0 bottom-9 z-[70] mx-auto w-fit rounded-[5px] border px-3 py-1.5 text-[11px] shadow-[0_12px_32px_-12px_rgba(0,0,0,0.9)] ${
              toast.tone === 'bad'
                ? 'border-bad/45 bg-bad/10 text-bad'
                : 'border-line bg-panel2 text-ink'
            }`}
          >
            {toast.text}
          </div>
        )}
        <SearchDialog data={data} open={searchOpen} onOpenChange={setSearchOpen} svc={svc} />
      </div>
    </SvcCtx.Provider>
  )
}
