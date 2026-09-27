import { useMemo } from 'react'
import { Activity } from 'lucide-react'
import { EmptyState } from './components'
import { formatClock, type MessageKey } from './i18n'
import { StructuredValue, TypedCollection } from './typed-collection'
import type { CollectionFieldDefinition } from './typed-collection-model'
import type { ControllerEvent, Locale } from './types'

function eventTone(event: ControllerEvent): 'good' | 'warn' | 'bad' | 'info' {
  const kind = event.kind.toLowerCase()
  if (kind.includes('error') || kind.includes('fault') || kind.includes('hot')) return 'bad'
  if (kind.includes('warning') || kind.includes('door') || kind.includes('disconnect')) return 'warn'
  if (kind.includes('connect') || kind.includes('ready') || kind.includes('complete')) return 'good'
  return 'info'
}

function eventMessage(event: ControllerEvent): string | undefined {
  return event.text || event.reason || event.state
}

export function EventList({
  events,
  locale,
  t,
  limit = 6,
  toolbar = false,
}: {
  events: ControllerEvent[]
  locale: Locale
  t: (key: MessageKey) => string
  limit?: number
  toolbar?: boolean
}) {
  const copy = (english: string, persian: string) => locale === 'fa' ? persian : english
  const fields = useMemo<CollectionFieldDefinition<ControllerEvent>[]>(() => [
    { id: 'time', icon: 'time', label: copy('Time', 'زمان'), get: (event) => event.time, width: 142 },
    { id: 'kind', icon: 'type', label: copy('Type', 'نوع'), get: (event) => event.kind, width: 165 },
    { id: 'message', icon: 'message', label: copy('Message', 'پیام'), get: eventMessage, width: 330 },
    { id: 'source', icon: 'source', label: copy('Source', 'مبدأ'), get: (event) => event.source, width: 170, visible: toolbar },
    { id: 'metadata', icon: 'metadata', label: copy('Metadata', 'فراداده'), get: (event) => event.metadata, width: 270, visible: toolbar },
  ], [locale, toolbar])
  const visible = events.slice(0, limit)
  return (
    <TypedCollection
      records={visible}
      fields={fields}
      locale={locale}
      direction={locale === 'fa' ? 'rtl' : 'ltr'}
      ariaLabel={copy('Controller event collection', 'مجموعهٔ رویدادهای کنترلر')}
      filename="controller-events"
      toolbar={toolbar}
      windowSize={120}
      preserveViewportAnchor
      className={`typed-collection--events${toolbar ? '' : ' typed-collection--compact'}`}
      rowKey={(event) => `${event.id}:${event.time}`}
      rowClassName={(event) => `typed-collection__row--${eventTone(event)}`}
      renderCell={(field, value, event) => {
        if (field.id === 'time') return <time dateTime={event.time}>{formatClock(locale, event.time)}</time>
        if (field.id === 'kind') return <strong className="typed-collection__event-kind">{event.kind}</strong>
        if (field.id === 'message') return <div className="typed-collection__event-copy"><span className="typed-collection__event-message" dir="auto">{eventMessage(event) || '—'}</span>{!toolbar && event.metadata && Object.keys(event.metadata).length > 0 && <StructuredValue value={event.metadata} label={copy('Expand event metadata', 'نمایش فرادادهٔ رویداد')} />}</div>
        if (field.id === 'metadata' && event.metadata && Object.keys(event.metadata).length) {
          return <StructuredValue value={event.metadata} label={copy('Expand event metadata', 'نمایش فرادادهٔ رویداد')} />
        }
        if (value === undefined) return <span aria-hidden="true">—</span>
        return undefined
      }}
      empty={<EmptyState icon={Activity} title={t('noEvents')} detail={t('eventStream')} />}
    />
  )
}
