import { cva, type VariantProps } from 'class-variance-authority'
import type { ButtonHTMLAttributes } from 'react'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-[5px] text-[12px] transition-colors disabled:pointer-events-none disabled:opacity-40 outline-none focus-visible:ring-1 focus-visible:ring-acc/60',
  {
    variants: {
      variant: {
        default: 'border border-line bg-panel text-ink hover:border-line2 hover:bg-panel2',
        ghost: 'border border-transparent text-mut hover:bg-panel hover:text-ink',
        accent: 'border border-acc/45 bg-acc/10 text-acc hover:bg-acc/15',
        outline: 'border border-line2 bg-transparent text-ink hover:bg-panel',
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

export type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & VariantProps<typeof buttonVariants>

export function Button({ className, variant, size, ...props }: ButtonProps) {
  return <button className={cn(buttonVariants({ variant, size }), className)} {...props} />
}
