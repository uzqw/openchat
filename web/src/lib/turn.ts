// Turn/status helpers shared by the pages: terminal detection, poll
// pacing and the login-wait loop. No component abstraction — just the
// functions the UI needs.

import type { ProviderSnapshot, TaskStatus, Turn } from '../types'
import { api } from '../api'

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
 * Requests a Gemini login and waits for the operation to reach a terminal
 * state, reporting each snapshot to onSnapshot so the UI can render
 * queued/running/succeeded/failed. Aborts (unmount) raise AbortError.
 */
export async function runLogin(
  onSnapshot: (s: ProviderSnapshot) => void,
  signal: AbortSignal,
): Promise<LoginOutcome> {
  await api.login()
  for (;;) {
    const snap = await api.snapshot(signal)
    onSnapshot(snap)
    if (snap.login_operation === 'succeeded') return { ok: true, message: '登录成功，可以重试提问。' }
    if (snap.login_operation === 'failed') return { ok: false, message: snap.login_message || '登录未完成，请重试。' }
    await sleep(POLL_INTERVAL_MS, signal)
  }
}
