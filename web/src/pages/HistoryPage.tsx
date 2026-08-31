// Read-only conversation history: the list and the per-conversation
// detail. Retry and login are never offered here (the conversation is
// archived); acknowledging a "result unknown" task is the one action that
// stays available so quarantine can be lifted from anywhere in the UI.
// Conversations with a saved Gemini remote session can be resumed: the
// button reactivates the conversation and jumps to its /chat/:id link.

import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, apiErrorMessage } from '../api'
import { TurnList } from '../components/TurnList'
import { normalizeMarkdown } from '../lib/markdown'
import { providerLabel } from '../lib/provider'
import { Button, Card, ErrorBox, Spinner } from '../components/ui'
import type { Conversation, ConversationDetail, Task } from '../types'

export function HistoryPage() {
  const [items, setItems] = useState<Conversation[] | null>(null)
  const [error, setError] = useState('')
  const [resuming, setResuming] = useState('')
  const navigate = useNavigate()

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

  async function resume(id: string) {
    setResuming(id)
    setError('')
    try {
      await api.resumeConversation(id)
      navigate(`/chat/${id}`)
    } catch (e) {
      setError(apiErrorMessage(e))
    } finally {
      setResuming('')
    }
  }

  if (error) {
    return (
      <div className="mx-auto w-full max-w-3xl px-3 py-4 sm:px-5 sm:py-5 lg:px-8">
        <ErrorBox>{error}</ErrorBox>
      </div>
    )
  }
  if (!items) {
    return (
      <div className="mx-auto flex w-full max-w-3xl items-center justify-center px-3 py-16 text-center sm:px-5 lg:px-8">
        <Spinner label="加载中…" />
      </div>
    )
  }

  return (
    <div className="mx-auto w-full max-w-3xl px-3 py-4 sm:px-5 sm:py-5 lg:px-8">
      <h1 className="mb-5 text-lg font-semibold">历史会话</h1>
      {items.length === 0 ? (
        <Card>
          <p className="text-sm text-slate-500">
            还没有历史会话。去 <Link className="text-sky-600 underline" to="/">当前会话</Link> 开始对话。
          </p>
        </Card>
      ) : (
        <ul className="space-y-2">
          {items.map((c) => (
            <li key={c.id} className="flex flex-wrap items-center gap-3 rounded-xl border border-slate-200 bg-white p-3 text-sm hover:bg-slate-50">
              <Link to={`/history/${c.id}`} className="flex min-w-0 flex-1 basis-64 flex-wrap items-center gap-x-3 gap-y-1">
                <span className="min-w-0 flex-1 truncate text-slate-800">{c.title}</span>
                <span
                  className={
                    (c.provider === 'grok' ? 'bg-sky-100 text-sky-700 ' : 'bg-violet-100 text-violet-700 ') +
                    'shrink-0 rounded-full px-2 py-0.5 text-xs'
                  }
                >
                  {providerLabel(c.provider)}
                </span>
                <span
                  className={
                    c.status === 'active'
                      ? 'shrink-0 rounded-full bg-emerald-100 px-2 py-0.5 text-xs text-emerald-700'
                      : 'shrink-0 rounded-full bg-slate-100 px-2 py-0.5 text-xs text-slate-500'
                  }
                >
                  {c.status === 'active' ? '当前' : '已归档'}
                </span>
                <span className="shrink-0 text-xs text-slate-400">{new Date(c.created).toLocaleString()}</span>
              </Link>
              {c.status === 'archived' && (
                <Button
                  variant="secondary"
                  className="shrink-0"
                  disabled={!c.remote_id || resuming === c.id}
                  title={c.remote_id ? '恢复远端会话并继续对话' : '该会话未保存远端会话，无法续聊'}
                  onClick={() => resume(c.id)}
                >
                  {resuming === c.id ? '恢复中…' : '继续对话'}
                </Button>
              )}
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
  const [copied, setCopied] = useState(false)
  const [resuming, setResuming] = useState(false)
  const navigate = useNavigate()

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

  async function resume() {
    if (!id) return
    setResuming(true)
    setError('')
    try {
      await api.resumeConversation(id)
      navigate(`/chat/${id}`)
    } catch (e) {
      setError(apiErrorMessage(e))
    } finally {
      setResuming(false)
    }
  }

  if (error) {
    return (
      <div className="mx-auto w-full max-w-3xl px-3 py-4 sm:px-5 sm:py-5 lg:px-8">
        <ErrorBox>{error}</ErrorBox>
      </div>
    )
  }
  if (!conv) {
    return (
      <div className="mx-auto flex w-full max-w-3xl items-center justify-center px-3 py-16 text-center sm:px-5 lg:px-8">
        <Spinner label="加载中…" />
      </div>
    )
  }

  async function copyConversation() {
    if (!conv) return
    const lines: string[] = []
    for (const t of conv.turns) {
      lines.push(`User: ${t.prompt}`)
      for (const task of t.tasks) {
        if (task.result) lines.push(`Assistant: ${normalizeMarkdown(task.result)}`)
        else if (task.error_message) lines.push(`Assistant: ${task.error_message}`)
      }
    }
    try {
      await navigator.clipboard.writeText(lines.join('\n\n---\n\n'))
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch { void 0 }
  }

  return (
    <div className="mx-auto w-full max-w-3xl px-3 py-4 sm:px-5 sm:py-5 lg:px-8">
      <div className="mb-5 flex flex-wrap items-start gap-3">
        <h1 className="min-w-0 flex-1 basis-64 truncate text-lg font-semibold">{conv.title}</h1>
        <div className="flex w-full flex-wrap items-center gap-2 sm:w-auto sm:justify-end">
          {conv.remote_id && (
            <Button disabled={resuming} onClick={resume}>
              {resuming ? '恢复中…' : '继续对话'}
            </Button>
          )}
          <Button variant="secondary" onClick={copyConversation}>
            {copied ? '已复制' : '复制会话'}
          </Button>
          <Link to="/history">
            <Button variant="secondary">返回历史</Button>
          </Link>
        </div>
      </div>
      {error && (
        <div className="mb-4">
          <ErrorBox>{error}</ErrorBox>
        </div>
      )}
      <div role="status" className="mb-5 rounded-xl border border-slate-200 bg-slate-50 p-3 text-sm text-slate-700">
        {conv.remote_id
          ? '只读历史：会话已归档。点击「继续对话」恢复远端会话后即可继续提问。'
          : '只读历史：会话已归档，且未保存远端会话，不能继续提问。'}
      </div>
      <TurnList conv={conv} quarantined={false} onAcknowledge={acknowledge} />
    </div>
  )
}
