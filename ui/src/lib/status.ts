export type StatusTone = 'ok' | 'info' | 'warn' | 'bad' | 'mut'

/** Tone of an HTTP status code, or a label that starts with one ("200 out"). */
export function statusTone(code?: string): StatusTone {
  const first = (code ?? '').trim().charAt(0)
  return first === '2'
    ? 'ok'
    : first === '3'
      ? 'info'
      : first === '4'
        ? 'warn'
        : first === '5'
          ? 'bad'
          : 'mut'
}

/** Every status a step can answer with, whether it is one code or several. */
export function codesOf(s: { cd?: string; cds?: string[] }): string[] {
  return s.cds?.length ? s.cds : s.cd ? [s.cd] : []
}

/** The codes as one badge label: "400 · 500". */
export function codesLabel(s: { cd?: string; cds?: string[] }): string {
  return codesOf(s).join(' · ')
}

/** Border and text for a small status-code badge, shared by docs and nodes. */
export const statusBadgeTone: Record<StatusTone, string> = {
  ok: 'border-acc/40 text-acc',
  info: 'border-info/40 text-info',
  warn: 'border-warn/45 text-warn',
  bad: 'border-bad/45 text-bad',
  mut: 'border-line text-mut',
}

/** Raw stroke color for each tone; edges draw with SVG rather than classes. */
export const statusColor: Record<StatusTone, string> = {
  ok: '#58e0b7',
  info: '#9fc6ff',
  warn: '#e0a33e',
  bad: '#e06a6a',
  mut: '#7d9c99',
}

/** Border and text for the label sitting on a return line. */
export const statusLineTone: Record<StatusTone, string> = {
  ok: 'border-acc/30 text-acc',
  info: 'border-info/30 text-info',
  warn: 'border-warn/30 text-warn',
  bad: 'border-bad/30 text-bad',
  mut: 'border-line2/70 text-mut',
}
