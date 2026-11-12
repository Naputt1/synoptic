export function RpcCell({ label, value }: { label: string; value?: string }) {
  if (!value) return null
  return (
    <div className="min-w-0">
      <div className="text-faint text-[9.5px] tracking-[0.14em] uppercase">{label}</div>
      <div className="text-ink mt-0.5 truncate text-[11.5px]" title={value}>
        {value}
      </div>
    </div>
  )
}
