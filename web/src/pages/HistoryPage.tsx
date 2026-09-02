// Conversation history: searchable, grouped by day, loaded in pages with a
// load-more button, and a retry button when a load fails. Read-only list;
// the per-conversation detail and resume stay as before.

import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { api, apiErrorMessage } from '../api'
import { TurnList } from '../components/TurnList'
import { normalizeMarkdown } from '../lib/markdown'
import { providerLabel } from '../lib/provider'
import { Button, Card, ErrorBox, Spinner } from '../components/ui'
import type { Conversation, ConversationDetail, Task } from '../types'

const PAGE_SIZE = 50

function dayLabel(d: Date, now: Date): string {
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const day = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  const diff = Math.round((today.getTime() - day.getTime()) / 86_400_000)
  if (diff === 0) return '今天'
  if (diff === 1) return '昨天'
  if (d.getFullYear() === now.getFullYear()) return `${d.getMonth() + 1}月${d.getDate()}日`
  return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日`
}

// Group conversations by calendar day, newest first.
function groupByDay(items: Conversation[], now: Date): { key: string; label: string; items: Conversation[] }[] {
  const byDay = new Map<string, { key: string; label: string; items: Conversation[] }>()
  for (const c of items) {
    const d = new Date(c.created)
    const key = `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`
    const g = byDay.get(key) ?? { key, label: dayLabel(d, now), items: [] }
    g.items.push(c)
    byDay.set(key, g)
  }
  return [...byDay.values()].sort((a, b) => (a.items[0].created < b.items[0].created ? 1 : -1))
}

export function HistoryPage() {
  const [items, setItems] = useState<Conversation[]>([])
  const [page, setPage] = useState(0)
  const [totalItems, setTotalItems] = useState(0)
  const [error, setError] = useState('')
  const [query, setQuery] = useState('')
  const [busy, setBusy] = useState(false)
  const [resuming, setResuming] = useState('')
  const navigate = useNavigate()

  const loadPage = useCallback(async (p: number) => {
    setBusy(true)
    setError('')
    try {
      const r = await api.listConversations(p, PAGE_SIZE)
      setItems((prev) => (p === 1 ? r.items : [...prev, ...r.items]))
      setPage(p)
      setTotalItems(r.totalItems)
    } catch (e) {
      setError(apiErrorMessage(e))
    } finally {
      setBusy(false)
    }
  }, [])

  // StrictMode double-mounts effects in dev; load the first page exactly once
  // so a superseded failed attempt can't leave a stale error over the list.
  const initialLoadRef = useRef(false)
  useEffect(() => {
    if (initialLoadRef.current) return
    initialLoadRef.current = true
    void loadPage(1)
  }, [loadPage])

  // While searching, keep loading pages so the filter covers the whole
  // history, not just what has been browsed so far.
  useEffect(() => {
    if (!query.trim() || busy || items.length >= totalItems) return
    void loadPage(page + 1)
  }, [query, busy, items.length, totalItems, page, loadPage])

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q ? items.filter((c) => c.title.toLowerCase().includes(q)) : items
  }, [items, query])

  const groups = useMemo(() => groupByDay(filtered, new Date()), [filtered])

  const searching = query.trim() !== ''
  const hasMore = items.length < totalItems

  function retry() {
    void loadPage(page === 0 ? 1 : page + 1)
  }

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

  if (busy && page === 0) {
    return (
      <div className="mx-auto flex w-full max-w-3xl items-center justify-center px-3 py-16 text-center sm:px-5 lg:px-8">
        <Spinner label="加载中…" />
      </div>
    )
  }
  if (error && items.length === 0) {
    return (
      <div className="mx-auto w-full max-w-3xl px-3 py-4 sm:px-5 sm:py-5 lg:px-8">
        <h1 className="mb-5 text-lg font-semibold">历史会话</h1>
        <ErrorBox>{error}</ErrorBox>
        <div className="mt-3">
          <Button onClick={retry}>重试</Button>
        </div>
      </div>
    )
  }

  return (
    <div className="mx-auto w-full max-w-3xl px-3 py-4 sm:px-5 sm:py-5 lg:px-8">
      <h1 className="mb-4 text-lg font-semibold">历史会话</h1>
      <input
        type="search"
        aria-label="搜索历史会话"
        placeholder="搜索历史会话"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        className="mb-4 w-full rounded-md border border-line-strong bg-surface px-3 py-2 text-sm text-ink placeholder:text-ink-faint focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
      />
      {error && (
        <div className="mb-4">
          <ErrorBox>{error}</ErrorBox>
          <div className="mt-2">
            <Button variant="secondary" onClick={retry}>
              重试
            </Button>
          </div>
        </div>
      )}
      {items.length === 0 ? (
        <Card>
          <p className="text-sm text-ink-faint">
            还没有历史会话。去 <Link className="text-accent underline" to="/">当前会话</Link> 开始对话。
          </p>
        </Card>
      ) : filtered.length === 0 ? (
        <Card>
          <p className="text-sm text-ink-faint">没有找到匹配「{query.trim()}」的会话。</p>
        </Card>
      ) : (
        <>
          {groups.map((g) => (
            <section key={g.key} aria-label={g.label} className="mb-5">
              <h2 className="mb-2 text-xs font-medium text-ink-faint">{g.label}</h2>
              <ul className="space-y-2">
                {g.items.map((c) => (
                  <li key={c.id} className="flex flex-wrap items-center gap-3 rounded-xl border border-line bg-surface p-3 text-sm hover:bg-subtle">
                    <Link to={`/history/${c.id}`} className="flex min-w-0 flex-1 basis-64 flex-wrap items-center gap-x-3 gap-y-1">
                      <span className="w-full min-w-0 truncate text-ink sm:w-auto sm:flex-1">{c.title}</span>
                      <span className="shrink-0 rounded-full bg-provider-soft px-2 py-0.5 text-xs text-provider-ink">
                        {providerLabel(c.provider)}
                      </span>
                      <span
                        className={
                          c.status === 'active'
                            ? 'shrink-0 rounded-full bg-ok-soft px-2 py-0.5 text-xs text-ok-ink'
                            : 'shrink-0 rounded-full bg-subtle px-2 py-0.5 text-xs text-ink-faint'
                        }
                      >
                        {c.status === 'active' ? '当前' : '已归档'}
                      </span>
                      <span className="shrink-0 text-xs text-ink-faint">
                        {new Date(c.created).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })}
                      </span>
                    </Link>
                    {c.status === 'archived' && (
                      <Button
                        variant="secondary"
                        className="w-full min-h-11 shrink-0 sm:w-auto sm:min-h-9"
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
            </section>
          ))}
          {hasMore && !searching && (
            <div className="flex justify-center pt-1">
              <Button variant="secondary" disabled={busy} onClick={() => void loadPage(page + 1)}>
                {busy ? '加载中…' : '加载更多'}
              </Button>
            </div>
          )}
        </>
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
            <Button disabled={resuming} onClick={resume} className="min-h-11 sm:min-h-9">
              {resuming ? '恢复中…' : '继续对话'}
            </Button>
          )}
          <Button variant="secondary" onClick={copyConversation} className="min-h-11 sm:min-h-9">
            {copied ? '已复制' : '复制会话'}
          </Button>
          <Link to="/history">
            <Button variant="secondary" className="min-h-11 sm:min-h-9">返回历史</Button>
          </Link>
        </div>
      </div>
      {error && (
        <div className="mb-4">
          <ErrorBox>{error}</ErrorBox>
        </div>
      )}
      <div role="status" className="mb-5 rounded-xl border border-line bg-subtle p-3 text-sm text-ink-soft">
        {conv.remote_id
          ? '只读历史：会话已归档。点击「继续对话」恢复远端会话后即可继续提问。'
          : '只读历史：会话已归档，且未保存远端会话，不能继续提问。'}
      </div>
      <TurnList conv={conv} quarantined={false} onAcknowledge={acknowledge} />
    </div>
  )
}
