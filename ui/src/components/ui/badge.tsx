import { cva, type VariantProps } from 'class-variance-authority'
import type { HTMLAttributes } from 'react'

import { cn } from '@/lib/utils'

const badgeVariants = cva(
  'inline-flex items-center rounded-[3px] border px-1 py-px text-[9.5px] tracking-[0.1em] uppercase',
  {
    variants: {
      variant: {
        default: 'border-line text-mut',
        accent: 'border-acc/45 text-acc',
        warn: 'border-warn/45 text-warn',
        bad: 'border-bad/45 text-bad',
        info: 'border-info/45 text-info',
        plain: 'text-faint border-transparent',
      },
    },
    defaultVariants: { variant: 'default' },
  },
)

export type BadgeProps = HTMLAttributes<HTMLSpanElement> & VariantProps<typeof badgeVariants>

export function Badge({ className, variant, ...props }: BadgeProps) {
  return <span className={cn(badgeVariants({ variant }), className)} {...props} />
}
