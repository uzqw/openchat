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

export interface TurnRequest {
  prompt: string
  model?: string
  thinking?: string
}

export interface LoginAck {
  login_operation: LoginOperation
}
