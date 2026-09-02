// mapping between backend ConversationDetail/Task and assistant-ui ThreadMessage
import type { ConversationDetail, Task } from '../types'
import type { ThreadMessage } from '@assistant-ui/react'
import { providerLabel } from '../lib/provider'

export type TaskMeta = {
  taskId: string
  turnId: string
  status: Task['status']
  provider: string
  requested_model: string
  resolved_model: string
  thinking: string
  latency_ms: number
  error_code?: string
  error_message?: string
  unknown_acknowledged_at?: string | null
}

function taskMeta(task: Task, provider: string): TaskMeta {
  return {
    taskId: task.id,
    turnId: task.turn,
    status: task.status,
    provider,
    requested_model: task.requested_model,
    resolved_model: task.resolved_model,
    thinking: task.thinking,
    latency_ms: task.latency_ms,
    error_code: task.error_code,
    error_message: task.error_message,
    unknown_acknowledged_at: task.unknown_acknowledged_at,
  }
}

function taskStatusToMessageStatus(s: Task['status']): ThreadMessage['status'] {
  if (s === 'pending' || s === 'running') return { type: 'running' } as ThreadMessage['status']
  if (s === 'succeeded') return { type: 'complete' } as ThreadMessage['status']
  // failed / auth_required / unknown_outcome / canceled -> incomplete (shows error handling in custom UI)
  return { type: 'incomplete', reason: s } as unknown as ThreadMessage['status']
}

function taskToContent(task: Task, provider: string): ThreadMessage['content'] {
  const label = providerLabel(provider)
  switch (task.status) {
    case 'pending':
      return [{ type: 'text', text: '排队中' }]
    case 'running':
      return [{ type: 'text', text: task.result ? task.result : '正在生成…' }]
    case 'succeeded':
      return [{ type: 'text', text: task.result || '' }]
    case 'failed':
      return [{ type: 'text', text: task.error_message || '请求执行失败' }]
    case 'auth_required':
      return [{ type: 'text', text: `需要登录 ${label} 才能继续 — 该会话已有成功回答，已归档为只读；登录后请新建会话。` }]
    case 'unknown_outcome':
      if (task.unknown_acknowledged_at) {
        return [{ type: 'text', text: '已确认，已恢复使用。可新建会话继续。' }]
      }
      return [{ type: 'text', text: `无法确认结果：请求可能已经提交到 ${label}。为安全起见，会话已归档、${label} 已暂停，且不能直接重试；请确认浏览器已停止生成。` }]
    case 'canceled':
      return [{ type: 'text', text: '已取消，可以重试。' }]
    default:
      return [{ type: 'text', text: task.result || task.error_message || '' }]
  }
}

export function convertConversation(detail: ConversationDetail | null): ThreadMessage[] {
  if (!detail) return []
  const out: ThreadMessage[] = []
  for (const turn of detail.turns) {
    // user message
    out.push({
      id: turn.id,
      role: 'user',
      content: [{ type: 'text', text: turn.prompt }],
      createdAt: new Date(turn.created),
      status: { type: 'complete' } as ThreadMessage['status'],
      metadata: { custom: { turnId: turn.id } } as unknown as ThreadMessage['metadata'],
      attachments: [],
    } as unknown as ThreadMessage)
    // assistant messages: one per task (history) – keeps retry history visible
    // if no tasks yet (edge), push placeholder running
    if (turn.tasks.length === 0) {
      out.push({
        id: `${turn.id}__assistant`,
        role: 'assistant',
        content: [{ type: 'text', text: '' }],
        createdAt: new Date(turn.created),
        status: { type: 'running' } as ThreadMessage['status'],
        metadata: { custom: { turnId: turn.id, placeholder: true, provider: detail.provider } } as unknown as ThreadMessage['metadata'],
        attachments: [],
      } as unknown as ThreadMessage)
    } else {
      for (const task of turn.tasks) {
        out.push({
          id: task.id,
          role: 'assistant',
          content: taskToContent(task, detail.provider),
          createdAt: new Date(task.created),
          status: taskStatusToMessageStatus(task.status),
          metadata: { custom: taskMeta(task, detail.provider) } as unknown as ThreadMessage['metadata'],
          attachments: [],
        } as unknown as ThreadMessage)
      }
    }
  }
  return out
}
