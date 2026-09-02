import { useState } from 'react'
import { Navigate, NavLink, Route, Routes, useParams } from 'react-router-dom'
import { ConversationList } from './components/ConversationList'
import { ChatPage } from './pages/ChatPage'
import { HistoryDetailPage, HistoryPage } from './pages/HistoryPage'
import { SettingsPage } from './pages/SettingsPage'

// /chat/:id renders the chat UI pinned to one conversation (the shareable
// link for retrieving and continuing a conversation).
function ChatPageWithId() {
  const { id } = useParams<{ id: string }>()
  return <ChatPage conversationId={id} />
}

const navLinkClass = ({ isActive }: { isActive: boolean }) =>
  `group flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors ${
    isActive ? 'bg-sky-50 font-semibold text-sky-700' : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900'
  }`

const mobileNavLinkClass = ({ isActive }: { isActive: boolean }) =>
  `group flex min-h-11 items-center rounded-lg px-2 py-2 text-xs whitespace-nowrap transition-colors ${
    isActive ? 'bg-sky-50 font-semibold text-sky-700' : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900'
  }`

const railLinkClass = ({ isActive }: { isActive: boolean }) =>
  `flex justify-center rounded-lg px-2 py-2 text-base leading-none transition-colors ${
    isActive ? 'bg-sky-50 text-sky-700' : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900'
  }`

function Navigation({ mobile = false, collapsed = false }: { mobile?: boolean; collapsed?: boolean }) {
  const linkClass = mobile ? mobileNavLinkClass : collapsed ? railLinkClass : navLinkClass
  const iconClass = mobile ? 'hidden' : 'text-base leading-none'
  return (
    <nav className={mobile ? 'flex items-center gap-1' : 'space-y-1'} aria-label="主导航">
      <NavLink to="/" className={linkClass} end title="当前会话">
        <span aria-hidden="true" className={iconClass}>
          ◌
        </span>
        {!collapsed && '当前会话'}
      </NavLink>
      <NavLink to="/history" className={linkClass} title="历史">
        <span aria-hidden="true" className={iconClass}>
          ▤
        </span>
        {!collapsed && '历史'}
      </NavLink>
    </nav>
  )
}

function SettingsLink({ mobile = false, collapsed = false }: { mobile?: boolean; collapsed?: boolean }) {
  const linkClass = mobile ? mobileNavLinkClass : collapsed ? railLinkClass : navLinkClass
  return (
    <NavLink to="/settings" className={linkClass} title="设置">
      <span aria-hidden="true" className={mobile ? 'hidden' : 'text-base leading-none'}>
        ⚙
      </span>
      {!collapsed && '设置'}
    </NavLink>
  )
}

export default function App() {
  // collapsed sidebar state persisted in localStorage (desktop only, lg+)
  const COLLAPSE_KEY = 'openchat.sidebar.collapsed'
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(COLLAPSE_KEY) === '1')
  function toggleSidebar() {
    setCollapsed((v) => {
      localStorage.setItem(COLLAPSE_KEY, v ? '0' : '1')
      return !v
    })
  }

  return (
    <div className="flex h-dvh min-w-0 overflow-hidden bg-slate-100 text-slate-900">
      <aside
        className={
          (collapsed ? 'w-14' : 'w-64') +
          ' hidden shrink-0 flex-col border-r border-slate-200 bg-white transition-[width] duration-200 lg:flex'
        }
      >
        <div className="flex items-center gap-2 border-b border-slate-100 px-3 py-4">
          {!collapsed && <p className="min-w-0 flex-1 truncate pl-2 font-semibold tracking-tight">OpenChat</p>}
          <button
            type="button"
            aria-label={collapsed ? '展开侧边栏' : '收起侧边栏'}
            title={collapsed ? '展开侧边栏' : '收起侧边栏'}
            onClick={toggleSidebar}
            className="shrink-0 rounded-md px-2 py-1.5 text-slate-400 hover:bg-slate-100 hover:text-slate-700"
          >
            {collapsed ? '»' : '«'}
          </button>
        </div>
        <div className="flex-1 overflow-y-auto px-3 py-5">
          <p className={(collapsed ? 'hidden ' : '') + 'px-3 pb-2 text-[11px] font-semibold uppercase tracking-wider text-slate-400'}>会话</p>
          <Navigation collapsed={collapsed} />
          {!collapsed && (
            <>
              <p className="px-3 pb-2 pt-5 text-[11px] font-semibold uppercase tracking-wider text-slate-400">历史会话</p>
              <ConversationList />
            </>
          )}
        </div>
        <div className="border-t border-slate-100 px-3 py-4">
          <SettingsLink collapsed={collapsed} />
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex min-h-11 shrink-0 items-center gap-2 border-b border-slate-200 bg-white px-2 sm:px-5 lg:hidden">
          {/* brand yields its width to the page title on narrow phones; the
              desktop sidebar and the page h1 still identify the app */}
          <span className="hidden shrink-0 font-semibold tracking-tight sm:inline">OpenChat</span>
          <div id="mobile-title-slot" className="flex min-w-0 flex-1 items-center gap-2 overflow-hidden" />
          <div className="flex shrink-0 items-center gap-1">
            <Navigation mobile />
            <SettingsLink mobile />
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
    </div>
  )
}
