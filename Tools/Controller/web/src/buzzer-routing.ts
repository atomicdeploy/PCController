export type BuzzerPath = 'board' | 'host' | 'both' | 'none'

export function buzzerPathFromState(boardSilent: boolean, hostSilent: boolean): BuzzerPath {
  if (!boardSilent && !hostSilent) return 'both'
  if (!boardSilent) return 'board'
  if (!hostSilent) return 'host'
  return 'none'
}
