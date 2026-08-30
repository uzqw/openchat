import { Navigate, NavLink, Route, Routes, useParams } from 'react-router-dom'
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
  `group flex items-center rounded-lg px-2 py-2 text-xs whitespace-nowrap transition-colors ${
    isActive ? 'bg-sky-50 font-semibold text-sky-700' : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900'
  }`

function Navigation({ mobile = false }: { mobile?: boolean }) {
  const linkClass = mobile ? mobileNavLinkClass : navLinkClass
  const iconClass = mobile ? 'hidden' : 'text-base leading-none'
  return (
    <nav className={mobile ? 'flex items-center gap-1' : 'space-y-1'} aria-label="主导航">
      <NavLink to="/" className={linkClass} end>
        <span aria-hidden="true" className={iconClass}>
          ◌
        </span>
        当前会话
      </NavLink>
      <NavLink to="/history" className={linkClass}>
        <span aria-hidden="true" className={iconClass}>
          ▤
        </span>
        历史
      </NavLink>
    </nav>
  )
}

function SettingsLink({ mobile = false }: { mobile?: boolean }) {
  return (
    <NavLink to="/settings" className={mobile ? mobileNavLinkClass : navLinkClass}>
      <span aria-hidden="true" className={mobile ? 'hidden' : 'text-base leading-none'}>
        ⚙
      </span>
      设置
    </NavLink>
  )
}

export default function App() {
  return (
    <div className="flex h-dvh min-w-0 overflow-hidden bg-slate-100 text-slate-900">
      <aside className="hidden w-64 shrink-0 flex-col border-r border-slate-200 bg-white lg:flex">
        <div className="border-b border-slate-100 px-5 py-5">
          <p className="font-semibold tracking-tight">Gemini 助手</p>
          <p className="mt-1 text-xs text-slate-400">OpenChat 工作区</p>
        </div>
        <div className="flex-1 px-3 py-5">
          <p className="px-3 pb-2 text-[11px] font-semibold uppercase tracking-wider text-slate-400">会话</p>
          <Navigation />
        </div>
        <div className="border-t border-slate-100 px-3 py-4">
          <SettingsLink />
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex min-h-14 shrink-0 items-center justify-between gap-3 border-b border-slate-200 bg-white px-3 sm:px-5 lg:hidden">
          <span className="shrink-0 font-semibold tracking-tight">Gemini 助手</span>
          <div className="flex min-w-0 items-center gap-1 overflow-x-auto">
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
