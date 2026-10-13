import type { ControlDescriptor } from './types'

/** Search reveals a stable channel; it never actuates it. */
export function searchChannels(controls: readonly ControlDescriptor[], query: string): ControlDescriptor[] {
  const term = query.trim().toLocaleLowerCase();
  if (!term) return [];
  return controls.filter(control => !control.hidden && /^(pwm\.\d+|relay\.\d+|seat\.[ab])$/.test(control.key)
    && `${control.name} ${control.key} ${control.group ?? ''} ${control.kind}`.toLocaleLowerCase().includes(term));
}
