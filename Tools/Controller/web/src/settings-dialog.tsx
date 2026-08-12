import { type ReactNode, useEffect, useRef } from 'react'
import { AnimatePresence, motion } from 'motion/react'
import { Settings2, X } from 'lucide-react'

export function SettingsDialog({ open, active = true, title, closeLabel, onClose, children }: {
  open: boolean
  active?: boolean
  title: string
  closeLabel: string
  onClose: () => void
  children: ReactNode
}) {
  const dialog = useRef<HTMLElement>(null)
  const returnFocus = useRef<HTMLElement | null>(null)

  useEffect(() => {
    if (!open) return
    returnFocus.current = document.activeElement as HTMLElement | null
    return () => {
      const target = returnFocus.current
      returnFocus.current = null
      window.requestAnimationFrame(() => target?.focus())
    }
  }, [open])

  useEffect(() => {
    if (!open || !active) return
    const frame = window.requestAnimationFrame(() => {
      if (!dialog.current?.contains(document.activeElement)) dialog.current?.querySelector<HTMLElement>('button, input, select, textarea, [tabindex]:not([tabindex="-1"])')?.focus()
    })
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); onClose(); return }
      if (event.key !== 'Tab' || !dialog.current) return
      const focusable = [...dialog.current.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])')]
      if (!focusable.length) return
      const first = focusable[0]
      const last = focusable.at(-1)!
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus() }
    }
    document.addEventListener('keydown', key, true)
    return () => {
      window.cancelAnimationFrame(frame)
      document.removeEventListener('keydown', key, true)
    }
  }, [active, onClose, open])

  return <AnimatePresence>
    {open && <motion.div className="settings-dialog-layer" aria-hidden={active ? undefined : true} inert={active ? undefined : true} initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }}>
      <button className="settings-dialog-backdrop" type="button" aria-label={closeLabel} onClick={onClose} />
      <motion.section
        ref={dialog}
        className="settings-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="settings-dialog-title"
        initial={{ opacity: 0, y: 18, scale: .985 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, y: 12, scale: .99 }}
        transition={{ duration: .28, ease: [0.22, 1, 0.36, 1] }}
      >
        <header><span><Settings2 size={19} /><strong id="settings-dialog-title">{title}</strong></span><button type="button" aria-label={closeLabel} onClick={onClose}><X size={19} /></button></header>
        <div className="settings-dialog__content">{children}</div>
      </motion.section>
    </motion.div>}
  </AnimatePresence>
}
