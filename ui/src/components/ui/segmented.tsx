import * as ToggleGroup from '@radix-ui/react-toggle-group'

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
    <ToggleGroup.Root
      type="single"
      value={value}
      onValueChange={(v) => {
        if (v) onChange(v)
      }}
      className={cn('border-line flex overflow-hidden rounded-[5px] border', className)}
    >
      {options.map((o) => (
        <ToggleGroup.Item
          key={o.value}
          value={o.value}
          title={o.title}
          className="text-mut hover:text-ink data-[state=on]:bg-acc/15 data-[state=on]:text-acc focus-visible:text-ink px-2 py-[3px] text-[11px] transition-colors outline-none data-[state=on]:shadow-[inset_0_0_0_1px_rgba(88,224,183,0.28)]"
        >
          {o.label}
        </ToggleGroup.Item>
      ))}
    </ToggleGroup.Root>
  )
}
