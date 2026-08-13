import {
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type KeyboardEvent as ReactKeyboardEvent,
  type PropsWithChildren,
  type ReactNode,
  forwardRef,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
} from 'react'
import { createPortal } from 'react-dom'
import { AnimatePresence, motion, useIsPresent } from 'motion/react'
import {
  AudioLines,
  AlertTriangle,
  Check,
  ChevronRight,
  Keyboard,
  LoaderCircle,
  MoreHorizontal,
  VolumeX,
  X,
  type LucideIcon,
} from 'lucide-react'
import type { DialogState, ToastMessage } from './types'
import { HoldActionSession } from './hold-action'

function interfaceCopy(english: string, persian: string): string {
  return typeof document !== 'undefined' && document.documentElement.lang.toLowerCase().startsWith('fa') ? persian : english
}

export function Icon({ icon: Icon, size = 18 }: { icon: LucideIcon; size?: number }) {
  return <Icon aria-hidden="true" size={size} strokeWidth={1.8} />
}

export function KeyCombo({ keys, separator = '+' }: { keys: Array<string | string[]>; separator?: string }) {
  const label = keys.map((group) => Array.isArray(group) ? group.join(interfaceCopy(' or ', ' یا ')) : group).join(separator === '+' ? interfaceCopy(' plus ', ' به‌علاوهٔ ') : ` ${separator} `)
  return (
    <span className="key-combo" dir="ltr" aria-label={label}>
      {keys.map((group, groupIndex) => (
        <span className="key-combo__group" key={`${groupIndex}-${Array.isArray(group) ? group.join('-') : group}`}>
          {groupIndex > 0 && <span className="key-combo__separator" aria-hidden="true">{separator}</span>}
          {(Array.isArray(group) ? group : [group]).map((key, keyIndex) => (
            <span className="key-combo__key" key={key}>
              {keyIndex > 0 && <span className="key-combo__alternative" aria-hidden="true">/</span>}
              <kbd>{key}</kbd>
            </span>
          ))}
        </span>
      ))}
    </span>
  )
}

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  icon?: LucideIcon
  tone?: 'primary' | 'secondary' | 'ghost' | 'danger' | 'success'
  busy?: boolean
  compact?: boolean
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(function Button({
  icon,
  tone = 'secondary',
  busy,
  compact,
  children,
  className = '',
  disabled,
  ...props
}, ref) {
  return (
    <button
      className={`button button--${tone}${compact ? ' button--compact' : ''} ${className}`}
      disabled={disabled || busy}
      ref={ref}
      {...props}
    >
      <span className="button__wash" aria-hidden="true" />
      {busy ? <LoaderCircle className="spin" aria-hidden="true" size={17} /> : icon ? <Icon icon={icon} size={17} /> : null}
      {children ? <span>{children}</span> : null}
    </button>
  )
})

interface HoldActionButtonProps extends Omit<
  ButtonProps,
  'onPointerDown' | 'onPointerUp' | 'onPointerCancel' | 'onPointerLeave' |
  'onLostPointerCapture' | 'onKeyDown' | 'onKeyUp' | 'onBlur' | 'onClick'
> {
  onHoldStart: () => void | Promise<unknown>
  onHoldStop: () => void | Promise<unknown>
  onHoldError?: (error: unknown) => void
}

// HoldActionButton treats every interrupted gesture as a release. Motion stays
// fail-safe across pointer capture loss, touch cancellation, task switching,
// hidden tabs, route changes, keyboard activation, and component unmount.
export function HoldActionButton({
  onHoldStart,
  onHoldStop,
  onHoldError,
  disabled,
  className = '',
  children,
  ...props
}: HoldActionButtonProps) {
  const [holding, setHolding] = useState(false)
  const startRef = useRef(onHoldStart)
  const stopRef = useRef(onHoldStop)
  const errorRef = useRef(onHoldError)
  startRef.current = onHoldStart
  stopRef.current = onHoldStop
  errorRef.current = onHoldError
  const sessionRef = useRef<HoldActionSession | null>(null)
  if (!sessionRef.current) {
    sessionRef.current = new HoldActionSession(
      () => startRef.current(),
      () => stopRef.current(),
      (error) => errorRef.current?.(error),
      setHolding,
    )
  }
  const session = sessionRef.current

  useEffect(() => {
    const release = () => session.release()
    const onVisibility = () => {
      if (document.visibilityState !== 'visible') release()
    }
    window.addEventListener('blur', release)
    window.addEventListener('pagehide', release)
    document.addEventListener('visibilitychange', onVisibility)
    return () => {
      window.removeEventListener('blur', release)
      window.removeEventListener('pagehide', release)
      document.removeEventListener('visibilitychange', onVisibility)
      session.release(false)
    }
  }, [session])

  return <Button
    {...props}
    disabled={disabled}
    className={`${className} hold-action${holding ? ' is-holding' : ''}`}
    aria-pressed={holding}
    onPointerDown={(event) => {
      if (event.button !== 0 || disabled) return
      event.preventDefault()
      event.currentTarget.setPointerCapture?.(event.pointerId)
      session.begin()
    }}
    onPointerUp={(event) => {
      if (event.currentTarget.hasPointerCapture?.(event.pointerId)) {
        event.currentTarget.releasePointerCapture?.(event.pointerId)
      }
      session.release()
    }}
    onPointerCancel={() => session.release()}
    onPointerLeave={() => session.release()}
    onLostPointerCapture={() => session.release()}
    onKeyDown={(event) => {
      if ((event.key !== ' ' && event.key !== 'Enter') || event.repeat || disabled) return
      event.preventDefault()
      session.begin()
    }}
    onKeyUp={(event) => {
      if (event.key !== ' ' && event.key !== 'Enter') return
      event.preventDefault()
      session.release()
    }}
    data-touch-mode="hold"
    onBlur={() => session.release()}
  >{children}</Button>
}

interface CardProps extends PropsWithChildren {
  icon?: LucideIcon
  iconTone?: 'accent' | 'green' | 'amber' | 'violet' | 'red'
  title?: ReactNode
  eyebrow?: ReactNode
  action?: ReactNode
  menu?: CardMenuItem[]
  className?: string
  id?: string
}

export interface CardMenuItem {
  label: string
  icon?: LucideIcon
  onSelect: () => void
  disabled?: boolean
  tone?: 'normal' | 'danger'
}

export function Card({ icon, iconTone = 'accent', title, eyebrow, action, menu = [], children, className = '', id }: CardProps) {
	const menuID = `card-menu-${useId().replace(/:/g, '')}`
  const [menuOpen, setMenuOpen] = useState(false)
  const [menuPosition, setMenuPosition] = useState({ left: 0, top: 0 })
  const cardRef = useRef<HTMLElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  const returnFocusRef = useRef<HTMLElement | null>(null)

  const closeMenu = (restoreFocus = true) => {
    setMenuOpen(false)
    if (restoreFocus) window.requestAnimationFrame(() => returnFocusRef.current?.focus())
  }

  const openMenu = (left: number, top: number, returnFocus: HTMLElement | null) => {
    returnFocusRef.current = returnFocus
    setMenuPosition({ left, top })
    setMenuOpen(true)
  }

  useLayoutEffect(() => {
    if (!menuOpen || !menuRef.current) return
    const bounds = menuRef.current.getBoundingClientRect()
    const margin = 10
    setMenuPosition((current) => ({
      left: Math.max(margin, Math.min(current.left, window.innerWidth - bounds.width - margin)),
      top: Math.max(margin, Math.min(current.top, window.innerHeight - bounds.height - margin)),
    }))
  }, [menuOpen])

  useEffect(() => {
    if (!menuOpen) return
    const focusFrame = window.requestAnimationFrame(() => menuRef.current?.querySelector<HTMLButtonElement>('button:not(:disabled)')?.focus())
    const dismiss = (event: PointerEvent) => {
      if (!menuRef.current?.contains(event.target as Node) && !triggerRef.current?.contains(event.target as Node)) closeMenu(false)
    }
    const dismissWithoutRestore = () => closeMenu(false)
		const dismissOnFocusDeparture = (event: FocusEvent) => {
			const target = event.target as Node | null
			if (!menuRef.current?.contains(target) && !triggerRef.current?.contains(target)) closeMenu(false)
		}
		const dismissWhenHidden = () => {
			if (document.visibilityState !== 'visible') closeMenu(false)
		}
    document.addEventListener('pointerdown', dismiss, true)
		document.addEventListener('focusin', dismissOnFocusDeparture)
		document.addEventListener('visibilitychange', dismissWhenHidden)
    window.addEventListener('resize', dismissWithoutRestore)
    window.addEventListener('scroll', dismissWithoutRestore, true)
    return () => {
      window.cancelAnimationFrame(focusFrame)
      document.removeEventListener('pointerdown', dismiss, true)
			document.removeEventListener('focusin', dismissOnFocusDeparture)
			document.removeEventListener('visibilitychange', dismissWhenHidden)
      window.removeEventListener('resize', dismissWithoutRestore)
      window.removeEventListener('scroll', dismissWithoutRestore, true)
    }
  }, [menuOpen])

  const openFromKeyboard = (event: ReactKeyboardEvent<HTMLElement>) => {
    if (!menu.length || !(event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10'))) return
    event.preventDefault()
    const bounds = cardRef.current?.getBoundingClientRect()
    if (bounds) openMenu(bounds.right - 12, bounds.top + 44, cardRef.current)
  }

  const navigateMenu = (event: ReactKeyboardEvent<HTMLDivElement>) => {
    const items = [...(menuRef.current?.querySelectorAll<HTMLButtonElement>('button:not(:disabled)') ?? [])]
    if (!items.length) return
    const activeIndex = items.indexOf(document.activeElement as HTMLButtonElement)
    const current = activeIndex < 0 ? 0 : activeIndex
    if (event.key === 'Escape') { event.preventDefault(); closeMenu(); return }
		if (event.key === 'Tab') { closeMenu(false); return }
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
    event.preventDefault()
    const next = event.key === 'Home'
      ? 0
      : event.key === 'End'
        ? items.length - 1
        : event.key === 'ArrowDown'
          ? (current + 1) % items.length
          : (current - 1 + items.length) % items.length
    items[next]?.focus()
  }

  return (
    <section
      id={id}
      className={`card ${className}`}
      ref={cardRef}
      tabIndex={menu.length ? 0 : undefined}
      onKeyDown={openFromKeyboard}
      onContextMenu={menu.length ? (event) => {
        event.preventDefault()
        openMenu(event.clientX, event.clientY, cardRef.current)
      } : undefined}
    >
      {(title || eyebrow || action || menu.length > 0) && (
        <header className="card__header">
          <div className="card__heading">
            {icon && <span className={`card__icon card__icon--${iconTone}`}><Icon icon={icon} size={18} /></span>}
            <div className="card__heading-copy">
              {eyebrow && <div className="eyebrow">{eyebrow}</div>}
              {title && <h2>{title}</h2>}
            </div>
          </div>
          {(action || menu.length > 0) && <div className="card__action">
            {action}
            {menu.length > 0 && <button
              ref={triggerRef}
              type="button"
              className="card__menu-trigger"
              aria-label={interfaceCopy('Card actions', 'عملیات کارت')}
              aria-haspopup="menu"
							aria-controls={menuID}
              aria-expanded={menuOpen}
              onClick={(event) => {
                const bounds = event.currentTarget.getBoundingClientRect()
                openMenu(bounds.right, bounds.bottom + 7, event.currentTarget)
              }}
            ><Icon icon={MoreHorizontal} size={18} /></button>}
          </div>}
        </header>
      )}
      {children}
      {typeof document !== 'undefined' && createPortal(
        <AnimatePresence>
          {menuOpen && <motion.div
			id={menuID}
            ref={menuRef}
            className="card-menu"
            role="menu"
            aria-label={interfaceCopy('Card actions', 'عملیات کارت')}
            style={menuPosition}
            initial={{ opacity: 0, scale: .97 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: .98 }}
            transition={{ duration: .14, ease: [0.22, 1, 0.36, 1] }}
            onKeyDown={navigateMenu}
          >
            {menu.map((item) => <button
              key={item.label}
              type="button"
              role="menuitem"
              disabled={item.disabled}
              data-tone={item.tone ?? 'normal'}
              onClick={() => { closeMenu(); item.onSelect() }}
            >{item.icon && <Icon icon={item.icon} size={17} />}<span>{item.label}</span></button>)}
          </motion.div>}
        </AnimatePresence>,
        document.body,
      )}
    </section>
  )
}

export function SectionTitle({
  eyebrow,
  title,
  detail,
  action,
}: {
  eyebrow: ReactNode
  title: ReactNode
  detail?: ReactNode
  action?: ReactNode
}) {
  return (
    <header className="section-title">
      <div>
        <div className="eyebrow">{eyebrow}</div>
        <h1>{title}</h1>
        {detail && <p>{detail}</p>}
      </div>
      {action && <div className="section-title__action">{action}</div>}
    </header>
  )
}

export function StatusBadge({
  tone,
  children,
  pulse,
}: PropsWithChildren<{ tone: 'good' | 'warn' | 'bad' | 'info' | 'neutral'; pulse?: boolean }>) {
  return (
    <span className={`status-badge status-badge--${tone}${pulse ? ' status-badge--pulse' : ''}`}>
      <span className="status-badge__line" aria-hidden="true" />
      {children}
    </span>
  )
}

export function Sparkline({
  values,
  tone = 'accent',
  label,
}: {
  values: number[]
  tone?: 'accent' | 'green' | 'amber' | 'violet'
  label: string
}) {
  const id = useId().replace(/:/g, '')
  const width = 300
  const height = 92
  const data = values.length > 1 ? values : [0, 0]
  const minimum = Math.min(...data)
  const maximum = Math.max(...data)
  const span = Math.max(1, maximum - minimum)
  const points = data.map((value, index) => ({
    x: (index / Math.max(1, data.length - 1)) * width,
    y: height - 8 - ((value - minimum) / span) * (height - 20),
  }))
  const line = points.map((point, index) => `${index === 0 ? 'M' : 'L'}${point.x.toFixed(2)},${point.y.toFixed(2)}`).join(' ')
  const area = `${line} L${width},${height} L0,${height} Z`
  return (
    <svg className={`sparkline sparkline--${tone}`} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={label} preserveAspectRatio="none">
      <defs>
        <linearGradient id={`fill-${id}`} x1="0" x2="0" y1="0" y2="1">
          <stop offset="0" stopColor="currentColor" stopOpacity=".28" />
          <stop offset="1" stopColor="currentColor" stopOpacity="0" />
        </linearGradient>
        <linearGradient id={`line-${id}`} x1="0" x2="1">
          <stop offset="0" stopColor="currentColor" stopOpacity=".4" />
          <stop offset=".55" stopColor="currentColor" />
          <stop offset="1" stopColor="currentColor" stopOpacity=".65" />
        </linearGradient>
      </defs>
      <path className="sparkline__grid" d="M0 24H300 M0 48H300 M0 72H300" />
      <motion.path
        className="sparkline__area"
        d={area}
        fill={`url(#fill-${id})`}
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: .7 }}
      />
      <motion.path
        className="sparkline__line"
        d={line}
        fill="none"
        stroke={`url(#line-${id})`}
        initial={{ pathLength: 0, opacity: 0 }}
        animate={{ pathLength: 1, opacity: 1 }}
        transition={{ duration: .8, ease: [0.22, 1, 0.36, 1] }}
      />
    </svg>
  )
}

export function MetricCard({
  icon,
  label,
  value,
  unit,
  values,
  tone,
  detail,
}: {
  icon: LucideIcon
  label: string
  value: string
  unit: string
  values: number[]
  tone: 'accent' | 'green' | 'amber' | 'violet'
  detail?: string
}) {
  return (
    <article className={`metric metric--${tone}`}>
      <div className="metric__top">
        <span className="metric__icon"><Icon icon={icon} size={19} /></span>
        <span className="metric__label">{label}</span>
      </div>
      <div className="metric__value" dir="ltr">
        <strong>{value}</strong><span>{unit}</span>
      </div>
      {detail && <div className="metric__detail">{detail}</div>}
      <Sparkline values={values} tone={tone} label={interfaceCopy(`${label} trend`, `روند ${label}`)} />
    </article>
  )
}

export function Toggle({
  checked,
  onChange,
  label,
  detail,
  disabled,
}: {
  checked: boolean
  onChange: (value: boolean) => void
  label: ReactNode
  detail?: ReactNode
  disabled?: boolean
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      className={`toggle-row${disabled ? ' is-disabled' : ''}`}
      disabled={disabled}
      onClick={() => onChange(!checked)}
    >
      <span className="toggle-row__copy">
        <strong>{label}</strong>
        {detail && <small>{detail}</small>}
      </span>
      <span className="toggle" aria-hidden="true"><span /></span>
    </button>
  )
}

export function RangeField({
  label,
  value,
  min,
  max,
  step = 1,
  unit,
  onChange,
  disabled,
}: {
  label: ReactNode
  value: number
  min: number
  max: number
  step?: number
  unit?: string
  onChange: (value: number) => void
  disabled?: boolean
}) {
  const rangeID = useId()
  const [exactValue, setExactValue] = useState(String(value))
  useEffect(() => setExactValue(String(value)), [value])
  const ratio = ((value - min) / Math.max(1, max - min)) * 100
  const commitExactValue = () => {
    const candidate = Number(exactValue)
    if (!Number.isFinite(candidate)) {
      setExactValue(String(value))
      return
    }
    const bounded = Math.min(max, Math.max(min, candidate))
    setExactValue(String(bounded))
    onChange(bounded)
  }
  return (
    <div className={`range-field${disabled ? ' is-disabled' : ''}`}>
      <span className="range-field__label">
        <label htmlFor={rangeID}><strong>{label}</strong></label>
        <span className="range-field__value" dir="ltr">
          <input
            className="range-field__number"
            type="number"
            value={exactValue}
            min={min}
            max={max}
            step={step}
            disabled={disabled}
            aria-label={typeof label === 'string' ? interfaceCopy(`${label} exact value`, `مقدار دقیق ${label}`) : interfaceCopy('Exact value', 'مقدار دقیق')}
            onInput={(event) => setExactValue(event.currentTarget.value)}
            onBlur={commitExactValue}
            onKeyDown={(event) => { if (event.key === 'Enter') event.currentTarget.blur() }}
          />
          {unit && <span>{unit}</span>}
        </span>
      </span>
      <input
        id={rangeID}
        type="range"
        value={value}
        min={min}
        max={max}
        step={step}
        disabled={disabled}
        style={{ '--range': `${ratio}%` } as React.CSSProperties}
        onChange={(event) => onChange(Number(event.target.value))}
      />
    </div>
  )
}

export function Segmented<T extends string>({
  value,
  options,
  onChange,
  label,
}: {
  value: T
  options: { value: T; label: ReactNode; icon?: LucideIcon }[]
  onChange: (value: T) => void
  label: string
}) {
  return (
    <div className="segmented" role="radiogroup" aria-label={label}>
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          role="radio"
          aria-checked={option.value === value}
          className={option.value === value ? 'is-active' : ''}
          onClick={() => onChange(option.value)}
        >
          {option.icon && <Icon icon={option.icon} size={16} />}
          <span>{option.label}</span>
        </button>
      ))}
    </div>
  )
}

export function TextField({
  label,
  hint,
  error,
  success,
  action,
  id,
  'aria-describedby': describedBy,
  ...props
}: InputHTMLAttributes<HTMLInputElement> & {
  label: ReactNode
  hint?: ReactNode
  error?: ReactNode
  success?: ReactNode
  action?: ReactNode
}) {
  const generatedID = useId()
  const inputID = id || `field-${generatedID.replace(/:/g, '')}`
  const hintID = `${inputID}-message`
  const message = error || success || hint
  const contextualHint = Boolean(hint) && !error && !success
  return (
    <div className={`text-field${error ? ' is-invalid' : success ? ' is-valid' : ''}`}>
      <label htmlFor={inputID}>{label}</label>
      <div className="text-field__control">
        <input
          id={inputID}
          aria-describedby={[describedBy, message ? hintID : ''].filter(Boolean).join(' ') || undefined}
          aria-invalid={error ? true : undefined}
          {...props}
        />
        {action && <div className="text-field__action">{action}</div>}
      </div>
      {message && <small id={hintID} className={`text-field__message${contextualHint ? ' text-field__message--contextual' : ''}`} role={error ? 'alert' : 'status'}>{message}</small>}
    </div>
  )
}

export function EmptyState({ icon, title, detail, action }: { icon: LucideIcon; title: ReactNode; detail: ReactNode; action?: ReactNode }) {
  return (
    <div className="empty-state">
      <span className="empty-state__icon"><Icon icon={icon} size={25} /></span>
      <div><strong>{title}</strong><p>{detail}</p></div>
      {action}
    </div>
  )
}

export function DataRow({ label, value, mono, tone }: { label: ReactNode; value: ReactNode; mono?: boolean; tone?: 'good' | 'warn' | 'bad' }) {
  return (
    <div className="data-row">
      <span>{label}</span>
      <strong className={`${mono ? 'mono' : ''}${tone ? ` text-${tone}` : ''}`}>{value}</strong>
    </div>
  )
}

export function NavButton({ icon, label, active, badge, onClick }: { icon: LucideIcon; label: string; active?: boolean; badge?: string; onClick: () => void }) {
  return (
    <button className={`nav-button${active ? ' is-active' : ''}`} onClick={onClick} aria-current={active ? 'page' : undefined}>
      <span className="nav-button__icon"><Icon icon={icon} size={20} /></span>
      <span className="nav-button__label">{label}</span>
      {badge && <span className="nav-button__badge">{badge}</span>}
      <ChevronRight className="nav-button__chevron" aria-hidden="true" size={15} />
    </button>
  )
}

export function focusPageDestination(container: HTMLElement | null): boolean {
  if (!container?.isConnected) return false
  const destination = container.querySelector<HTMLElement>('h1') ?? container
  destination.dataset.routeFocusTarget = ''
  if (!destination.hasAttribute('tabindex')) destination.setAttribute('tabindex', '-1')
  destination.focus({ preventScroll: true })
  return true
}

function PageSurface({ focusOnEnter, children }: PropsWithChildren<{ focusOnEnter: boolean }>) {
  const surfaceRef = useRef<HTMLDivElement>(null)
  const isPresent = useIsPresent()
  // Capture the route-entry decision at mount. AnimatePresence may retain this
  // surface while later application state updates render around it.
  const shouldFocusOnEnter = useRef(focusOnEnter)

  return (
    <motion.div
      ref={surfaceRef}
      className="page"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: .18, ease: 'easeOut' }}
      onAnimationComplete={() => {
        if (isPresent && shouldFocusOnEnter.current) focusPageDestination(surfaceRef.current)
      }}
    >
      {children}
    </motion.div>
  )
}

export function PageTransition({ pageKey, children }: PropsWithChildren<{ pageKey: string }>) {
  const initialPageRendered = useRef(false)
  useEffect(() => { initialPageRendered.current = true }, [])

  return (
    <AnimatePresence mode="wait" initial={false}>
      <PageSurface key={pageKey} focusOnEnter={initialPageRendered.current}>
        {children}
      </PageSurface>
    </AnimatePresence>
  )
}

export function BootGate({
  open,
  progress,
  locale,
  productTitle,
  productShortName,
  productTagline,
  onEnter,
}: {
  open: boolean
  progress: number
  locale: 'en' | 'fa'
  productTitle: string
  productShortName: string
  productTagline: string
  onEnter: (sound: boolean) => void
}) {
  const ready = progress >= 100
  const phase = progress < 34
    ? locale === 'fa' ? 'بارگذاری هویت سامانه' : 'Loading system identity'
    : progress < 68
      ? locale === 'fa' ? 'برقراری مسیر کنترل' : 'Establishing control route'
      : progress < 96
        ? locale === 'fa' ? 'همگام‌سازی وضعیت میزبان' : 'Synchronizing host state'
        : locale === 'fa' ? 'مرکز کنترل آماده است' : 'Control center is ready'
  return (
    <AnimatePresence>
      {open && (
        <motion.section
          className="boot-gate"
          role="dialog"
          aria-modal="true"
          aria-busy={!ready}
          aria-labelledby="boot-title"
          initial={{ opacity: 1 }}
          exit={{ opacity: 0 }}
          transition={{ duration: .28, ease: 'easeOut' }}
        >
          <div className="boot-fuji" aria-hidden="true">
            <span className="boot-fuji__rear" />
            <span className="boot-fuji__face" />
            <span className="boot-fuji__snow" />
            <span className="boot-fuji__trace" />
          </div>
          <div className="boot-identity">
            <div className="brand__mark" aria-hidden="true"><span>{productShortName}</span><i /><i /></div>
            <div>
              <span>{productTitle.toUpperCase()} / {productTagline}</span>
              <h1 id="boot-title">{locale === 'fa' ? 'مرکز کنترل یکپارچه' : productTitle}</h1>
            </div>
          </div>
          <div className="boot-status" aria-live="polite">
            <div><span>{phase}</span><b dir="ltr">{Math.round(progress).toString().padStart(2, '0')}%</b></div>
            <div className="boot-progress" role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(progress)}>
              <span style={{ inlineSize: `${progress}%` }} />
            </div>
          </div>
          <div className={`boot-entry${ready ? ' is-ready' : ''}`} inert={!ready ? true : undefined}>
            <Button tone="primary" icon={AudioLines} disabled={!ready} onClick={() => onEnter(true)}>
              {locale === 'fa' ? 'ورود با نشانه‌های صوتی' : 'Enter with audio cues'}
            </Button>
            <Button icon={VolumeX} disabled={!ready} onClick={() => onEnter(false)}>
              {locale === 'fa' ? 'ورود بی‌صدا' : 'Enter quietly'}
            </Button>
          </div>
        </motion.section>
      )}
    </AnimatePresence>
  )
}

export function HotkeyHelp({ open, locale, onClose }: { open: boolean; locale: 'en' | 'fa'; onClose: () => void }) {
  const title = locale === 'fa' ? 'میانبرهای مرکز کنترل' : 'Control center shortcuts'
  const shortcuts: Array<{ keys: Array<string | string[]>; separator?: string; detail: string }> = locale === 'fa'
    ? [
        { keys: [['Ctrl', '⌘'], 'K'], detail: 'فرمان‌ها و صفحه‌ها' },
        { keys: ['Alt', '1…8'], detail: 'رفتن مستقیم به صفحه' },
        { keys: ['G', ['D', 'C', 'B', 'V', 'W', 'E', 'S']], separator: 'سپس', detail: 'رفتن به داشبورد، کنترلر، میزکار تجهیزات، دستگاه، فضای داده، رویدادها یا تنظیمات' },
        { keys: [['Ctrl', '⌘'], 'Shift', ['←', '→']], detail: 'صفحهٔ کناری در جهت دیداری' },
        { keys: ['?'], detail: 'نمایش یا بستن این راهنما' },
        { keys: ['M'], detail: 'قطع یا وصل نشانه‌های صوتی' },
        { keys: ['Esc'], detail: 'بستن لایهٔ فعال' },
      ]
    : [
        { keys: [['Ctrl', '⌘'], 'K'], detail: 'Commands and pages' },
        { keys: ['Alt', '1…8'], detail: 'Open a page directly' },
        { keys: ['G', ['D', 'C', 'B', 'V', 'W', 'E', 'S']], separator: 'then', detail: 'Go to dashboard, controls, peripheral workbench, device, data workspace, events, or settings' },
        { keys: [['Ctrl', '⌘'], 'Shift', ['←', '→']], detail: 'Adjacent page in the visual direction' },
        { keys: ['?'], detail: 'Show or close this guide' },
        { keys: ['M'], detail: 'Mute or enable interaction cues' },
        { keys: ['Esc'], detail: 'Close the active layer' },
      ]
  return (
    <AnimatePresence>
      {open && (
        <motion.div className="hotkey-layer" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
          <button className="modal-backdrop" aria-label={locale === 'fa' ? 'بستن راهنمای صفحه‌کلید' : 'Close keyboard guide'} onClick={onClose} />
          <motion.section
            className="hotkey-help"
            role="dialog"
            aria-modal="true"
            aria-labelledby="hotkey-help-title"
            initial={{ opacity: 0, scale: .985 }}
            animate={{ opacity: 1, scale: 1 }}
            exit={{ opacity: 0, scale: .99 }}
            transition={{ duration: .2, ease: 'easeOut' }}
          >
            <header><span><Keyboard size={19} /><i>{locale === 'fa' ? 'راهنمای کلیدها' : 'KEY MAP'}</i></span><button className="modal__close" onClick={onClose} aria-label={locale === 'fa' ? 'بستن' : 'Close'}><X size={18} /></button></header>
            <h2 id="hotkey-help-title">{title}</h2>
            <div className="hotkey-list">
              {shortcuts.map((shortcut) => <div key={`${shortcut.keys.flat().join('-')}-${shortcut.detail}`}><KeyCombo keys={shortcut.keys} separator={shortcut.separator} /><span>{shortcut.detail}</span></div>)}
            </div>
          </motion.section>
        </motion.div>
      )}
    </AnimatePresence>
  )
}

export function Modal({ state, onClose, busy }: { state: DialogState; onClose: () => void; busy?: boolean }) {
  const confirmRef = useRef<HTMLButtonElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const dialogRef = useRef<HTMLDivElement>(null)
  const closeRef = useRef(onClose)
  const busyRef = useRef(Boolean(busy))
  closeRef.current = onClose
  busyRef.current = Boolean(busy)
  useEffect(() => {
    if (!state.open) return
    const previous = document.activeElement as HTMLElement | null
    const focusFrame = window.requestAnimationFrame(() => {
      const target = state.tone === 'danger' ? cancelRef.current : confirmRef.current
      ;(target ?? dialogRef.current)?.focus()
    })
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !busyRef.current) {
        event.preventDefault()
        closeRef.current()
        return
      }
      if (event.key !== 'Tab' || !dialogRef.current) return
      const focusable = [...dialogRef.current.querySelectorAll<HTMLElement>(
        'button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [href], [tabindex]:not([tabindex="-1"])',
      )].filter((element) => element.offsetParent !== null)
      if (!focusable.length) {
        event.preventDefault()
        dialogRef.current.focus()
        return
      }
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKey)
    return () => {
      window.cancelAnimationFrame(focusFrame)
      document.removeEventListener('keydown', onKey)
      if (previous?.isConnected) previous.focus()
    }
  }, [state.open, state.tone])
  return (
    <AnimatePresence>
      {state.open && (
        <motion.div className="modal-layer" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
          <motion.button className="modal-backdrop" aria-label={interfaceCopy('Close dialog', 'بستن گفت‌وگو')} onClick={onClose} disabled={busy} />
          <motion.div
            ref={dialogRef}
            className={`modal modal--${state.tone ?? 'normal'}`}
            role="dialog"
            aria-modal="true"
            aria-labelledby="active-dialog-title"
            tabIndex={-1}
            initial={{ opacity: 0, scale: .985, y: 8 }}
            animate={{ opacity: 1, scale: 1, y: 0 }}
            exit={{ opacity: 0, scale: .99, y: 5 }}
            transition={{ duration: .2, ease: [0.22, 1, 0.36, 1] }}
          >
            <button className="modal__close" aria-label={interfaceCopy('Close', 'بستن')} onClick={onClose} disabled={busy}><X size={18} /></button>
            <div className="modal__signal" aria-hidden="true">
              {state.tone === 'danger' ? <AlertTriangle size={25} /> : <Check size={25} />}
            </div>
            <h2 id="active-dialog-title">{state.title}</h2>
            <p>{state.body}</p>
            <div className="modal__actions">
              <Button ref={cancelRef} tone="ghost" onClick={onClose} disabled={busy}>{interfaceCopy('Cancel', 'انصراف')}</Button>
              <Button
                ref={confirmRef}
                tone={state.tone === 'danger' ? 'danger' : 'primary'}
                busy={busy}
                onClick={() => state.action?.()}
              >
                {state.confirmLabel}
              </Button>
            </div>
          </motion.div>
        </motion.div>
      )}
    </AnimatePresence>
  )
}

export function ToastStack({ messages, dismiss }: { messages: ToastMessage[]; dismiss: (id: number) => void }) {
  return (
    <div className="toast-stack" aria-live="polite" aria-atomic="false">
      <AnimatePresence initial={false}>
        {messages.map((message) => (
          <motion.article
            key={message.id}
            className={`toast toast--${message.tone}`}
            initial={{ opacity: 0, x: 18 }}
            animate={{ opacity: 1, x: 0 }}
            exit={{ opacity: 0, x: 12 }}
            transition={{ duration: .32, ease: [0.22, 1, 0.36, 1] }}
          >
            <span className="toast__rail" aria-hidden="true" />
            <div><strong>{message.title}</strong>{message.detail && <p>{message.detail}</p>}</div>
            <button aria-label={interfaceCopy('Dismiss', 'بستن اعلان')} onClick={() => dismiss(message.id)}><X size={16} /></button>
          </motion.article>
        ))}
      </AnimatePresence>
    </div>
  )
}
