// Search params are strings, and an absent value must not become stage 0.
export const numParam = (v?: string): number | undefined => {
  if (v === undefined || v === '') return undefined
  const n = Number(v)
  return Number.isInteger(n) ? n : undefined
}
