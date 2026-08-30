// Thin typed client for the v1 REST API. The backend enforces Basic Auth
// (browser prompt / dev no-auth on loopback) and Idempotency-Key; every
// write goes through fetch on the same origin.

import type {
  Conversation,
  ConversationDetail,
  LoginAck,
  Paginated,
  ProviderSnapshot,
  Task,
  Turn,
  TurnRequest,
} from './types'

/** Parsed error from the unified envelope `{"error":{"code","message"}}`. */
export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export function apiErrorMessage(e: unknown): string {
  return e instanceof ApiError ? e.message : '网络或服务器错误，请稍后重试'
}

export function isAbort(e: unknown): boolean {
  return e instanceof DOMException && e.name === 'AbortError'
}

const JSON_HEADERS = { Accept: 'application/json', 'Content-Type': 'application/json' }

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(path, { ...init, headers: { ...JSON_HEADERS, ...init.headers } })
  if (res.status === 204) return undefined as T
  const body: unknown = await res.json().catch(() => null)
  if (!res.ok) {
    const err = (body as { error?: { code?: string; message?: string } } | null)?.error
    throw new ApiError(res.status, err?.code ?? 'unknown', err?.message ?? `请求失败（${res.status}）`)
  }
  return body as T
}

export const api = {
  snapshot: (signal?: AbortSignal) =>
    request<ProviderSnapshot>('/api/providers/gemini', { signal }),

  createConversation: () => request<Conversation>('/api/conversations', { method: 'POST' }),

  listConversations: (page = 1, perPage = 200) =>
    request<Paginated<Conversation>>(`/api/conversations?page=${page}&perPage=${perPage}`),

  getConversation: (id: string) =>
    request<ConversationDetail>(`/api/conversations/${encodeURIComponent(id)}`),

  createTurn: (id: string, body: TurnRequest) =>
    request<Turn>(`/api/conversations/${encodeURIComponent(id)}/turns`, {
      method: 'POST',
      body: JSON.stringify(body),
      headers: { 'Idempotency-Key': crypto.randomUUID() },
    }),

  getTurn: (id: string, signal?: AbortSignal) =>
    request<Turn>(`/api/turns/${encodeURIComponent(id)}`, { signal }),

  retryTask: (id: string) =>
    request<Task>(`/api/tasks/${encodeURIComponent(id)}/retry`, { method: 'POST' }),

  acknowledgeUnknown: (id: string) =>
    request<void>(`/api/tasks/${encodeURIComponent(id)}/acknowledge-unknown`, { method: 'POST' }),

  login: () => request<LoginAck>('/api/providers/gemini/login', { method: 'POST' }),
}
