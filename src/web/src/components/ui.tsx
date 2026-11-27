import { useEffect, type ButtonHTMLAttributes, type ReactNode } from 'react'
import { errorMessage, stateClasses, stateLabel } from '../lib/format'
import type { WorkspaceState } from '../api/types'

type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger' | 'subtle'
type ButtonSize = 'sm' | 'md'

const buttonVariants: Record<ButtonVariant, string> = {
  primary:
    'bg-cyan-400 text-slate-950 hover:bg-cyan-300 shadow-sm shadow-cyan-500/20',
  secondary:
    'border border-slate-700 bg-slate-800/70 text-slate-200 hover:border-slate-600 hover:bg-slate-800',
  ghost: 'text-slate-300 hover:bg-slate-800/80 hover:text-slate-100',
  danger: 'bg-rose-500 text-white hover:bg-rose-400 shadow-sm shadow-rose-500/20',
  subtle: 'bg-slate-800 text-slate-200 hover:bg-slate-700',
}

const buttonSizes: Record<ButtonSize, string> = {
  sm: 'h-8 px-3 text-xs',
  md: 'h-10 px-4 text-sm',
}

export function Button({
  variant = 'secondary',
  size = 'md',
  className = '',
  type = 'button',
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant
  size?: ButtonSize
}) {
  return (
    <button
      type={type}
      className={`inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-lg font-medium transition focus:outline-none focus-visible:ring-2 focus-visible:ring-cyan-400/60 disabled:cursor-not-allowed disabled:opacity-45 ${buttonVariants[variant]} ${buttonSizes[size]} ${className}`}
      {...props}
    />
  )
}

export function Card({ children, className = '' }: { children: ReactNode; className?: string }) {
  return (
    <section
      className={`rounded-2xl border border-slate-800/80 bg-slate-900/60 shadow-lg shadow-slate-950/20 ${className}`}
    >
      {children}
    </section>
  )
}

export function CardHeader({
  title,
  subtitle,
  action,
}: {
  title: ReactNode
  subtitle?: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="flex items-start justify-between gap-3 border-b border-slate-800/70 px-5 py-4">
      <div>
        <h2 className="text-sm font-semibold text-slate-100">{title}</h2>
        {subtitle ? <p className="mt-1 text-xs text-slate-400">{subtitle}</p> : null}
      </div>
      {action}
    </div>
  )
}

type BadgeTone = 'slate' | 'cyan' | 'emerald' | 'amber' | 'rose' | 'sky'

const badgeTones: Record<BadgeTone, string> = {
  slate: 'border-slate-600/40 bg-slate-700/20 text-slate-300',
  cyan: 'border-cyan-400/30 bg-cyan-400/10 text-cyan-300',
  emerald: 'border-emerald-400/30 bg-emerald-400/10 text-emerald-300',
  amber: 'border-amber-400/30 bg-amber-400/10 text-amber-300',
  rose: 'border-rose-400/30 bg-rose-400/10 text-rose-300',
  sky: 'border-sky-400/30 bg-sky-400/10 text-sky-300',
}

export function Badge({
  children,
  tone = 'slate',
  className = '',
}: {
  children: ReactNode
  tone?: BadgeTone
  className?: string
}) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 py-0.5 text-xs font-medium ${badgeTones[tone]} ${className}`}
    >
      {children}
    </span>
  )
}

export function StateBadge({ state }: { state: WorkspaceState }) {
  return (
    <span
      className={`inline-flex items-center gap-1.5 whitespace-nowrap rounded-full border px-2.5 py-0.5 text-xs font-medium ${stateClasses(state)}`}
    >
      <span className="h-1.5 w-1.5 rounded-full bg-current" />
      {stateLabel(state)}
    </span>
  )
}

export function Spinner({ className = 'h-4 w-4' }: { className?: string }) {
  return (
    <span
      className={`inline-block animate-spin rounded-full border-2 border-current border-t-transparent ${className}`}
      role="status"
      aria-label="加载中"
    />
  )
}

export function Skeleton({ className = '' }: { className?: string }) {
  return <div className={`animate-pulse rounded-lg bg-slate-800/70 ${className}`} />
}

export function ErrorNotice({
  error,
  onRetry,
  className = '',
}: {
  error: unknown
  onRetry?: () => void
  className?: string
}) {
  return (
    <div
      className={`flex flex-wrap items-center justify-between gap-3 rounded-xl border border-rose-500/30 bg-rose-500/10 px-4 py-3 text-sm text-rose-200 ${className}`}
    >
      <span>{errorMessage(error)}</span>
      {onRetry ? (
        <Button variant="ghost" size="sm" onClick={onRetry}>
          重试
        </Button>
      ) : null}
    </div>
  )
}

export function EmptyState({
  title,
  description,
  action,
}: {
  title: string
  description?: string
  action?: ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-3 px-6 py-14 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-2xl border border-slate-700 bg-slate-800/60 text-lg text-slate-400">
        ◇
      </div>
      <div>
        <p className="text-sm font-medium text-slate-200">{title}</p>
        {description ? <p className="mt-1 max-w-md text-xs text-slate-400">{description}</p> : null}
      </div>
      {action}
    </div>
  )
}

export function Field({
  label,
  hint,
  error,
  children,
  htmlFor,
}: {
  label: string
  hint?: string
  error?: string
  children: ReactNode
  htmlFor?: string
}) {
  return (
    <div className="space-y-1.5">
      <label htmlFor={htmlFor} className="block text-xs font-medium text-slate-300">
        {label}
      </label>
      {children}
      {error ? (
        <p className="text-xs text-rose-300">{error}</p>
      ) : hint ? (
        <p className="text-xs text-slate-500">{hint}</p>
      ) : null}
    </div>
  )
}

export const inputClass =
  'w-full rounded-lg border border-slate-700 bg-slate-950/70 px-3 py-2 text-sm text-slate-100 placeholder:text-slate-600 focus:border-cyan-400/70 focus:outline-none focus:ring-2 focus:ring-cyan-400/20'

export function Detail({
  label,
  value,
  mono = false,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
      <p className="text-[11px] uppercase tracking-wide text-slate-500">{label}</p>
      <p className={`mt-1 break-all text-sm text-slate-200 ${mono ? 'font-mono text-xs' : ''}`}>
        {value}
      </p>
    </div>
  )
}

export function ProgressBar({ value }: { value: number }) {
  const clamped = Math.max(0, Math.min(100, value))
  return (
    <div
      className="h-2 w-full overflow-hidden rounded-full bg-slate-800"
      role="progressbar"
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={clamped}
    >
      <div
        className="h-full rounded-full bg-gradient-to-r from-cyan-500 to-emerald-400 transition-[width] duration-300"
        style={{ width: `${clamped}%` }}
      />
    </div>
  )
}

export function Modal({
  open,
  onClose,
  title,
  description,
  children,
  footer,
  width = 'max-w-2xl',
}: {
  open: boolean
  onClose: () => void
  title: ReactNode
  description?: ReactNode
  children: ReactNode
  footer?: ReactNode
  width?: string
}) {
  useEffect(() => {
    if (!open) {
      return
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        onClose()
      }
    }
    window.addEventListener('keydown', onKeyDown)
    const previous = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    return () => {
      window.removeEventListener('keydown', onKeyDown)
      document.body.style.overflow = previous
    }
  }, [open, onClose])

  if (!open) {
    return null
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-slate-950/75 p-3 backdrop-blur-sm sm:items-center sm:p-6"
      onClick={onClose}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="modal-title"
        className={`flex max-h-[92vh] w-full ${width} flex-col overflow-hidden rounded-2xl border border-slate-700/80 bg-slate-900 shadow-2xl shadow-black/40`}
        onClick={(event) => event.stopPropagation()}
      >
        <div className="flex items-start justify-between gap-4 border-b border-slate-800 px-5 py-4">
          <div>
            <h2 id="modal-title" className="text-base font-semibold text-slate-100">
              {title}
            </h2>
            {description ? <p className="mt-1 text-xs text-slate-400">{description}</p> : null}
          </div>
          <button
            type="button"
            onClick={onClose}
            className="rounded-lg px-2 py-1 text-slate-400 transition hover:bg-slate-800 hover:text-slate-200"
            aria-label="关闭"
          >
            ✕
          </button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
        {footer ? (
          <div className="flex flex-wrap items-center justify-end gap-2 border-t border-slate-800 px-5 py-3">
            {footer}
          </div>
        ) : null}
      </div>
    </div>
  )
}
