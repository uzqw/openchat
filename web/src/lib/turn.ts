// Turn/status helpers shared by the pages: terminal detection, poll
// pacing and the login-wait loop. No component abstraction — just the
// functions the UI needs.

import type { ProviderSnapshot, ProvidersResponse, TaskStatus, Turn } from '../types'
import { api } from '../api'

/**
 * Resolves the snapshot for one site from the /api/providers response,
 * falling back to the default site, then the first entry (quarantine and
 * capabilities are global anyway; login state is what differs).
 */
export function snapshotOf(resp: ProvidersResponse, site: string): ProviderSnapshot {
  return (
    resp.providers.find((p) => p.site === site) ??
    resp.providers.find((p) => p.site === resp.default_site) ??
    resp.providers[0]
  )
}

/** Pace between turn polls. Terminal states (and unmount) stop the loop. */
export const POLL_INTERVAL_MS = 800

export function isTerminal(status: TaskStatus): boolean {
  return (
    status === 'succeeded' ||
    status === 'failed' ||
    status === 'auth_required' ||
    status === 'unknown_outcome' ||
    status === 'canceled'
  )
}

export function hasSuccess(turns: Turn[]): boolean {
  return turns.some((t) => t.tasks.some((task) => task.status === 'succeeded'))
}

/** Sleep that rejects with AbortError when the signal fires. */
export function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const timer = window.setTimeout(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve()
    }, ms)
    function onAbort() {
      window.clearTimeout(timer)
      reject(new DOMException('poll aborted', 'AbortError'))
    }
    if (signal?.aborted) {
      window.clearTimeout(timer)
      reject(new DOMException('poll aborted', 'AbortError'))
      return
    }
    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

/**
 * Polls one turn until its current task reaches a terminal status.
 * onProgress is invoked with each observed turn so the UI can render
 * pending/running progress live. A turn with no tasks or an
 * already-terminal task returns immediately. Aborting (unmount, a newer
 * submit) raises AbortError and stops polling.
 */
export async function pollTurn(
  turnId: string,
  signal: AbortSignal,
  onProgress?: (turn: Turn) => void,
): Promise<Turn> {
  for (;;) {
    const turn = await api.getTurn(turnId, signal)
    onProgress?.(turn)
    const current = turn.current_task
    if (!current || isTerminal(current.status)) return turn
    await sleep(POLL_INTERVAL_MS, signal)
  }
}

export interface LoginOutcome {
  ok: boolean
  message: string
}

/**
 * Requests a login for one site and waits for the operation to reach a
 * terminal state, reporting each snapshot to onSnapshot so the UI can
 * render queued/running/succeeded/failed. Aborts (unmount) raise
 * AbortError.
 */
export async function runLogin(
  site: string,
  onSnapshot: (s: ProviderSnapshot) => void,
  signal: AbortSignal,
): Promise<LoginOutcome> {
  await api.login(site)
  for (;;) {
    const resp = await api.providers(signal)
    const snap = snapshotOf(resp, site)
    onSnapshot(snap)
    if (snap.login_operation === 'succeeded') return { ok: true, message: '登录成功，可以重试提问。' }
    if (snap.login_operation === 'failed') return { ok: false, message: snap.login_message || '登录未完成，请重试。' }
    await sleep(POLL_INTERVAL_MS, signal)
  }
}

/**
 * Requests an on-demand probe refresh (检测在线) and waits for the cache
 * to be refreshed, reporting each snapshot to onSnapshot. Aborts (unmount)
 * raise AbortError. The backend never probes on a background timer — this
 * button is the only way to refresh outside of startup.
 */
export async function runRefresh(
  site: string,
  onSnapshot: (s: ProviderSnapshot) => void,
  signal: AbortSignal,
): Promise<LoginOutcome> {
  const before = snapshotOf(await api.providers(signal), site).refreshed_at
  await api.refresh(site)
  const deadline = Date.now() + 60_000
  for (;;) {
    const snap = snapshotOf(await api.providers(signal), site)
    onSnapshot(snap)
    if (snap.refreshed_at && snap.refreshed_at !== before) {
      return { ok: true, message: '检测完成。' }
    }
    if (Date.now() > deadline) return { ok: false, message: '检测超时，请稍后重试。' }
    await sleep(POLL_INTERVAL_MS, signal)
  }
}
