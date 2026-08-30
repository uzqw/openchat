// Current Gemini session. Loads the active conversation (the newest one —
// creating a new conversation archives the previous one), submits turns
// with model/thinking selection, polls the turn until a terminal state or
// unmount, and renders every terminal state with its actions.

import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, apiErrorMessage, isAbort } from '../api'
import { TurnList } from '../components/TurnList'
import { Button, Card, ErrorBox, Label, Select, Spinner, Textarea } from '../components/ui'
import { isTerminal, pollTurn, runLogin } from '../lib/turn'
import type { ConversationDetail, ProviderSnapshot, Task, Turn } from '../types'

const THINKING_OPTIONS = [
  { value: '', label: '不改变网站当前值' },
  { value: 'standard', label: 'standard' },
  { value: 'extended', label: 'extended' },
]

export function ChatPage() {
  const [snapshot, setSnapshot] = useState<ProviderSnapshot | null>(null)
  const [conv, setConv] = useState<ConversationDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [input, setInput] = useState('')
  const [model, setModel] = useState('')
  const [thinking, setThinking] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [loginHint, setLoginHint] = useState('')

  const pollRef = useRef<AbortController | null>(null)
  const mounted = useRef(true)
  const convRef = useRef<ConversationDetail | null>(null)
  convRef.current = conv

  const stopPolling = () => {
    pollRef.current?.abort()
    pollRef.current = null
  }

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      stopPolling()
    }
  }, [])

  // The frontend never walks the queue or pokes OpenCLI directly: find
  // the current session through the read-only list/detail endpoints.
  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const [snap, list] = await Promise.all([api.snapshot(), api.listConversations(1, 1)])
        if (cancelled) return
        setSnapshot(snap)
        setConv(list.items[0]?.status === 'active' ? await api.getConversation(list.items[0].id) : null)
      } catch (e) {
        if (!cancelled) setError(apiErrorMessage(e))
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const newConversation = () => {
    stopPolling()
    setConv(null)
    setError('')
    setLoginHint('')
  }

  async function submit() {
    if (busy) return
    const prompt = input.trim()
    if (!prompt) return
    stopPolling()
    setBusy(true)
    setError('')
    setLoginHint('')
    try {
      const c = convRef.current ?? (await api.createConversation())
      const turn = await api.createTurn(c.id, {
        prompt,
        model: model || undefined,
        thinking: thinking || undefined,
      })
      if (!mounted.current) return
      setInput('')
      setConv(await api.getConversation(c.id))
      const current = turn.current_task ?? turn.tasks[turn.tasks.length - 1]
      if (current && !isTerminal(current.status)) {
        const ac = new AbortController()
        pollRef.current = ac
        await pollTurn(turn.id, ac.signal, (t) => applyTurnProgress(t))
        if (!mounted.current) return
        setConv(await api.getConversation(c.id))
      }
    } catch (e) {
      if (!isAbort(e)) setError(apiErrorMessage(e))
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  /** Live pending/running progress from the poll loop replaces the turn. */
  function applyTurnProgress(t: Turn) {
    setConv((prev) =>
      prev ? { ...prev, turns: prev.turns.map((tu) => (tu.id === t.id ? t : tu)) } : prev,
    )
  }

  async function retry(task: Task) {
    if (busy) return
    stopPolling()
    setBusy(true)
    setError('')
    try {
      const fresh = await api.retryTask(task.id)
      const c = convRef.current
      if (!c) return
      const ac = new AbortController()
      pollRef.current = ac
      await pollTurn(fresh.turn, ac.signal, (t) => applyTurnProgress(t))
      if (!mounted.current) return
      setConv(await api.getConversation(c.id))
    } catch (e) {
      if (!isAbort(e)) setError(apiErrorMessage(e))
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  async function startLogin() {
    if (busy) return
    stopPolling()
    setBusy(true)
    setError('')
    setLoginHint('登录请求已排队…')
    let ac: AbortController | null = null
    try {
      ac = new AbortController()
      pollRef.current = ac
      const outcome = await runLogin(
        (snap) => {
          setSnapshot(snap)
          setLoginHint(
            snap.login_operation === 'running'
              ? '请在可见 Chrome 中完成 Gemini 登录…'
              : snap.login_operation === 'queued'
                ? '登录已排队，等待执行…'
                : '',
          )
        },
        ac.signal,
      )
      if (!mounted.current) return
      setLoginHint(outcome.message)
      setSnapshot(await api.snapshot())
    } catch (e) {
      if (!isAbort(e)) setError(apiErrorMessage(e))
    } finally {
      if (ac) pollRef.current = null
      if (mounted.current) setBusy(false)
    }
  }

  async function acknowledge(task: Task) {
    if (busy) return
    setBusy(true)
    setError('')
    try {
      await api.acknowledgeUnknown(task.id)
      if (!mounted.current) return
      setSnapshot(await api.snapshot())
      const c = convRef.current
      if (c) setConv(await api.getConversation(c.id))
    } catch (e) {
      if (!isAbort(e)) setError(apiErrorMessage(e))
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  if (loading) {
    return (
      <div className="mx-auto max-w-3xl px-4 py-16 text-center">
        <Spinner label="加载中…" />
      </div>
    )
  }

  const quarantined = snapshot?.quarantined ?? false
  const archived = conv?.status === 'archived'
  const inputDisabled = busy || archived || quarantined
  const submitHint = quarantined
    ? 'Gemini 已隔离：请先在历史记录中找到结果未知的任务，确认 Chrome 已空闲。'
    : undefined

  return (
    <div className="mx-auto w-full max-w-3xl px-4">
      {archived && (
        <div role="status" className="mb-4 rounded-md border border-slate-300 bg-slate-100 p-3 text-sm text-slate-700">
          只读历史：该会话已归档，不能继续提问。
          <Button className="ml-3" variant="secondary" onClick={newConversation}>
            新建会话
          </Button>
        </div>
      )}

      {error && (
        <div className="mb-4">
          <ErrorBox>{error}</ErrorBox>
        </div>
      )}

      {conv ? (
        <TurnList
          conv={conv}
          quarantined={quarantined}
          busy={busy}
          loginHint={loginHint}
          onRetry={retry}
          onAcknowledge={acknowledge}
          onLogin={startLogin}
        />
      ) : (
        <Card className="py-10 text-center text-sm text-slate-500">
          {quarantined ? (
            <div className="space-y-2">
              <p>Gemini 已隔离，暂时无法创建新会话。</p>
              <p>
                <Link className="text-sky-600 underline" to="/history">
                  前往历史记录确认 Chrome 已空闲
                </Link>
              </p>
            </div>
          ) : (
            <p>
              还没有会话。输入问题开始对话。
              {snapshot && snapshot.models.length === 0 && '（模型列表尚未获取，将沿用网站当前模型。）'}
            </p>
          )}
        </Card>
      )}

      {submitHint && (
        <p role="status" className="mb-2 text-sm text-amber-700">
          {submitHint}
        </p>
      )}

      <form
        className="mt-6"
        onSubmit={(e) => {
          e.preventDefault()
          void submit()
        }}
      >
        <Label htmlFor="prompt-input">消息</Label>
        <Textarea
          id="prompt-input"
          rows={4}
          value={input}
          disabled={inputDisabled}
          placeholder={quarantined ? 'Gemini 已隔离，无法发送' : '输入问题，Enter 发送，Shift+Enter 换行'}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault()
              void submit()
            }
          }}
        />
        <div className="mt-3 flex flex-wrap items-center gap-4">
          <div>
            <Label htmlFor="model-select">模型</Label>
            <Select id="model-select" value={model} disabled={busy} onChange={(e) => setModel(e.target.value)}>
              <option value="">沿用当前模型（默认）</option>
              {(snapshot?.models ?? []).map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </Select>
          </div>
          <div>
            <Label htmlFor="thinking-select">思考模式</Label>
            <Select id="thinking-select" value={thinking} disabled={busy} onChange={(e) => setThinking(e.target.value)}>
              {THINKING_OPTIONS.map((o) => (
                <option key={o.value} value={o.value}>
                  {o.label}
                </option>
              ))}
            </Select>
          </div>
          <div className="ml-auto pt-5">
            <Button type="submit" disabled={busy || inputDisabled || input.trim() === ''}>
              {busy ? '处理中…' : '发送'}
            </Button>
          </div>
        </div>
      </form>
    </div>
  )
}
