// Minimal shadcn-style primitives (tailwind only, no component library).

import type {
  ButtonHTMLAttributes,
  LabelHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
  TextareaHTMLAttributes,
} from 'react'

const focusRing = 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent'

export function Button({
  variant = 'primary',
  className = '',
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'primary' | 'secondary' | 'danger' }) {
  const styles = {
    primary: 'bg-accent-fill text-white hover:bg-accent-fill-strong',
    secondary: 'border border-line-strong bg-surface text-ink-soft hover:bg-subtle',
    danger: 'bg-red-600 text-white hover:bg-red-700',
  }[variant]
  return (
    <button
      className={`inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-2 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-60 ${focusRing} ${styles} ${className}`}
      {...props}
    />
  )
}

export function Card({ className = '', children }: { className?: string; children: ReactNode }) {
  return <div className={`rounded-lg border border-line bg-surface p-4 shadow-sm ${className}`}>{children}</div>
}

export function Label({ className = '', ...props }: LabelHTMLAttributes<HTMLLabelElement>) {
  return (
    <label className={`mb-1 block text-sm font-medium text-ink-soft ${className}`} {...props} />
  )
}

export function Textarea({ className = '', ...props }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={`w-full rounded-md border border-line-strong bg-surface px-3 py-2 text-sm text-ink placeholder:text-ink-faint disabled:cursor-not-allowed disabled:bg-subtle disabled:text-ink-faint ${focusRing} ${className}`}
      {...props}
    />
  )
}

export function Select({ className = '', ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={`rounded-md border border-line-strong bg-surface px-2 py-1.5 text-sm text-ink-soft disabled:cursor-not-allowed disabled:bg-subtle disabled:text-ink-faint ${focusRing} ${className}`}
      {...props}
    />
  )
}

// Skeleton: a pulsing placeholder block (design tokens only).
export function Skeleton({ className = '' }: { className?: string }) {
  return <div aria-hidden="true" className={`animate-pulse rounded-md bg-hover ${className}`} />
}

// ProgressBar: indeterminate progress (no % is available from the API), a
// bar sweeping across a track. The sweep animation lives in index.css so
// prefers-reduced-motion can disable it.
export function ProgressBar({ label = '进行中', className = '' }: { className?: string; label?: string }) {
  return (
    <div role="progressbar" aria-label={label} className={`h-1.5 w-full overflow-hidden rounded-full bg-hover ${className}`}>
      <div className="oc-progress-bar h-full w-1/3 rounded-full bg-accent-fill" />
    </div>
  )
}

export function ErrorBox({ children }: { children: ReactNode }) {
  return (
    <div role="alert" className="rounded-md border border-danger-line bg-danger-soft p-3 text-sm text-danger-ink">
      {children}
    </div>
  )
}
