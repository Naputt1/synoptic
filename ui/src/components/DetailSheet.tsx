import { GoSource } from '@/components/GoSource'
import { Badge } from '@/components/ui/badge'
import { Sheet } from '@/components/ui/sheet'
import { useData } from '@/lib/context'
import { touchTags } from '@/lib/data'
import type { Call, Stage } from '@/lib/types'
import { Inputs } from '@/panes/DocPanel'

import { CallsList } from './CallsList'
export function DetailSheet({
  open,
  onOpenChange,
  title,
  subtitle,
  doc,
  src,
  calls,
  stage,
  inputs,
  focus,
  focusLines,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  title: string
  subtitle?: string
  doc?: string
  src?: string
  calls?: Call[]
  stage?: Stage
  inputs?: Stage['in']
  focus?: number
  focusLines?: number[]
}) {
  const data = useData()
  return (
    <Sheet open={open} onOpenChange={onOpenChange} title={title} subtitle={subtitle}>
      {doc && <p className="text-ink/85 text-[12px] leading-[1.6]">{doc}</p>}
      {stage && (
        <div className="mt-3 flex flex-wrap gap-1">
          <Badge>{stage.r ?? stage.k}</Badge>
          {stage.x && <Badge variant="info">outside this repo</Badge>}
          {stage.chg && <Badge variant="warn">changed</Badge>}
          {touchTags(stage).map((t) => (
            <Badge key={t} variant={t === 'db' ? 'warn' : 'default'}>
              {t}
            </Badge>
          ))}
        </div>
      )}
      {inputs && inputs.length > 0 && <Inputs items={inputs} />}
      {calls && calls.length > 0 && <CallsList calls={calls} />}
      {src ? (
        <div className="mt-4">
          <div className="text-faint mb-1.5 text-[9.5px] tracking-[0.14em] uppercase">source</div>
          <GoSource code={src} focus={focus} focusLines={focusLines} />
        </div>
      ) : data.redact ? (
        <p className="text-faint mt-4 text-[11px] italic">
          source omitted (built with -no-source) — the flow, docs and line numbers are still here.
        </p>
      ) : (
        <p className="text-faint mt-4 text-[11px] italic">
          no body in this repo — the call is wired here, the code lives in{' '}
          {stage?.x ?? 'another module'}
        </p>
      )}
    </Sheet>
  )
}
