/**
 * Pointer Events are the modern, cross-input equivalent of touchstart.  A
 * regular control may activate on touch-down, while a control marked `hold`
 * keeps its release-sensitive safety semantics.
 */
export const touchActivationGraceMS = 800

export class TouchActivationGate<T extends object> {
  private readonly suppressUntil = new WeakMap<T, number>()

  begin(target: T, pointerType: string, isPrimary: boolean, now = Date.now()): boolean {
    if (pointerType !== 'touch' || !isPrimary) return false
    this.suppressUntil.set(target, now + touchActivationGraceMS)
    return true
  }

  shouldSuppressNativeClick(target: T, detail: number, now = Date.now()): boolean {
    if (detail === 0) return false // Keyboard and programmatic activation remain intact.
    const deadline = this.suppressUntil.get(target) ?? 0
    if (now > deadline) return false
    this.suppressUntil.delete(target)
    return true
  }
}

function actionTarget(target: EventTarget | null): HTMLElement | null {
  if (!(target instanceof Element)) return null
  const direct = target.closest<HTMLElement>('button, [role="button"], [role="switch"], input[type="checkbox"]')
  if (direct) return direct
  const label = target.closest('label')
  return label?.querySelector<HTMLElement>('input[type="checkbox"]') ?? null
}

function eligible(target: HTMLElement): boolean {
  if (target.matches(':disabled, [aria-disabled="true"]')) return false
  return !target.closest('[data-touch-mode="hold"], [data-touch-mode="deferred"]')
}

/**
 * Enables instant press for ordinary touch controls without changing keyboard
 * semantics or double-firing the delayed compatibility click.  The listener
 * deliberately avoids motion/hold controls, which must start on press and
 * stop on release.
 */
export function installImmediateTouchActivation(root: Document = document): () => void {
  const gate = new TouchActivationGate<HTMLElement>()
  const activating = new WeakSet<HTMLElement>()

  const onPointerDown = (event: PointerEvent) => {
    const target = actionTarget(event.target)
    if (!target || !eligible(target) || !gate.begin(target, event.pointerType, event.isPrimary)) return

    event.preventDefault()
    try { target.focus({ preventScroll: true }) } catch { target.focus() }
    activating.add(target)
    target.click()
    activating.delete(target)
  }

  const onClick = (event: MouseEvent) => {
    const target = actionTarget(event.target)
    if (!target || activating.has(target)) return
    if (!gate.shouldSuppressNativeClick(target, event.detail)) return
    event.preventDefault()
    event.stopImmediatePropagation()
  }

  root.addEventListener('pointerdown', onPointerDown, true)
  root.addEventListener('click', onClick, true)
  return () => {
    root.removeEventListener('pointerdown', onPointerDown, true)
    root.removeEventListener('click', onClick, true)
  }
}
