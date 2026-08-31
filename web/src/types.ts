// JSON shapes of the v1 REST API (docs/domain-api.md §4, internal/api/handlers.go).

export type ConversationStatus = 'active' | 'archived'
export type TaskStatus =
  | 'pending'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'auth_required'
  | 'unknown_outcome'
  | 'canceled'
export type LoginOperation = 'idle' | 'queued' | 'running' | 'succeeded' | 'failed'

export interface Conversation {
  id: string
  title: string
  status: ConversationStatus
  provider: string // site adapter: "gemini" | "grok"
  remote_id?: string
  created: string
}

export interface Task {
  id: string
  turn: string
  retry_of?: string
  requested_model: string
  resolved_model: string
  thinking: string
  status: TaskStatus
  result?: string
  error_code?: string
  error_message?: string
  unknown_acknowledged_at?: string | null
  latency_ms: number
  created: string
}

export interface Turn {
  id: string
  conversation: string
  prompt: string
  idempotency_key: string
  created: string
  tasks: Task[]
  current_task?: Task
}

export interface ConversationDetail extends Conversation {
  turns: Turn[]
}

export interface Paginated<T> {
  items: T[]
  page: number
  perPage: number
  totalItems: number
  totalPages: number
}

export interface ProviderSnapshot {
  site: string // opencli adapter name ("gemini" / "grok")
  model_pick: boolean // ask accepts --model (model selector shown)
  thinking_supported: boolean // ask accepts --thinking (thinking selector shown)
  version: string
  bridge: string
  models: string[]
  logged_in: boolean
  login_operation: LoginOperation
  login_message?: string
  quarantined: boolean
  refreshed_at?: string
  write_blocked?: string
}

// GET /api/providers: all site states plus the configured default site.
export interface ProvidersResponse {
  default_site: string
  providers: ProviderSnapshot[]
}

export interface TurnRequest {
  prompt: string
  model?: string
  thinking?: string
}

export interface LoginAck {
  login_operation: LoginOperation
}

export interface RefreshAck {
  refresh_operation: 'queued'
}
