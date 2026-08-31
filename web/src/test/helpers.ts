// Test helpers: a route-based fetch stub so pages run against an in-memory
// fake of the v1 REST API (never a real backend, never real Gemini).

import { vi } from 'vitest'
import type { Conversation, ConversationDetail, ProviderSnapshot, Task, Turn } from '../types'

export function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

export interface FakeRoute {
  match: (method: string, path: string) => boolean
  handler: (path: string, init: RequestInit) => Response
}

export interface FetchStub {
  /** every request, as "METHOD /path[,METHOD /path...]" */
  calls: string[]
  fn: ReturnType<typeof vi.fn>
}

/** Installs a fetch that routes by method+pathname; returns call logging. */
export function stubFetch(routes: FakeRoute[]): FetchStub {
  const calls: string[] = []
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), 'http://localhost/')
    const method = init?.method ?? 'GET'
    calls.push(`${method} ${url.pathname}`)
    for (const r of routes) {
      if (r.match(method, url.pathname)) return r.handler(url.pathname, init ?? {})
    }
    return jsonResponse({ error: { code: 'not_found', message: 'no route' } }, 404)
  })
  vi.stubGlobal('fetch', fn)
  return { calls, fn }
}

export const m = (method: string, path: string) => (mm: string, p: string) => mm === method && p === path

// ---- fixture builders --------------------------------------------------------

export const ISO = '2026-01-01T00:00:00Z'

export function makeTask(over: Partial<Task>): Task {
  return {
    id: 't1',
    turn: 'tu1',
    requested_model: '',
    resolved_model: '',
    thinking: '',
    status: 'pending',
    result: '',
    error_message: '',
    latency_ms: 0,
    created: ISO,
    ...over,
  }
}

export function makeTurn(over: Partial<Turn> = {}): Turn {
  return {
    id: 'tu1',
    conversation: 'c1',
    prompt: '你好',
    idempotency_key: 'k1',
    created: ISO,
    tasks: [],
    ...over,
  }
}

export function toList(c: Conversation): Conversation {
  return { id: c.id, title: c.title, status: c.status, provider: c.provider, created: ISO }
}

export function makeConversation(id = 'c1', turns: Turn[] = []): ConversationDetail {
  return {
    id,
    title: turns[0]?.prompt.slice(0, 20) ?? '新会话',
    status: 'active',
    provider: 'gemini',
    created: ISO,
    turns,
  }
}

export function makeSnapshot(over: Partial<ProviderSnapshot> = {}): ProviderSnapshot {
  return {
    site: 'gemini',
    model_pick: true,
    thinking_supported: true,
    version: '1.8.7',
    bridge: 'Bridge Extension 1.0.23',
    models: [],
    logged_in: true,
    login_operation: 'idle',
    quarantined: false,
    ...over,
  }
}
