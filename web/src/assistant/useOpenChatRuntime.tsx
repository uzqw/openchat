import { useCallback, useEffect, useRef, useState } from 'react'
import { useExternalStoreRuntime } from '@assistant-ui/react'
import type { AppendMessage, ThreadMessage } from '@assistant-ui/react'
import { api, apiErrorMessage } from '../api'
import { providerLabel } from '../lib/provider'
import { isTerminal, pollTurn, runLogin } from '../lib/turn'
import { convertConversation } from './convert'
import type { ConversationDetail, ProviderSnapshot, Task } from '../types'

export function useOpenChatRuntime(conversationId?: string) {
  const [messages, setMessages] = useState<ThreadMessage[]>([])
  const [isRunning, setIsRunning] = useState(false)
  const [providers, setProviders] = useState<ProviderSnapshot[]>([])
  const [defaultSite, setDefaultSite] = useState('gemini')
  const [conv, setConv] = useState<ConversationDetail | null>(null)
  const [model, setModel] = useState('')
  const [thinking, setThinking] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [loginHint, setLoginHint] = useState('')
  const convRef = useRef<ConversationDetail | null>(null)
  convRef.current = conv
  const providersRef = useRef<ProviderSnapshot[]>([])
  providersRef.current = providers
  const [nextSite, setNextSite] = useState<string | null>(null)
  const pollRef = useRef<AbortController | null>(null)

  // snapshot of the CURRENT conversation's site (fallback: selected next site or default) —
  // drives model/thinking selectors and the quarantine banner label
  const snapshot: ProviderSnapshot | null = (() => {
    if (providers.length === 0) return null
    const site = conv?.provider || nextSite || defaultSite
    return (
      providers.find((p) => p.site === site) ??
      providers.find((p) => p.site === defaultSite) ??
      providers[0] ??
      null
    )
  })()

  // applySiteSnapshot patches one site's state into the providers list
  // (login/refresh poll loops report per-site snapshots)
  const applySiteSnapshot = useCallback((s: ProviderSnapshot) => {
    setProviders((prev) => (prev.length ? prev.map((p) => (p.site === s.site ? s : p)) : [s]))
  }, [])

  const refreshConversation = useCallback(async (id: string) => {
    const d = await api.getConversation(id)
    setConv(d)
    convRef.current = d
    setMessages(convertConversation(d))
    return d
  }, [])

  // initial load: a pinned conversation id (resumed via /chat/:id) wins,
  // otherwise the single active conversation
  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const [resp, list] = await Promise.all([api.providers(), api.listConversations(1, 200)])
        if (cancelled) return
        setProviders(resp.providers)
        setDefaultSite(resp.default_site)
        let target: ConversationDetail | null = null
        if (conversationId) {
          target = await api.getConversation(conversationId)
        } else {
          const active = list.items.find((item) => item.status === 'active')
          target = active ? await api.getConversation(active.id) : null
        }
        if (cancelled) return
        if (target) {
          setConv(target)
          setMessages(convertConversation(target))
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
  }, [conversationId])

  const reloadSnapshot = useCallback(async () => {
    try {
      const resp = await api.providers()
      setProviders(resp.providers)
      setDefaultSite(resp.default_site)
      return resp
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
      if (providersRef.current.some((p) => p.quarantined)) {
        setError('当前站点已隔离：请先确认 Chrome 已空闲')
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
          const siteForNew = nextSite || defaultSite
          const created = await api.createConversation(siteForNew)
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
    const site = convRef.current?.provider || defaultSite
    try {
      const outcome = await runLogin(
        site,
        (snap) => {
          applySiteSnapshot(snap)
          setLoginHint(
            snap.login_operation === 'running'
              ? `请在可见 Chrome 中完成 ${providerLabel(snap.site)} 登录…`
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
  }, [refreshConversation, reloadSnapshot, applySiteSnapshot, defaultSite])

  const newConversation = useCallback(async (provider?: string) => {
    stopPolling()
    setBusy(true)
    setError('')
    setLoginHint('')
    try {
      const created = await api.createConversation(provider ?? defaultSite)
      const d = await api.getConversation(created.id)
      setConv(d)
      convRef.current = d
      setMessages(convertConversation(d))
      return d
    } catch (e) {
      setError(apiErrorMessage(e))
      return null
    } finally {
      setBusy(false)
    }
  }, [defaultSite])

  const resumeConversation = useCallback(async (id: string) => {
    stopPolling()
    setBusy(true)
    setError('')
    setLoginHint('')
    try {
      const c = await api.resumeConversation(id)
      const d = await api.getConversation(c.id)
      setConv(d)
      convRef.current = d
      setMessages(convertConversation(d))
      return d
    } catch (e) {
      setError(apiErrorMessage(e))
      return null
    } finally {
      setBusy(false)
    }
  }, [])

  const onCancel = useCallback(async () => {
    stopPolling()
    setIsRunning(false)
    const c = convRef.current
    if (c) {
      const ids: string[] = []
      for (const turn of c.turns) for (const task of turn.tasks) if (task.status === 'running' || task.status === 'pending') ids.push(task.id)
      for (const id of ids) {
        try { await api.cancelTask(id) } catch { void 0 }
      }
      try { await refreshConversation(c.id) } catch { void 0 }
      await reloadSnapshot()
    }
    setBusy(false)
  }, [refreshConversation, reloadSnapshot])

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
    providers,
    defaultSite,
    setNextSite,
    retry,
    acknowledge,
    startLogin,
    newConversation,
    resumeConversation,
    reloadSnapshot,
  }
}
