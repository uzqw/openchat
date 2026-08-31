// Sidebar conversation list: shows conversations directly with a provider
// prefix badge. Active conversations link to /chat/:id (continue chatting),
// archived ones to /history/:id (read-only history). Refetches on route
// change and every 30s so newly created/archived conversations appear.

import { useCallback, useEffect, useState } from 'react'
import { NavLink, useLocation } from 'react-router-dom'
import { api, apiErrorMessage } from '../api'
import type { Conversation } from '../types'

const REFRESH_MS = 30_000

export function ConversationList() {
  const [items, setItems] = useState<Conversation[]>([])
  const [error, setError] = useState('')
  const location = useLocation()

  const load = useCallback(() => {
    api
      .listConversations(1, 100)
      .then((r) => setItems(r.items))
      .catch((e) => setError(apiErrorMessage(e)))
  }, [])

  useEffect(() => {
    load()
    const t = setInterval(load, REFRESH_MS)
    return () => clearInterval(t)
  }, [load, location])

  if (error) return <p className="px-3 py-2 text-xs text-slate-400">{error}</p>
  if (items.length === 0) return <p className="px-3 py-2 text-xs text-slate-400">还没有会话</p>

  return (
    <ul className="space-y-0.5" aria-label="会话列表">
      {items.map((c) => (
        <li key={c.id}>
          <NavLink
            to={c.status === 'active' ? `/chat/${c.id}` : `/history/${c.id}`}
            title={c.title}
            className={({ isActive }) =>
              `flex items-center gap-2 rounded-lg px-3 py-1.5 text-sm transition-colors ${
                isActive ? 'bg-sky-50 font-semibold text-sky-700' : 'text-slate-600 hover:bg-slate-100 hover:text-slate-900'
              }`
            }
          >
            <span
              className={
                (c.provider === 'grok' ? 'bg-sky-100 text-sky-700 ' : 'bg-violet-100 text-violet-700 ') +
                'shrink-0 rounded px-1.5 py-0.5 text-[10px] leading-tight'
              }
            >
              {c.provider === 'grok' ? 'Grok' : 'Gem'}
            </span>
            <span className="min-w-0 flex-1 truncate">{c.title}</span>
            {c.status === 'active' && <span aria-label="当前" className="h-1.5 w-1.5 shrink-0 rounded-full bg-emerald-500" />}
          </NavLink>
        </li>
      ))}
    </ul>
  )
}