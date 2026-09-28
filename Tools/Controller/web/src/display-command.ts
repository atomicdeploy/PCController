import { shellArgument } from './command-line'

export type DisplayTarget = 'segments' | 'lcd' | 'both'
export type DisplayRepeat = 'once' | 'loop' | 'interval'

export interface DisplayCommandOptions {
  target: DisplayTarget
  text: string
  speedMS: number
  durationMS: number
  repeat: DisplayRepeat
  intervalMS: number
  scroll: boolean
}

export function displayPresentationCommand(options: DisplayCommandOptions): string {
  const parts = [
    'display',
    options.target,
    '--speed', `${options.speedMS}ms`,
    '--duration', `${options.durationMS}ms`,
    '--repeat', options.repeat,
  ]
  if (options.repeat === 'interval') parts.push('--interval', `${options.intervalMS}ms`)
  if (options.scroll && options.target !== 'lcd') parts.push('--scroll')
  parts.push('--', shellArgument(options.text))
  return parts.join(' ')
}
