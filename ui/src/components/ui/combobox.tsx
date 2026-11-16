import { Autocomplete } from '@base-ui/react/autocomplete'
import { Check, ChevronDown } from 'lucide-react'
import { useRef } from 'react'
import type { KeyboardEvent } from 'react'

import { cn } from '@/lib/utils'

export type ComboboxOption = {
  value: string
  label: string
  kind?: string
  detail?: string
}

/**
 * A shadcn-style combobox: a bordered input with a suggestion list. The input
 * keeps free-form text so refs such as `HEAD~1` still work, while branches,
 * tags and recent commits are one click away.
 */
export function Combobox({
  value,
  onValueChange,
  options,
  placeholder,
  ariaLabel,
  disabled,
  className,
  onKeyDown,
  empty = 'no matches',
}: {
  value: string
  onValueChange: (value: string) => void
  options: ComboboxOption[]
  placeholder?: string
  ariaLabel: string
  disabled?: boolean
  className?: string
  onKeyDown?: (event: KeyboardEvent<HTMLInputElement>) => void
  empty?: string
}) {
  const typing = useRef(false)

  return (
    <Autocomplete.Root
      items={options}
      value={value}
      onValueChange={(next, details) => {
        typing.current = details.reason === 'input-change' || details.reason === 'input-clear'
        onValueChange(next)
      }}
      itemToStringValue={(option) => option.value}
      onOpenChange={(open) => {
        if (!open) typing.current = false
      }}
      filter={(option, query) => {
        // Opening a picker that already holds a ref shows the full list. Only
        // a query the user is actively typing filters the options.
        if (!typing.current) return true
        const needle = query.trim().toLowerCase()
        if (!needle) return true
        return (
          option.value.toLowerCase().includes(needle) ||
          option.label.toLowerCase().includes(needle) ||
          option.detail?.toLowerCase().includes(needle) === true
        )
      }}
    >
      <Autocomplete.InputGroup
        className={cn(
          'border-line bg-canvas text-ink focus-within:border-line2 flex h-6 items-center rounded-[4px] border transition-colors',
          disabled && 'cursor-not-allowed opacity-40',
          className,
        )}
      >
        <Autocomplete.Input
          aria-label={ariaLabel}
          placeholder={placeholder}
          disabled={disabled}
          onKeyDown={onKeyDown}
          autoComplete="off"
          spellCheck={false}
          className="text-ink placeholder:text-faint h-full min-w-0 flex-1 bg-transparent px-2 text-[11px] outline-none disabled:cursor-not-allowed"
        />
        <Autocomplete.Trigger
          aria-label={`show ${ariaLabel} suggestions`}
          disabled={disabled}
          className="text-faint hover:text-ink focus-visible:ring-acc/60 mr-0.5 flex size-5 shrink-0 items-center justify-center rounded-[3px] outline-none focus-visible:ring-1 disabled:cursor-not-allowed"
        >
          <ChevronDown className="size-3" />
        </Autocomplete.Trigger>
      </Autocomplete.InputGroup>

      <Autocomplete.Portal>
        <Autocomplete.Positioner sideOffset={4} align="start" className="isolate z-50 outline-none">
          <Autocomplete.Popup className="border-line bg-panel text-ink w-[var(--anchor-width)] max-w-[min(440px,var(--available-width))] min-w-[260px] origin-[var(--transform-origin)] overflow-hidden rounded-[6px] border shadow-[0_22px_54px_-18px_rgba(0,0,0,0.9)] outline-none">
            <Autocomplete.Empty className="text-faint px-3 py-2 text-[11px]">
              {empty}
            </Autocomplete.Empty>
            <Autocomplete.List className="max-h-[min(320px,var(--available-height))] overflow-y-auto overscroll-contain py-1">
              {(option: ComboboxOption) => (
                <Autocomplete.Item
                  key={`${option.kind ?? 'ref'}:${option.value}`}
                  value={option}
                  className="data-highlighted:bg-panel2 data-highlighted:text-ink grid cursor-default grid-cols-[56px_minmax(0,1fr)_auto] items-center gap-2 px-2 py-1.5 text-[11px] outline-none"
                >
                  <span className="text-faint text-[9px] tracking-[0.12em] uppercase">
                    {option.kind ?? 'ref'}
                  </span>
                  <span className="text-mut min-w-0 truncate">{option.label}</span>
                  <span className="flex min-w-0 items-center gap-1.5">
                    {option.detail && (
                      <code className="text-faint max-w-[120px] truncate text-[10px]">
                        {option.detail}
                      </code>
                    )}
                    {option.value === value && <Check className="text-acc size-3 shrink-0" />}
                  </span>
                </Autocomplete.Item>
              )}
            </Autocomplete.List>
          </Autocomplete.Popup>
        </Autocomplete.Positioner>
      </Autocomplete.Portal>
    </Autocomplete.Root>
  )
}
