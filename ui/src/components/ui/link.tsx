import { Link } from '@tanstack/react-router'
import type { AnchorHTMLAttributes } from 'react'

type AppLinkProps = AnchorHTMLAttributes<HTMLAnchorElement> & { href: string }

// TanStack's types still require `to` alongside `href`. At runtime `href` wins
// and is parsed into the location, so pass it as both to keep the anchor real.
export function AppLink({ href, ...props }: AppLinkProps) {
  return <Link to={href} href={href} {...props} />
}
