// Minimal shadcn-style primitives (tailwind only, no component library).

import type {
  ButtonHTMLAttributes,
  LabelHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
  TextareaHTMLAttributes,
} from 'react'

const focusRing = 'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-sky-600'

export function Button({
  variant = 'primary',
  className = '',
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: 'primary' | 'secondary' | 'danger' }) {
  const styles = {
    primary: 'bg-sky-600 text-white hover:bg-sky-700 disabled:bg-sky-300',
    secondary: 'border border-slate-300 bg-white text-slate-700 hover:bg-slate-50',
    danger: 'bg-red-600 text-white hover:bg-red-700 disabled:bg-red-300',
  }[variant]
  return (
    <button
      className={`inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-2 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-60 ${focusRing} ${styles} ${className}`}
      {...props}
    />
  )
}

export function Card({ className = '', children }: { className?: string; children: ReactNode }) {
  return <div className={`rounded-lg border border-slate-200 bg-white p-4 shadow-sm ${className}`}>{children}</div>
}

export function Label({ className = '', ...props }: LabelHTMLAttributes<HTMLLabelElement>) {
  return (
    <label className={`mb-1 block text-sm font-medium text-slate-700 ${className}`} {...props} />
  )
}

export function Textarea({ className = '', ...props }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={`w-full rounded-md border border-slate-300 bg-white px-3 py-2 text-sm text-slate-900 placeholder:text-slate-400 disabled:cursor-not-allowed disabled:bg-slate-100 disabled:text-slate-500 ${focusRing} ${className}`}
      {...props}
    />
  )
}

export function Select({ className = '', ...props }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={`rounded-md border border-slate-300 bg-white px-2 py-1.5 text-sm text-slate-700 disabled:cursor-not-allowed disabled:bg-slate-100 disabled:text-slate-500 ${focusRing} ${className}`}
      {...props}
    />
  )
}

export function Spinner({ label }: { label?: string }) {
  return (
    <span role="status" className="inline-flex items-center gap-2 text-sm text-slate-600">
      <span aria-hidden="true" className="h-4 w-4 animate-spin rounded-full border-2 border-slate-300 border-t-sky-600" />
      {label}
    </span>
  )
}

export function ErrorBox({ children }: { children: ReactNode }) {
  return (
    <div role="alert" className="rounded-md border border-red-300 bg-red-50 p-3 text-sm text-red-800">
      {children}
    </div>
  )
}
