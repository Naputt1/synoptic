import { Toggle } from '@base-ui/react/toggle'
import { ToggleGroup } from '@base-ui/react/toggle-group'

import { cn } from '@/lib/utils'

type Option = { value: string; label: string; title?: string }

export function Segmented({
  value,
  options,
  onChange,
  className,
}: {
  value: string
  options: Option[]
  onChange: (v: string) => void
  className?: string
}) {
  return (
    <ToggleGroup
      value={[value]}
      onValueChange={(v) => {
        if (v[0]) onChange(v[0])
      }}
      className={cn('border-line flex overflow-hidden rounded-[5px] border', className)}
    >
      {options.map((o) => (
        <Toggle
          key={o.value}
          value={o.value}
          title={o.title}
          className="text-mut hover:text-ink data-[pressed]:bg-acc/15 data-[pressed]:text-acc focus-visible:text-ink px-2 py-[3px] text-[11px] transition-colors outline-none data-[pressed]:shadow-[inset_0_0_0_1px_rgba(88,224,183,0.28)]"
        >
          {o.label}
        </Toggle>
      ))}
    </ToggleGroup>
  )
}
