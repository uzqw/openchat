// Theme (dark mode) state: default follows the OS preference, a manual
// toggle overrides it and persists in localStorage. The `.dark` class on
// <html> drives Tailwind's dark variant (see index.css).

const KEY = 'openchat.theme'

function systemDark(): boolean {
  return typeof matchMedia === 'function' && matchMedia('(prefers-color-scheme: dark)').matches
}

export function applyTheme(mode: 'light' | 'dark') {
  document.documentElement.classList.toggle('dark', mode === 'dark')
  document.documentElement.style.colorScheme = mode
}

/** Apply the persisted override, or the OS preference. Call before first render. */
export function initTheme() {
  const saved = localStorage.getItem(KEY)
  applyTheme(saved === 'light' || saved === 'dark' ? saved : systemDark() ? 'dark' : 'light')
}

/** Flip the current theme and persist the override. */
export function toggleTheme() {
  const next = document.documentElement.classList.contains('dark') ? 'light' : 'dark'
  applyTheme(next)
  localStorage.setItem(KEY, next)
}

export function isDark(): boolean {
  return document.documentElement.classList.contains('dark')
}
