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
      className={cn('flex overflow-hidden rounded-[5px] border border-line', className)}
    >
      {options.map((o) => (
        <ToggleGroup.Item
          key={o.value}
          value={o.value}
          title={o.title}
          className="px-2 py-[3px] text-[11px] text-mut outline-none transition-colors hover:text-ink data-[state=on]:bg-acc/15 data-[state=on]:text-acc data-[state=on]:shadow-[inset_0_0_0_1px_rgba(88,224,183,0.28)] focus-visible:text-ink"
        >
          {o.label}
        </ToggleGroup.Item>
      ))}
    </ToggleGroup.Root>
  )
}
