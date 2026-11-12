import { Badge } from '@/components/ui/badge'
import { Sheet } from '@/components/ui/sheet'
import { frameName } from '@/lib/data'
import type { StreamMessage } from '@/lib/types'

export function MessageSheet({
  message,
  event,
  open,
  onOpenChange,
}: {
  message?: StreamMessage
  event?: string
  open: boolean
  onOpenChange: (v: boolean) => void
}) {
  return (
    <Sheet
      open={open}
      onOpenChange={onOpenChange}
      title={message ? frameName(message) : 'frame'}
      subtitle={event}
    >
      {message && (
        <div>
          <div className="flex flex-wrap items-center gap-1.5">
            <Badge variant={message.d === 'recv' ? 'info' : 'accent'}>
              {message.d === 'recv' ? 'received' : 'sent'}
            </Badge>
            {message.t && <Badge>{message.t}</Badge>}
            {message.file && (
              <span className="text-faint ml-auto text-[10px]">
                {message.file}:{message.l ?? 0}
              </span>
            )}
          </div>
          {message.n && message.n !== frameName(message) && (
            <p className="text-faint mt-2 text-[10px]">name {message.n}</p>
          )}
          <div className="mt-4">
            <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">fields</div>
            {message.f?.length ? (
              <div className="divide-line/40 border-line/40 divide-y border-y">
                {message.f.map((f) => (
                  <div key={f.n} className="flex flex-wrap items-baseline gap-x-2 py-1.5">
                    <code className="text-ink text-[11px]">{f.n}</code>
                    {f.t && <span className="text-mut text-[10px]">{f.t}</span>}
                    {f.tg && <span className="text-faint text-[9.5px]">{f.tg}</span>}
                    {f.rq && (
                      <span className="text-warn text-[9px] tracking-[0.1em] uppercase">
                        required
                      </span>
                    )}
                  </div>
                ))}
              </div>
            ) : (
              <p className="text-faint text-[11px] italic">
                no fields resolved — only the source expression is known.
              </p>
            )}
          </div>
          {message.e && (
            <div className="mt-4">
              <div className="text-faint mb-1 text-[9.5px] tracking-[0.14em] uppercase">
                expression
              </div>
              <code className="text-mut block text-[10.5px] break-words">{message.e}</code>
            </div>
          )}
          {message.ex && (
            <pre className="bg-canvas/70 text-mut mt-3 overflow-x-auto rounded-[4px] p-2 text-[10px] leading-relaxed">
              {message.ex}
            </pre>
          )}
        </div>
      )}
    </Sheet>
  )
}
