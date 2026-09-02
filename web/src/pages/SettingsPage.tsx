// Provider connection settings: per-site backend/Bridge/login state, the
// model list and the "go login" actions. Operations that would navigate or
// change the shared OpenCLI tab (login) are disabled while a quarantined
// state, a write guard or an already-successful active conversation makes
// them unsafe — the backend enforces the same rule, the UI just mirrors it.

import { useCallback, useEffect, useRef, useState } from 'react'
import { api, apiErrorMessage, isAbort } from '../api'
import { Button, Card, ErrorBox, Skeleton } from '../components/ui'
import { providerLabel } from '../lib/provider'
import { hasSuccess, runLogin, runRefresh } from '../lib/turn'
import type { ProviderSnapshot } from '../types'

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1 border-b border-line py-2 last:border-0">
      <span className="text-sm text-ink-faint">{label}</span>
      <span className="min-w-0 break-words text-right text-sm font-medium text-ink">{value}</span>
    </div>
  )
}

const loginOpLabel: Record<string, string> = {
  idle: '未开始',
  queued: '排队中',
  running: '进行中',
  succeeded: '已完成',
  failed: '失败',
}

// write-guard reason codes (internal/provider/guard.go) mapped to plain
// language: the guard fails closed so a modified local setup never runs a
// real write against the user's account.
const writeBlockedLabel: Record<string, string> = {
  adapter_override: '已启用（检测到本地配置覆盖）',
  plugin_installed: '已启用（检测到已安装的插件）',
  version_mismatch: '已启用（程序版本不匹配）',
}

export function SettingsPage() {
  const [providers, setProviders] = useState<ProviderSnapshot[]>([])
  const [busySite, setBusySite] = useState('')
  const [loginBlockedByActive, setLoginBlockedByActive] = useState(false)
  const [error, setError] = useState('')
  const [hints, setHints] = useState<Record<string, string>>({})
  const mounted = useRef(true)

  // patchSite merges one site's fresh snapshot into the list.
  function patchSite(s: ProviderSnapshot) {
    setProviders((prev) => (prev.length ? prev.map((p) => (p.site === s.site ? s : p)) : [s]))
  }

  // initial load, extracted so a failed load can be retried (leg 6);
  // isCancelled guards the mount effect's cleanup
  const load = useCallback(async (isCancelled?: () => boolean) => {
    setError('')
    try {
      const resp = await api.providers()
      if (isCancelled?.()) return
      setProviders(resp.providers)
      // mirror the backend gate: an active conversation that already has
      // a successful turn must never navigate the shared OpenCLI tab
      const list = await api.listConversations(1, 1)
      if (list.items[0]?.status === 'active') {
        const d = await api.getConversation(list.items[0].id)
        if (!isCancelled?.()) setLoginBlockedByActive(hasSuccess(d.turns))
      } else if (!isCancelled?.()) {
        setLoginBlockedByActive(false)
      }
    } catch (e) {
      if (!isCancelled?.()) setError(apiErrorMessage(e))
    }
  }, [])

  useEffect(() => {
    mounted.current = true
    let cancelled = false
    void load(() => cancelled)
    return () => {
      mounted.current = false
      cancelled = true
    }
  }, [load])

  async function startLogin(site: string) {
    if (busySite) return
    setBusySite(site)
    setError('')
    setHints((h) => ({ ...h, [site]: '登录请求已排队…' }))
    const ac = new AbortController()
    try {
      const outcome = await runLogin(
        site,
        (s) => {
          patchSite(s)
          setHints((h) => ({
            ...h,
            [s.site]:
              s.login_operation === 'running'
                ? `请在打开的浏览器窗口中完成 ${providerLabel(s.site)} 登录…`
                : s.login_operation === 'queued'
                  ? '登录已排队，等待执行…'
                  : '',
          }))
        },
        ac.signal,
      )
      if (!mounted.current) return
      setHints((h) => ({ ...h, [site]: outcome.message }))
      const resp = await api.providers()
      setProviders(resp.providers)
    } catch (e) {
      if (!isAbort(e)) setError(apiErrorMessage(e))
    } finally {
      if (mounted.current) setBusySite('')
    }
  }

  async function startRefresh(site: string) {
    if (busySite) return
    setBusySite(site)
    setError('')
    setHints((h) => ({ ...h, [site]: `正在检测 ${providerLabel(site)} 在线状态…` }))
    const ac = new AbortController()
    try {
      const outcome = await runRefresh(
        site,
        (s) => {
          patchSite(s)
        },
        ac.signal,
      )
      if (!mounted.current) return
      setHints((h) => ({ ...h, [site]: outcome.message }))
      const resp = await api.providers()
      setProviders(resp.providers)
    } catch (e) {
      if (!isAbort(e)) setError(apiErrorMessage(e))
    } finally {
      if (mounted.current) setBusySite('')
    }
  }

  if (providers.length === 0 && !error) {
    return (
      <div
        role="status"
        aria-label="加载中"
        className="mx-auto w-full max-w-3xl px-3 py-4 sm:px-5 sm:py-5 lg:px-8"
      >
        {/* skeleton of the loaded settings: title, provider card, model card */}
        <Skeleton className="mb-5 h-7 w-16" />
        <div className="space-y-4">
          <div className="rounded-lg border border-line bg-surface p-4 shadow-sm">
            <Skeleton className="mb-3 h-4 w-24" />
            {Array.from({ length: 4 }, (_, i) => (
              <Skeleton key={i} className="mb-2 h-5 w-full" />
            ))}
            <Skeleton className="mt-3 h-9 w-24" />
          </div>
          <div className="rounded-lg border border-line bg-surface p-4 shadow-sm">
            <Skeleton className="mb-3 h-4 w-28" />
            <Skeleton className="h-5 w-full" />
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="mx-auto w-full max-w-3xl px-3 py-4 sm:px-5 sm:py-5 lg:px-8">
      <h1 className="mb-5 text-lg font-semibold">设置</h1>
      {error && (
        <div className="mb-4">
          <ErrorBox>{error}</ErrorBox>
          <div className="mt-2">
            <Button variant="secondary" onClick={() => void load()}>
              重试
            </Button>
          </div>
        </div>
      )}
      {providers.map((snap) => {
        const label = providerLabel(snap.site)
        const loginDisabled =
          busySite !== '' || snap.quarantined || !!snap.write_blocked || loginBlockedByActive || snap.logged_in
        return (
          <div key={snap.site} className="space-y-4">
            <Card>
              <h2 className="mb-1 text-sm font-semibold text-ink-soft">{label}</h2>
              <Field label="程序版本" value={snap.version || '未知'} />
              <Field label="浏览器连接" value={snap.bridge || '未知'} />
              <Field label="登录状态" value={snap.logged_in ? '已登录' : '未登录'} />
              <Field label="暂停状态" value={snap.quarantined ? '已暂停（存在未确认的结果）' : '正常'} />
              <Field label="登录进度" value={loginOpLabel[snap.login_operation] ?? snap.login_operation} />
              {snap.login_message && <Field label="登录信息" value={snap.login_message} />}
              {snap.write_blocked && <Field label="写入保护" value={writeBlockedLabel[snap.write_blocked] ?? '已启用'} />}
              <div className="mt-3">
                {loginBlockedByActive && (
                  <p className="mb-2 text-sm text-ink-faint">
                    当前会话已有成功回答；为避免影响当前会话，登录入口已禁用。结束后可登录。
                  </p>
                )}
                {snap.logged_in && (
                  <p className="mb-2 text-sm text-ink-faint">当前已登录，无需登录。</p>
                )}
                {snap.quarantined && !snap.write_blocked && (
                  <p className="mb-2 text-sm text-warn-ink">{label} 已暂停：请先到对应会话确认浏览器已停止生成。</p>
                )}
                <Button disabled={loginDisabled} onClick={() => void startLogin(snap.site)}>
                  去登录
                </Button>
                <Button disabled={busySite !== ''} variant="secondary" onClick={() => void startRefresh(snap.site)}>
                  检测在线
                </Button>
                {hints[snap.site] && <p className="mt-2 break-words text-sm text-accent-strong">{hints[snap.site]}</p>}
              </div>
            </Card>

            <Card>
              <h2 className="mb-1 text-sm font-semibold text-ink-soft">可用模型（{label}）</h2>
              {snap.models.length === 0 ? (
                <p className="text-sm text-ink-faint">尚未获取模型列表（沿用网站当前模型；列表会自动更新）。</p>
              ) : (
                <ul className="space-y-1">
                  {snap.models.map((m) => (
                    <li key={m} className="break-words text-sm text-ink-soft">
                      {m}
                    </li>
                  ))}
                </ul>
              )}
            </Card>
          </div>
        )
      })}
    </div>
  )
}
