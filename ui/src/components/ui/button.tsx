import { cva, type VariantProps } from 'class-variance-authority'
import type { ButtonHTMLAttributes } from 'react'

import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'focus-visible:ring-acc/60 inline-flex items-center justify-center gap-1.5 rounded-[5px] text-[12px] whitespace-nowrap transition-colors outline-none focus-visible:ring-1 disabled:pointer-events-none disabled:opacity-40',
  {
    variants: {
      variant: {
        default: 'border-line bg-panel text-ink hover:border-line2 hover:bg-panel2 border',
        ghost: 'text-mut hover:bg-panel hover:text-ink border border-transparent',
        accent: 'border-acc/45 bg-acc/10 text-acc hover:bg-acc/15 border',
        outline: 'border-line2 text-ink hover:bg-panel border bg-transparent',
      },
      size: {
        default: 'h-7 px-2.5',
        sm: 'h-6 px-2 text-[11px]',
        icon: 'size-6',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  },
)

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> &
  VariantProps<typeof buttonVariants>

export function Button({ className, variant, size, ...props }: ButtonProps) {
  return <button className={cn(buttonVariants({ variant, size }), className)} {...props} />
}
