import type { PeripheralControlDescriptor, PeripheralPresentation } from './types'

/** Builds the complete normalized replacement payload committed on blur/Enter. */
export function peripheralPresentationPayload(
  controls: readonly PeripheralControlDescriptor[],
): Record<string, PeripheralPresentation> {
  return Object.fromEntries(controls.map((control, order) => [control.key, {
    name: control.name.trim() || control.default_name,
    description: control.description.trim() || control.default_description,
    order,
  }]))
}

/** Reorders one descriptor within its control kind; cross-kind drops are inert. */
export function movePeripheralControl(
  controls: readonly PeripheralControlDescriptor[],
  sourceKey: string,
  targetKey: string,
): PeripheralControlDescriptor[] {
  const sourceIndex = controls.findIndex((control) => control.key === sourceKey)
  const targetIndex = controls.findIndex((control) => control.key === targetKey)
  const source = controls[sourceIndex]
  const target = controls[targetIndex]
  if (!source || !target || source.key === target.key || source.kind !== target.kind) return [...controls]
  const reordered = [...controls]
  reordered.splice(sourceIndex, 1)
  reordered.splice(targetIndex, 0, source)
  return reordered
}
