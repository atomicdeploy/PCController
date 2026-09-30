import { formatByteProgress } from './byte-format'
import type { ControllerEvent, Locale, ToastMessage } from './types'

const terminalStates = new Set(['completed', 'failed', 'cancelled', 'downloaded', 'staged', 'outcome-uncertain'])

function operationLabel(kind: string, locale: Locale): string {
  const labels: Record<string, [string, string]> = {
    host: ['Host update', 'به‌روزرسانی میزبان'],
    firmware: ['Firmware update', 'به‌روزرسانی میان‌افزار'],
    eeprom: ['EEPROM update', 'به‌روزرسانی EEPROM'],
    'artifact-fetch': ['Artifact download', 'دریافت فایل'],
    'artifact-upload': ['Artifact upload', 'ارسال فایل'],
    'peer-artifact-upload': ['Incoming host update', 'دریافت به‌روزرسانی میزبان'],
    'device-capture': ['Board readback', 'بازخوانی برد'],
  }
  return labels[kind]?.[locale === 'fa' ? 1 : 0] ?? kind.replaceAll('-', ' ')
}

export function updateToastFromEvent(
  event: Pick<ControllerEvent, 'kind' | 'text' | 'metadata'>,
  locale: Locale,
): Omit<ToastMessage, 'id'> | null {
  const eventKind = event.kind.trim().toLowerCase()
  if (!/^(update|peer-update)\./.test(eventKind)) return null
  const metadata = event.metadata
  const operationID = metadata?.operation_id?.trim()
  if (!operationID) return null
  const state = (metadata?.state || eventKind.split('.').slice(1).join('.')).trim().toLowerCase()
  const operationKind = (metadata?.kind || (eventKind.startsWith('peer-update.') ? 'host' : 'update')).trim().toLowerCase()
  const stage = (metadata?.stage || state).replaceAll('-', ' ')
  const percentValue = Number(metadata?.progress_percent)
  const progressKnown = metadata?.progress_known === 'true' && Number.isFinite(percentValue)
  const progressPercent = progressKnown ? Math.max(0, Math.min(100, percentValue)) : undefined
  const doneValue = Number(metadata?.bytes_done)
  const totalValue = Number(metadata?.bytes_total)
  const bytesDone = Number.isFinite(doneValue) && doneValue >= 0 ? doneValue : undefined
  const bytesTotal = Number.isFinite(totalValue) && totalValue > 0 ? totalValue : undefined
  const terminal = terminalStates.has(state)
  const failed = state === 'failed'
  const warning = state === 'cancelled' || state === 'outcome-uncertain'
  return {
    tone: failed ? 'danger' : warning ? 'warning' : terminal ? 'success' : 'info',
    title: `${operationLabel(operationKind, locale)} · ${stage}`,
    detail: event.text || metadata?.detail,
    persistent: !terminal,
    updateOperationID: operationID,
    progressKnown,
    progressPercent,
    bytesDone,
    bytesTotal,
    progressLabel: bytesTotal !== undefined
      ? formatByteProgress(bytesDone ?? 0, bytesTotal)
      : progressKnown ? `${progressPercent}%` : (locale === 'fa' ? 'در حال انتقال' : 'Transfer in progress'),
  }
}
