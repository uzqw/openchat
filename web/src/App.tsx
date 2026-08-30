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
  `text-sm transition-colors ${isActive ? 'font-semibold text-sky-700' : 'text-slate-600 hover:text-slate-900'}`

export default function App() {
  return (
    <div className="min-h-screen bg-slate-100 text-slate-900">
      <header className="border-b border-slate-200 bg-white">
        <nav className="mx-auto flex max-w-5xl items-center gap-6 px-4 py-3" aria-label="主导航">
          <span className="font-semibold">Gemini 助手</span>
          <NavLink to="/" className={navLinkClass} end>
            当前会话
          </NavLink>
          <NavLink to="/history" className={navLinkClass}>
            历史
          </NavLink>
          <NavLink to="/settings" className={navLinkClass}>
            设置
          </NavLink>
        </nav>
      </header>
      <main className="py-6">
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
  )
}
