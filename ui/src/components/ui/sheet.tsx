import * as Dialog from '@radix-ui/react-dialog'
import { X } from 'lucide-react'
import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function Sheet({
  open,
  onOpenChange,
  title,
  subtitle,
  children,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  title: ReactNode
  subtitle?: ReactNode
  children: ReactNode
}) {
  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="sheet-overlay fixed inset-0 z-40 bg-black/55 backdrop-blur-[1px]" />
        <Dialog.Content className="sheet-content fixed right-0 top-0 z-50 flex h-full w-full max-w-[min(760px,92vw)] flex-col border-l border-line bg-canvas shadow-[-24px_0_60px_-40px_rgba(0,0,0,0.9)] outline-none">
          <div className="flex items-start gap-3 border-b border-line px-4 py-3">
            <div className="min-w-0 flex-1">
              <Dialog.Title className="truncate text-[13px] text-ink">{title}</Dialog.Title>
              {subtitle && <div className="mt-0.5 truncate text-[11px] text-mut">{subtitle}</div>}
            </div>
            <Dialog.Close asChild>
              <button className="rounded-[4px] border border-line p-1 text-mut hover:text-ink" aria-label="close">
                <X className="size-3.5" />
              </button>
            </Dialog.Close>
          </div>
          <div className={cn('min-h-0 flex-1 overflow-auto px-4 py-3')}>{children}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  )
}
