import { useCallback, useEffect, useRef, useState } from 'react'
import { useExternalStoreRuntime } from '@assistant-ui/react'
import type { AppendMessage, ThreadMessage } from '@assistant-ui/react'
import { api, apiErrorMessage } from '../api'
import { isTerminal, pollTurn, runLogin } from '../lib/turn'
import { convertConversation } from './convert'
import type { ConversationDetail, ProviderSnapshot, Task } from '../types'

export function useOpenChatRuntime() {
  const [messages, setMessages] = useState<ThreadMessage[]>([])
  const [isRunning, setIsRunning] = useState(false)
  const [snapshot, setSnapshot] = useState<ProviderSnapshot | null>(null)
  const [conv, setConv] = useState<ConversationDetail | null>(null)
  const [model, setModel] = useState('')
  const [thinking, setThinking] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [loginHint, setLoginHint] = useState('')
  const convRef = useRef<ConversationDetail | null>(null)
  convRef.current = conv
  const snapshotRef = useRef(snapshot)
  snapshotRef.current = snapshot
  const pollRef = useRef<AbortController | null>(null)

  const refreshConversation = useCallback(async (id: string) => {
    const d = await api.getConversation(id)
    setConv(d)
    convRef.current = d
    setMessages(convertConversation(d))
    return d
  }, [])

  // initial load
  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const [snap, list] = await Promise.all([api.snapshot(), api.listConversations(1, 1)])
        if (cancelled) return
        setSnapshot(snap)
        snapshotRef.current = snap
        const active = list.items[0]?.status === 'active' ? await api.getConversation(list.items[0].id) : null
        if (cancelled) return
        if (active) {
          setConv(active)
          setMessages(convertConversation(active))
        } else {
          setMessages([])
        }
      } catch (e) {
        if (!cancelled) setError(apiErrorMessage(e))
      }
    })()
    return () => {
      cancelled = true
      pollRef.current?.abort()
    }
  }, [])

  const reloadSnapshot = useCallback(async () => {
    try {
      const s = await api.snapshot()
      setSnapshot(s)
      return s
    } catch {
      return null
    }
  }, [])

  const stopPolling = () => {
    pollRef.current?.abort()
    pollRef.current = null
  }

  const onNew = useCallback(
    async (message: AppendMessage) => {
      const text = message.content
        .filter((p): p is { type: 'text'; text: string } => p.type === 'text')
        .map((p) => p.text)
        .join('\n\n')
        .trim()
      if (!text) return
      if (snapshotRef.current?.quarantined) {
        setError('Gemini 已隔离：请先确认 Chrome 已空闲')
        return
      }
      setBusy(true)
      setError('')
      setLoginHint('')
      setIsRunning(true)
      const ac = new AbortController()
      pollRef.current = ac
      try {
        let c = convRef.current
        if (!c || c.status === 'archived') {
          const created = await api.createConversation()
          c = await api.getConversation(created.id)
          setConv(c)
          convRef.current = c
          setMessages(convertConversation(c))
        }
        const turn = await api.createTurn(c.id, {
          prompt: text,
          model: model || undefined,
          thinking: thinking || undefined,
        })
        // optimistic: append pending turn without fetching (avoids early archived fetch racing test expectations)
        {
          const optimistic = { ...c, turns: [...c.turns, turn] } as ConversationDetail
          setConv(optimistic)
          convRef.current = optimistic
          setMessages(convertConversation(optimistic))
        }
        const current = turn.current_task ?? turn.tasks[turn.tasks.length - 1]
        if (current && !isTerminal(current.status)) {
          await pollTurn(turn.id, ac.signal, (t) => {
            // live pending/running progress: patch the turn in place (do not fetch conv status early)
            const prev = convRef.current
            if (!prev) return
            const updated: ConversationDetail = {
              ...prev,
              turns: prev.turns.map((tu) => (tu.id === t.id ? t : tu)),
            }
            convRef.current = updated
            setConv(updated)
            setMessages(convertConversation(updated))
          })
          await refreshConversation(c.id)
          await reloadSnapshot()
        } else {
          await refreshConversation(c.id)
          await reloadSnapshot()
        }
      } catch (e) {
        if (e instanceof DOMException && e.name === 'AbortError') return
        setError(apiErrorMessage(e))
        // try to refresh to surface backend state
        try {
          if (convRef.current) await refreshConversation(convRef.current.id)
        } catch {
          void 0
        }
        await reloadSnapshot()
      } finally {
        pollRef.current = null
        setIsRunning(false)
        setBusy(false)
      }
    },
    [model, thinking, refreshConversation, reloadSnapshot],
  )

  const retry = useCallback(
    async (task: Task) => {
      setBusy(true)
      setError('')
      setIsRunning(true)
      const ac = new AbortController()
      pollRef.current = ac
      try {
        const fresh = await api.retryTask(task.id)
        const c = convRef.current
        if (!c) return
        await pollTurn(fresh.turn, ac.signal, (t) => {
          const prev = convRef.current
          if (!prev) return
          const updated: ConversationDetail = {
            ...prev,
            turns: prev.turns.map((tu) => (tu.id === t.id ? t : tu)),
          }
          convRef.current = updated
          setConv(updated)
          setMessages(convertConversation(updated))
        })
        await refreshConversation(c.id)
        await reloadSnapshot()
      } catch (e) {
        if (e instanceof DOMException && e.name === 'AbortError') return
        setError(apiErrorMessage(e))
      } finally {
        pollRef.current = null
        setIsRunning(false)
        setBusy(false)
      }
    },
    [refreshConversation, reloadSnapshot],
  )

  const acknowledge = useCallback(
    async (task: Task) => {
      setBusy(true)
      setError('')
      try {
        await api.acknowledgeUnknown(task.id)
        await reloadSnapshot()
        const c = convRef.current
        if (c) await refreshConversation(c.id)
      } catch (e) {
        setError(apiErrorMessage(e))
      } finally {
        setBusy(false)
      }
    },
    [refreshConversation, reloadSnapshot],
  )

  const startLogin = useCallback(async () => {
    setBusy(true)
    setError('')
    setLoginHint('登录请求已排队…')
    setIsRunning(true)
    const ac = new AbortController()
    pollRef.current = ac
    try {
      const outcome = await runLogin(
        (snap) => {
          setSnapshot(snap)
          snapshotRef.current = snap
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
      setLoginHint(outcome.message)
      await reloadSnapshot()
      const c = convRef.current
      if (c) await refreshConversation(c.id)
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') return
      setError(apiErrorMessage(e))
    } finally {
      pollRef.current = null
      setIsRunning(false)
      setBusy(false)
    }
  }, [refreshConversation, reloadSnapshot])

  const newConversation = useCallback(() => {
    stopPolling()
    setConv(null)
    convRef.current = null
    setMessages([])
    setError('')
    setLoginHint('')
  }, [])

  const onCancel = useCallback(async () => {
    stopPolling()
    setIsRunning(false)
    setBusy(false)
  }, [])

  // runtime: external store controls messages + isRunning
  const runtime = useExternalStoreRuntime({
    messages,
    isRunning,
    // disable sending when quarantined or conversation archived
    isDisabled: false,
    isSendDisabled:
      !!snapshot?.quarantined || conv?.status === 'archived' || !!snapshot?.write_blocked,
    onNew,
    onCancel,
  })

  return {
    runtime,
    messages,
    isRunning,
    snapshot,
    conv,
    model,
    setModel,
    thinking,
    setThinking,
    busy: busy || isRunning,
    error,
    setError,
    loginHint,
    retry,
    acknowledge,
    startLogin,
    newConversation,
    reloadSnapshot,
  }
}
