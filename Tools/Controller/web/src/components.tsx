import {
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type PropsWithChildren,
  type ReactNode,
  forwardRef,
  useEffect,
  useId,
  useRef,
} from 'react'
import { AnimatePresence, motion } from 'motion/react'
import {
  AudioLines,
  AlertTriangle,
  Check,
  ChevronRight,
  Keyboard,
  LoaderCircle,
  VolumeX,
  X,
  type LucideIcon,
} from 'lucide-react'
import type { DialogState, ToastMessage } from './types'

export function Icon({ icon: Icon, size = 18 }: { icon: LucideIcon; size?: number }) {
  return <Icon aria-hidden="true" size={size} strokeWidth={1.8} />
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

interface CardProps extends PropsWithChildren {
  title?: ReactNode
  eyebrow?: ReactNode
  action?: ReactNode
  className?: string
  id?: string
}

export function Card({ title, eyebrow, action, children, className = '', id }: CardProps) {
  return (
    <section id={id} className={`card ${className}`}>
      {(title || eyebrow || action) && (
        <header className="card__header">
          <div>
            {eyebrow && <div className="eyebrow">{eyebrow}</div>}
            {title && <h2>{title}</h2>}
          </div>
          {action && <div className="card__action">{action}</div>}
        </header>
      )}
      {children}
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
  tone = 'cyan',
  label,
}: {
  values: number[]
  tone?: 'cyan' | 'green' | 'amber' | 'violet'
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
  tone: 'cyan' | 'green' | 'amber' | 'violet'
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
      <Sparkline values={values} tone={tone} label={`${label} trend`} />
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
    <label className={`toggle-row${disabled ? ' is-disabled' : ''}`}>
      <span className="toggle-row__copy">
        <strong>{label}</strong>
        {detail && <small>{detail}</small>}
      </span>
      <input type="checkbox" checked={checked} disabled={disabled} onChange={(event) => onChange(event.target.checked)} />
      <span className="toggle" aria-hidden="true"><span /></span>
    </label>
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
  const ratio = ((value - min) / Math.max(1, max - min)) * 100
  return (
    <label className={`range-field${disabled ? ' is-disabled' : ''}`}>
      <span className="range-field__label"><strong>{label}</strong><output dir="ltr">{value}{unit}</output></span>
      <input
        type="range"
        value={value}
        min={min}
        max={max}
        step={step}
        disabled={disabled}
        style={{ '--range': `${ratio}%` } as React.CSSProperties}
        onChange={(event) => onChange(Number(event.target.value))}
      />
    </label>
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

export function TextField({ label, hint, ...props }: InputHTMLAttributes<HTMLInputElement> & { label: ReactNode; hint?: ReactNode }) {
  return (
    <label className="text-field">
      <span>{label}</span>
      <input {...props} />
      {hint && <small>{hint}</small>}
    </label>
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

export function PageTransition({ pageKey, children }: PropsWithChildren<{ pageKey: string }>) {
  return (
    <AnimatePresence mode="wait" initial={false}>
      <motion.div
        key={pageKey}
        className="page"
        initial={{ opacity: 0, filter: 'blur(8px)', clipPath: 'inset(0 0 8% 0)' }}
        animate={{ opacity: 1, filter: 'blur(0px)', clipPath: 'inset(0 0 0% 0)' }}
        exit={{ opacity: 0, filter: 'blur(5px)', clipPath: 'inset(4% 0 0 0)' }}
        transition={{ duration: .28, ease: [0.22, 1, 0.36, 1] }}
      >
        {children}
      </motion.div>
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
        ? locale === 'fa' ? 'همگام‌سازی وضعیت زنده' : 'Synchronizing live state'
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
          exit={{ opacity: 0, filter: 'blur(15px)', clipPath: 'inset(0 0 100% 0)' }}
          transition={{ duration: .65, ease: [0.7, 0, 0.84, 0] }}
        >
          <div className="boot-grid" aria-hidden="true" />
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
            <small>{locale === 'fa' ? 'فقط بازخوردهای کوتاه تعاملی؛ بدون موسیقی یا صدای مداوم' : 'Short interaction cues only — no music or continuous sound'}</small>
          </div>
        </motion.section>
      )}
    </AnimatePresence>
  )
}

export function HotkeyHelp({ open, locale, onClose }: { open: boolean; locale: 'en' | 'fa'; onClose: () => void }) {
  const title = locale === 'fa' ? 'میانبرهای مرکز کنترل' : 'Control center shortcuts'
  const shortcuts = locale === 'fa'
    ? [
        ['Ctrl / ⌘ + K', 'فرمان‌ها و صفحه‌ها'],
        ['Alt + 1…7', 'رفتن مستقیم به صفحه'],
        ['G سپس D / C / B / V / W / E / S', 'رفتن به داشبورد، کنترلر، میزکار تجهیزات، دستگاه، فضای داده، رویدادها یا تنظیمات'],
        ['Ctrl / ⌘ + Shift + ← / →', 'صفحهٔ کناری در جهت دیداری'],
        ['?', 'نمایش یا بستن این راهنما'],
        ['M', 'قطع یا وصل نشانه‌های صوتی'],
        ['Esc', 'بستن لایهٔ فعال'],
      ]
    : [
        ['Ctrl / ⌘ + K', 'Commands and pages'],
        ['Alt + 1…7', 'Open a page directly'],
        ['G then D / C / B / V / W / E / S', 'Go to dashboard, controls, peripheral workbench, device, data workspace, events, or settings'],
        ['Ctrl / ⌘ + Shift + ← / →', 'Adjacent page in the visual direction'],
        ['?', 'Show or close this guide'],
        ['M', 'Mute or enable interaction cues'],
        ['Esc', 'Close the active layer'],
      ]
  return (
    <AnimatePresence>
      {open && (
        <motion.div className="hotkey-layer" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
          <button className="modal-backdrop" aria-label="Close keyboard guide" onClick={onClose} />
          <motion.section
            className="hotkey-help"
            role="dialog"
            aria-modal="true"
            aria-labelledby="hotkey-help-title"
            initial={{ opacity: 0, scale: .97, filter: 'blur(12px)', clipPath: 'inset(12% 8% 12% 8%)' }}
            animate={{ opacity: 1, scale: 1, filter: 'blur(0)', clipPath: 'inset(0% 0% 0% 0%)' }}
            exit={{ opacity: 0, scale: .98, filter: 'blur(8px)', clipPath: 'inset(9% 6% 9% 6%)' }}
            transition={{ type: 'spring', stiffness: 420, damping: 38 }}
          >
            <header><span><Keyboard size={19} /><i>KEY MAP</i></span><button className="modal__close" onClick={onClose} aria-label="Close"><X size={18} /></button></header>
            <h2 id="hotkey-help-title">{title}</h2>
            <p>{locale === 'fa' ? 'میانبرهای سراسری هنگام تایپ، ترکیب حروف فارسی یا کار با فیلدهای قابل‌ویرایش غیرفعال می‌شوند.' : 'Global shortcuts stand down while typing, composing with an IME, or working in editable controls.'}</p>
            <div className="hotkey-list">
              {shortcuts.map(([keys, detail]) => <div key={keys}><kbd dir="ltr">{keys}</kbd><span>{detail}</span></div>)}
            </div>
          </motion.section>
        </motion.div>
      )}
    </AnimatePresence>
  )
}

export function Modal({ state, onClose, busy }: { state: DialogState; onClose: () => void; busy?: boolean }) {
  const confirmRef = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!state.open) return
    const previous = document.activeElement as HTMLElement | null
    const timer = window.setTimeout(() => confirmRef.current?.focus(), 60)
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !busy) onClose()
    }
    document.addEventListener('keydown', onKey)
    return () => {
      window.clearTimeout(timer)
      document.removeEventListener('keydown', onKey)
      previous?.focus()
    }
  }, [state.open, busy, onClose])
  return (
    <AnimatePresence>
      {state.open && (
        <motion.div className="modal-layer" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
          <motion.button className="modal-backdrop" aria-label="Close dialog" onClick={onClose} disabled={busy} />
          <motion.div
            className={`modal modal--${state.tone ?? 'normal'}`}
            role="dialog"
            aria-modal="true"
            aria-labelledby="active-dialog-title"
            initial={{ opacity: 0, scale: .96, filter: 'blur(10px)', clipPath: 'inset(14% 8% 14% 8%)' }}
            animate={{ opacity: 1, scale: 1, filter: 'blur(0)', clipPath: 'inset(0% 0% 0% 0%)' }}
            exit={{ opacity: 0, scale: .975, filter: 'blur(7px)', clipPath: 'inset(10% 7% 10% 7%)' }}
            transition={{ type: 'spring', stiffness: 430, damping: 36, mass: .8 }}
          >
            <button className="modal__close" aria-label="Close" onClick={onClose} disabled={busy}><X size={18} /></button>
            <div className="modal__signal" aria-hidden="true">
              {state.tone === 'danger' ? <AlertTriangle size={25} /> : <Check size={25} />}
            </div>
            <h2 id="active-dialog-title">{state.title}</h2>
            <p>{state.body}</p>
            <div className="modal__actions">
              <Button tone="ghost" onClick={onClose} disabled={busy}>Cancel</Button>
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
            initial={{ opacity: 0, x: '18%', filter: 'blur(8px)', clipPath: 'inset(0 0 0 100%)' }}
            animate={{ opacity: 1, x: 0, filter: 'blur(0)', clipPath: 'inset(0 0 0 0%)' }}
            exit={{ opacity: 0, x: '12%', filter: 'blur(6px)', clipPath: 'inset(0 0 0 100%)' }}
            transition={{ duration: .32, ease: [0.22, 1, 0.36, 1] }}
          >
            <span className="toast__rail" aria-hidden="true" />
            <div><strong>{message.title}</strong>{message.detail && <p>{message.detail}</p>}</div>
            <button aria-label="Dismiss" onClick={() => dismiss(message.id)}><X size={16} /></button>
          </motion.article>
        ))}
      </AnimatePresence>
    </div>
  )
}
