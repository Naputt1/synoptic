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
        <Dialog.Content className="sheet-content border-line bg-canvas fixed top-0 right-0 z-50 flex h-full w-full max-w-[min(760px,92vw)] flex-col border-l shadow-[-24px_0_60px_-40px_rgba(0,0,0,0.9)] outline-none">
          <div className="border-line flex items-start gap-3 border-b px-4 py-3">
            <div className="min-w-0 flex-1">
              <Dialog.Title className="text-ink truncate text-[13px]">{title}</Dialog.Title>
              {subtitle && <div className="text-mut mt-0.5 truncate text-[11px]">{subtitle}</div>}
            </div>
            <Dialog.Close asChild>
              <button
                className="border-line text-mut hover:text-ink rounded-[4px] border p-1"
                aria-label="close"
              >
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
