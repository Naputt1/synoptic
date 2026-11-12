import { frameName } from '@/lib/data'
import type { StreamMessage } from '@/lib/types'

function fieldsOf(m: StreamMessage): string {
  const names = (m.f ?? []).map((f) => f.n)
  if (!names.length) return m.e ?? ''
  return names.slice(0, 4).join(', ') + (names.length > 4 ? ` +${names.length - 4}` : '')
}

export function MessageCard({
  m,
  event,
  onOpen,
}: {
  m: StreamMessage
  event?: string
  onOpen: () => void
}) {
  return (
    <button
      type="button"
      onClick={onOpen}
      className="border-line bg-panel hover:border-line2 hover:bg-panel2 block w-full rounded-[4px] border px-2 py-1.5 text-left transition-colors"
    >
      <div className="flex flex-wrap items-baseline gap-x-2">
        <code className="text-ink text-[11px] leading-tight">{m.ev || m.n || frameName(m)}</code>
        {m.t && <span className="text-faint text-[9px] break-all">{m.t}</span>}
      </div>
      <div className="text-mut mt-0.5 text-[9.5px] break-words">
        {fieldsOf(m) || (event ? m.n : 'shape unresolved')}
      </div>
    </button>
  )
}
