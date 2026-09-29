import {
  type CSSProperties,
  type PointerEvent as ReactPointerEvent,
  type ReactNode,
} from 'react'
import { Check, ChevronDown, Eye, EyeOff, GripVertical, RotateCcw } from 'lucide-react'
import { Button } from './components'
import { keyboardReorderOffset } from './pointer-reorder'

export interface CardLayoutCopy {
  move: string
  collapse: string
  expand: string
  hide: string
  show: string
  customize: string
  done: string
  reset: string
  hidden: string
}

export interface LayoutCardDescriptor<CardID extends string> {
  id: CardID
  label: string
}

export function CardLayoutEditor<CardID extends string>({
  copy,
  editing,
  cards,
  hidden,
  onToggleEditing,
  onShow,
  onReset,
}: {
  copy: CardLayoutCopy
  editing: boolean
  cards: readonly LayoutCardDescriptor<CardID>[]
  hidden: readonly CardID[]
  onToggleEditing: () => void
  onShow: (id: CardID) => void
  onReset: () => void
}) {
  const hiddenCards = cards.filter(({ id }) => hidden.includes(id))
  return <>
    <Button compact className="card-layout-editor__toggle" icon={editing ? Check : GripVertical} aria-pressed={editing} onClick={onToggleEditing}>
      {editing ? copy.done : copy.customize}
    </Button>
    {editing && <div className="card-layout-editor" role="toolbar" aria-label={copy.customize}>
      {hiddenCards.length > 0 && <div className="card-layout-editor__hidden">
        <span>{copy.hidden}</span>
        {hiddenCards.map(({ id, label }) => <Button key={id} compact icon={Eye} onClick={() => onShow(id)}>{copy.show} {label}</Button>)}
      </div>}
      <Button compact tone="ghost" icon={RotateCcw} onClick={onReset}>{copy.reset}</Button>
    </div>}
  </>
}

export function CardLayoutFrame<CardID extends string>({
  id,
  title,
  order,
  collapsed,
  hidden,
  editing,
  dragging,
  copy,
  onToggleCollapsed,
  onHide,
  onReorderStart,
  onReorderMove,
  onReorderEnd,
  onKeyboardReorder,
  children,
}: {
  id: CardID
  title: string
  order: number
  collapsed: boolean
  hidden: boolean
  editing: boolean
  dragging: boolean
  copy: CardLayoutCopy
  onToggleCollapsed: () => void
  onHide: () => void
  onReorderStart: (id: CardID, x: number, y: number) => void
  onReorderMove: (id: CardID, x: number, y: number) => void
  onReorderEnd: () => void
  onKeyboardReorder: (id: CardID, offset: -1 | 1) => void
  children: ReactNode
}) {
  if (hidden) return null
  const beginReorder = (event: ReactPointerEvent<HTMLButtonElement>) => {
    if (event.button !== 0 || !event.isPrimary) return
    event.preventDefault()
    event.currentTarget.setPointerCapture(event.pointerId)
    onReorderStart(id, event.clientX, event.clientY)
  }
  const finishReorder = (event: ReactPointerEvent<HTMLButtonElement>) => {
    if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId)
    onReorderEnd()
  }
  const style = { order } as CSSProperties
  return <section
    className={`card-layout-frame${editing ? ' is-editing' : ''}${collapsed ? ' is-collapsed' : ''}${dragging ? ' is-dragging' : ''}`}
    data-layout-card-id={id}
    style={style}
  >
    {(editing || collapsed) && <div className="card-layout-frame__toolbar">
      {editing && <button
        type="button"
        className="card-layout-frame__reorder"
        title={`${copy.move} ${title}`}
        aria-label={`${copy.move} ${title}`}
        onPointerDown={beginReorder}
        onPointerMove={(event) => {
          if (event.currentTarget.hasPointerCapture(event.pointerId)) onReorderMove(id, event.clientX, event.clientY)
        }}
        onPointerUp={finishReorder}
        onPointerCancel={onReorderEnd}
        onLostPointerCapture={onReorderEnd}
        onKeyDown={(event) => {
          const direction = document.documentElement.dir === 'rtl' ? 'rtl' : 'ltr'
          const offset = keyboardReorderOffset(event.key, direction)
          if (offset === null) return
          event.preventDefault()
          onKeyboardReorder(id, offset)
        }}
      ><GripVertical size={16} /></button>}
      <strong>{title}</strong>
      <button
        type="button"
        title={`${collapsed ? copy.expand : copy.collapse} ${title}`}
        aria-label={`${collapsed ? copy.expand : copy.collapse} ${title}`}
        aria-expanded={!collapsed}
        onClick={onToggleCollapsed}
      ><ChevronDown size={16} /></button>
      {editing && <button type="button" title={`${copy.hide} ${title}`} aria-label={`${copy.hide} ${title}`} onClick={onHide}><EyeOff size={16} /></button>}
    </div>}
    {!collapsed && children}
  </section>
}
