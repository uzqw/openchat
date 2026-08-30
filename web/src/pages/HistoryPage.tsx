// Read-only conversation history: the list and the per-conversation
// detail. Retry and login are never offered here (the conversation is
// archived); acknowledging a "result unknown" task is the one action that
// stays available so quarantine can be lifted from anywhere in the UI.

import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { api, apiErrorMessage } from '../api'
import { TurnList } from '../components/TurnList'
import { Button, Card, ErrorBox, Spinner } from '../components/ui'
import type { Conversation, ConversationDetail, Task } from '../types'

export function HistoryPage() {
  const [items, setItems] = useState<Conversation[] | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    api
      .listConversations(1, 200)
      .then((r) => !cancelled && setItems(r.items))
      .catch((e) => !cancelled && setError(apiErrorMessage(e)))
    return () => {
      cancelled = true
    }
  }, [])

  if (error) {
    return (
      <div className="mx-auto max-w-3xl px-4">
        <ErrorBox>{error}</ErrorBox>
      </div>
    )
  }
  if (!items) {
    return (
      <div className="mx-auto max-w-3xl px-4 py-16 text-center">
        <Spinner label="加载中…" />
      </div>
    )
  }

  return (
    <div className="mx-auto w-full max-w-3xl px-4">
      <h1 className="mb-4 text-lg font-semibold">历史会话</h1>
      {items.length === 0 ? (
        <Card>
          <p className="text-sm text-slate-500">
            还没有历史会话。去 <Link className="text-sky-600 underline" to="/">当前会话</Link> 开始对话。
          </p>
        </Card>
      ) : (
        <ul className="space-y-2">
          {items.map((c) => (
            <li key={c.id}>
              <Link
                to={`/history/${c.id}`}
                className="flex items-center gap-3 rounded-lg border border-slate-200 bg-white p-3 text-sm hover:bg-slate-50"
              >
                <span className="flex-1 truncate text-slate-800">{c.title}</span>
                <span
                  className={
                    c.status === 'active'
                      ? 'rounded-full bg-emerald-100 px-2 py-0.5 text-xs text-emerald-700'
                      : 'rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-500'
                  }
                >
                  {c.status === 'active' ? '当前' : '已归档'}
                </span>
                <span className="text-xs text-slate-400">{new Date(c.created).toLocaleString()}</span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export function HistoryDetailPage() {
  const { id } = useParams<{ id: string }>()
  const [conv, setConv] = useState<ConversationDetail | null>(null)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    if (!id) return
    try {
      setConv(await api.getConversation(id))
    } catch (e) {
      setError(apiErrorMessage(e))
    }
  }, [id])

  useEffect(() => {
    void load()
  }, [load])

  async function acknowledge(task: Task) {
    try {
      await api.acknowledgeUnknown(task.id)
      await load()
    } catch (e) {
      setError(apiErrorMessage(e))
    }
  }

  if (error) {
    return (
      <div className="mx-auto max-w-3xl px-4">
        <ErrorBox>{error}</ErrorBox>
      </div>
    )
  }
  if (!conv) {
    return (
      <div className="mx-auto max-w-3xl px-4 py-16 text-center">
        <Spinner label="加载中…" />
      </div>
    )
  }

  return (
    <div className="mx-auto w-full max-w-3xl px-4">
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-lg font-semibold">{conv.title}</h1>
        <Link to="/history">
          <Button variant="secondary">返回历史</Button>
        </Link>
      </div>
      <div role="status" className="mb-4 rounded-md border border-slate-300 bg-slate-100 p-3 text-sm text-slate-700">
        只读历史：会话已归档，不能继续提问。
      </div>
      <TurnList conv={conv} quarantined={false} onAcknowledge={acknowledge} />
    </div>
  )
}
