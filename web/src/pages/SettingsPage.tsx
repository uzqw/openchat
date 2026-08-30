// Gemini connection settings: backend/Bridge/login state, the model list
// and the "go login" action. Operations that would navigate or change the
// shared OpenCLI tab (login) are disabled while a quarantined state, a
// write guard or an already-successful active conversation makes them
// unsafe — the backend enforces the same rule, the UI just mirrors it.

import { useEffect, useRef, useState } from 'react'
import { api, apiErrorMessage, isAbort } from '../api'
import { Button, Card, ErrorBox, Spinner } from '../components/ui'
import { hasSuccess, runLogin } from '../lib/turn'
import type { ProviderSnapshot } from '../types'

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-slate-100 py-2 last:border-0">
      <span className="text-sm text-slate-500">{label}</span>
      <span className="min-w-0 break-words text-right text-sm font-medium text-slate-800">{value}</span>
    </div>
  )
}

const loginOpLabel: Record<string, string> = {
  idle: '空闲',
  queued: '排队中',
  running: '进行中',
  succeeded: '成功',
  failed: '失败',
}

export function SettingsPage() {
  const [snap, setSnap] = useState<ProviderSnapshot | null>(null)
  const [loginBlockedByActive, setLoginBlockedByActive] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [loginHint, setLoginHint] = useState('')
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    let cancelled = false
    ;(async () => {
      try {
        const s = await api.snapshot()
        if (cancelled) return
        setSnap(s)
        // mirror the backend gate: an active conversation that already has
        // a successful turn must never navigate the shared OpenCLI tab
        const list = await api.listConversations(1, 1)
        if (list.items[0]?.status === 'active') {
          const d = await api.getConversation(list.items[0].id)
          if (!cancelled) setLoginBlockedByActive(hasSuccess(d.turns))
        } else if (!cancelled) {
          setLoginBlockedByActive(false)
        }
      } catch (e) {
        if (!cancelled) setError(apiErrorMessage(e))
      }
    })()
    return () => {
      mounted.current = false
      cancelled = true
    }
  }, [])

  async function startLogin() {
    if (busy) return
    setBusy(true)
    setError('')
    setLoginHint('登录请求已排队…')
    const ac = new AbortController()
    try {
      const outcome = await runLogin(
        (s) => {
          setSnap(s)
          setLoginHint(
            s.login_operation === 'running'
              ? '请在可见 Chrome 中完成 Gemini 登录…'
              : s.login_operation === 'queued'
                ? '登录已排队，等待执行…'
                : '',
          )
        },
        ac.signal,
      )
      if (!mounted.current) return
      setLoginHint(outcome.message)
      setSnap(await api.snapshot())
    } catch (e) {
      if (!isAbort(e)) setError(apiErrorMessage(e))
    } finally {
      if (mounted.current) setBusy(false)
    }
  }

  if (!snap && !error) {
    return (
      <div className="mx-auto flex w-full max-w-3xl items-center justify-center px-3 py-16 text-center sm:px-5 lg:px-8">
        <Spinner label="加载中…" />
      </div>
    )
  }

  const loginDisabled =
    busy || (snap?.quarantined ?? false) || !!snap?.write_blocked || loginBlockedByActive

  return (
    <div className="mx-auto w-full max-w-3xl px-3 py-4 sm:px-5 sm:py-5 lg:px-8">
      <h1 className="mb-5 text-lg font-semibold">设置</h1>
      {error && (
        <div className="mb-4">
          <ErrorBox>{error}</ErrorBox>
        </div>
      )}
      {snap && (
        <div className="space-y-4">
          <Card>
            <h2 className="mb-1 text-sm font-semibold text-slate-700">后端</h2>
            <Field label="OPENCLI 版本" value={snap.version || '未知'} />
            <Field label="Browser Bridge" value={snap.bridge || '未知'} />
            <Field label="登录状态" value={snap.logged_in ? '已登录' : '未登录'} />
          </Card>

          <Card>
            <h2 className="mb-1 text-sm font-semibold text-slate-700">Gemini</h2>
            <Field label="隔离" value={snap.quarantined ? '是（存在未确认的结果）' : '否'} />
            <Field label="登录操作" value={loginOpLabel[snap.login_operation] ?? snap.login_operation} />
            {snap.login_message && <Field label="登录信息" value={snap.login_message} />}
            {snap.write_blocked && <Field label="写入被阻止" value={snap.write_blocked} />}
            <div className="mt-3">
              {loginBlockedByActive && (
                <p className="mb-2 text-sm text-slate-500">
                  当前会话已有成功回答；为避免改动共享标签页，登录入口已禁用。结束后可登录。
                </p>
              )}
              {snap.quarantined && !snap.write_blocked && (
                <p className="mb-2 text-sm text-amber-700">Gemini 已隔离：请先到对应会话确认 Chrome 已空闲。</p>
              )}
              <Button disabled={loginDisabled} onClick={() => void startLogin()}>
                去登录
              </Button>
              {loginHint && <p className="mt-2 break-words text-sm text-sky-700">{loginHint}</p>}
            </div>
          </Card>

          <Card>
            <h2 className="mb-1 text-sm font-semibold text-slate-700">可用模型</h2>
            {snap.models.length === 0 ? (
              <p className="text-sm text-slate-500">
                尚未获取模型列表（沿用网站当前模型；缓存由后端在空闲时刷新）。
              </p>
            ) : (
              <ul className="space-y-1">
                {snap.models.map((m) => (
                  <li key={m} className="break-words text-sm text-slate-700">
                    {m}
                  </li>
                ))}
              </ul>
            )}
          </Card>
        </div>
      )}
    </div>
  )
}
