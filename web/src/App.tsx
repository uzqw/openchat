import { useEffect, useRef, useState } from 'react'
import { Navigate, NavLink, Route, Routes, useLocation, useNavigate, useParams } from 'react-router-dom'
import { ConversationList } from './components/ConversationList'
import { ChatPage } from './pages/ChatPage'
import { HistoryDetailPage, HistoryPage } from './pages/HistoryPage'
import { SettingsPage } from './pages/SettingsPage'
import { api, apiErrorMessage } from './api'
import { isDark, toggleTheme } from './lib/theme'

// /chat/:id renders the chat UI pinned to one conversation (the shareable
// link for retrieving and continuing a conversation).
function ChatPageWithId() {
  const { id } = useParams<{ id: string }>()
  return <ChatPage conversationId={id} />
}

const navLinkClass = ({ isActive }: { isActive: boolean }) =>
  `group flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors ${
    isActive ? 'bg-accent-soft font-semibold text-accent-strong' : 'text-ink-soft hover:bg-hover hover:text-ink'
  }`

const railLinkClass = ({ isActive }: { isActive: boolean }) =>
  `flex justify-center rounded-lg px-2 py-2 text-base leading-none transition-colors ${
    isActive ? 'bg-accent-soft text-accent-strong' : 'text-ink-soft hover:bg-hover hover:text-ink'
  }`

// 新会话: creates a fresh conversation (respecting the site chosen in the
// chat header) and jumps to it. The one place the shell talks to the API.
function NewConversationButton({ collapsed = false }: { collapsed?: boolean }) {
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function start() {
    setBusy(true)
    setError('')
    try {
      const saved = localStorage.getItem('openchat.site') || undefined
      const created = await api.createConversation(saved)
      navigate(`/chat/${created.id}`)
    } catch (e) {
      setError(apiErrorMessage(e))
    } finally {
      setBusy(false)
    }
  }
  if (collapsed) {
    return (
      <button
        type="button"
        aria-label="新会话"
        title="新会话"
        disabled={busy}
        onClick={() => void start()}
        className="flex w-full justify-center rounded-md bg-accent-fill py-2 text-base leading-none text-white hover:bg-accent-fill-strong disabled:cursor-not-allowed disabled:opacity-60"
      >
        +
      </button>
    )
  }
  return (
    <div>
      <button
        type="button"
        disabled={busy}
        onClick={() => void start()}
        className="flex w-full items-center justify-center gap-2 rounded-md bg-accent-fill px-3 py-2 text-sm font-medium text-white hover:bg-accent-fill-strong disabled:cursor-not-allowed disabled:opacity-60"
      >
        <span aria-hidden="true" className="text-base leading-none">
          +
        </span>
        新会话
      </button>
      {error && (
        <p role="alert" className="mt-2 px-1 text-xs text-danger-ink">
          {error}
        </p>
      )}
    </div>
  )
}

// Dark-mode toggle: shows the mode you switch TO (☀ in dark, ☾ in light).
function ThemeToggle({ className = '' }: { className?: string }) {
  const [dark, setDark] = useState(isDark())
  return (
    <button
      type="button"
      aria-label={dark ? '切换到浅色模式' : '切换到深色模式'}
      title={dark ? '切换到浅色模式' : '切换到深色模式'}
      onClick={() => {
        toggleTheme()
        setDark(isDark())
      }}
      className={`shrink-0 rounded-md px-2 py-1.5 text-base leading-none text-ink-faint hover:bg-hover hover:text-ink-soft ${className}`}
    >
      {dark ? '☀' : '☾'}
    </button>
  )
}

// Shared sidebar content: 新会话 on top, the conversation list as the body,
// and 历史/设置/theme in the footer. Rendered by the desktop aside and the
// mobile/tablet drawer so every width shares one mental model.
function SidebarBody({ collapsed = false }: { collapsed?: boolean }) {
  const linkClass = collapsed ? railLinkClass : navLinkClass
  return (
    <>
      <div className={collapsed ? 'px-2 pt-4' : 'px-3 pt-4'}>
        <NewConversationButton collapsed={collapsed} />
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-4">
        {!collapsed && <ConversationList />}
      </div>
      <div className="flex items-center gap-1 border-t border-line px-3 py-3">
        <NavLink to="/history" className={linkClass} title="历史">
          <span aria-hidden="true" className="text-base leading-none">
            ▤
          </span>
          {!collapsed && '历史'}
        </NavLink>
        <NavLink to="/settings" className={linkClass} title="设置">
          <span aria-hidden="true" className="text-base leading-none">
            ⚙
          </span>
          {!collapsed && '设置'}
        </NavLink>
        <ThemeToggle className={collapsed ? 'mx-auto' : 'ml-auto'} />
      </div>
    </>
  )
}

// Mobile/tablet (<lg) conversations drawer: the sidebar as a slide-over.
// Closes on backdrop click, Escape, or any navigation.
function ConversationsDrawer({ open, onClose }: { open: boolean; onClose: () => void }) {
  const closeRef = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!open) return
    closeRef.current?.focus()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  return (
    <div
      className={`fixed inset-0 z-50 transition-[visibility] duration-200 lg:hidden ${open ? 'visible' : 'invisible'}`}
    >
      <div
        data-testid="drawer-backdrop"
        aria-hidden="true"
        onClick={onClose}
        className={`absolute inset-0 bg-black/40 transition-opacity duration-200 ${open ? 'opacity-100' : 'opacity-0'}`}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-label="会话列表"
        className={`absolute inset-y-0 left-0 flex w-80 max-w-[85vw] flex-col bg-surface shadow-xl transition-transform duration-200 ${
          open ? 'translate-x-0' : '-translate-x-full'
        }`}
      >
        <div className="flex items-center gap-2 border-b border-line px-3 py-4">
          <p className="min-w-0 flex-1 truncate pl-2 font-semibold tracking-tight">OpenChat</p>
          <button
            ref={closeRef}
            type="button"
            aria-label="关闭会话列表"
            onClick={onClose}
            className="shrink-0 rounded-md px-2 py-1.5 text-ink-faint hover:bg-hover hover:text-ink-soft"
          >
            ✕
          </button>
        </div>
        <SidebarBody />
      </div>
    </div>
  )
}

export default function App() {
  // collapsed sidebar state persisted in localStorage (desktop only, lg+)
  const COLLAPSE_KEY = 'openchat.sidebar.collapsed'
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(COLLAPSE_KEY) === '1')
  const [drawerOpen, setDrawerOpen] = useState(false)
  const location = useLocation()

  // any navigation closes the mobile/tablet drawer
  useEffect(() => {
    setDrawerOpen(false)
  }, [location])

  function toggleSidebar() {
    setCollapsed((v) => {
      localStorage.setItem(COLLAPSE_KEY, v ? '0' : '1')
      return !v
    })
  }

  return (
    <div className="flex h-dvh min-w-0 overflow-hidden bg-app text-ink">
      <aside
        className={
          (collapsed ? 'w-14' : 'w-64') +
          ' hidden shrink-0 flex-col border-r border-line bg-surface transition-[width] duration-200 lg:flex'
        }
      >
        <div className="flex items-center gap-2 border-b border-line px-3 py-4">
          {!collapsed && <p className="min-w-0 flex-1 truncate pl-2 font-semibold tracking-tight">OpenChat</p>}
          <button
            type="button"
            aria-label={collapsed ? '展开侧边栏' : '收起侧边栏'}
            title={collapsed ? '展开侧边栏' : '收起侧边栏'}
            onClick={toggleSidebar}
            className="shrink-0 rounded-md px-2 py-1.5 text-ink-faint hover:bg-hover hover:text-ink-soft"
          >
            {collapsed ? '»' : '«'}
          </button>
        </div>
        <SidebarBody collapsed={collapsed} />
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex min-h-11 shrink-0 items-center gap-2 border-b border-line bg-surface px-2 sm:px-5 lg:hidden">
          <button
            type="button"
            aria-label="打开会话列表"
            aria-expanded={drawerOpen}
            onClick={() => setDrawerOpen(true)}
            className="shrink-0 rounded-md px-2 py-1.5 text-ink-soft hover:bg-hover hover:text-ink"
          >
            ☰
          </button>
          {/* brand yields its width to the page title on narrow phones; the
              desktop sidebar and the page h1 still identify the app */}
          <span className="hidden shrink-0 font-semibold tracking-tight sm:inline">OpenChat</span>
          <div id="mobile-title-slot" className="flex min-w-0 flex-1 items-center gap-2 overflow-hidden" />
          <div className="flex shrink-0 items-center gap-1">
            <ThemeToggle />
          </div>
        </header>
        <main className="min-h-0 flex-1 overflow-y-auto">
          <Routes>
            <Route path="/" element={<ChatPage />} />
            <Route path="/chat/:id" element={<ChatPageWithId />} />
            <Route path="/history" element={<HistoryPage />} />
            <Route path="/history/:id" element={<HistoryDetailPage />} />
            <Route path="/settings" element={<SettingsPage />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </main>
      </div>

      <ConversationsDrawer open={drawerOpen} onClose={() => setDrawerOpen(false)} />
    </div>
  )
}
