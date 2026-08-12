const quietCommandFamilies = new Set([
  'beep', 'buzzer', 'display', 'help', 'lcd', 'melody', 'menu', 'motion',
  'pwm', 'relay', 'rgb', 'side', 'status', 'strip',
])

/** Live controls already provide local/pushed feedback; success toasts add noise. */
export function commandSuccessShouldToast(command: string, explicitSuccess?: string): boolean {
  if (explicitSuccess?.trim()) return true
  const family = command.trim().split(/\s+/, 1)[0]?.toLowerCase() ?? ''
  return !quietCommandFamilies.has(family)
}
