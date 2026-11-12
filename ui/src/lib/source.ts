import { useCallback, useEffect, useState } from 'react'

/** Opens the source sheet from a `synoptic:source` event dispatched by the graph. */
export function useSourceSheet() {
  const [sheet, setSheet] = useState(false)
  const [focus, setFocus] = useState<{ line?: number; lines?: number[] }>({})
  useEffect(() => {
    const onSrc = (e: Event) => {
      const detail = (e as CustomEvent<{ line: number; startLine?: number; lines?: number[] }>)
        .detail
      const offset = (detail.startLine ?? 1) - 1
      const lines = detail.lines?.map((l) => l - offset)
      setFocus({ line: lines?.[0] ?? detail.line - offset, lines })
      setSheet(true)
    }
    window.addEventListener('synoptic:source', onSrc)
    return () => window.removeEventListener('synoptic:source', onSrc)
  }, [])
  const openSource = useCallback(() => {
    setFocus({})
    setSheet(true)
  }, [])
  return { sheet, setSheet, focus, openSource }
}
