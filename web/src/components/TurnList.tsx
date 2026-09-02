// TurnList renders one conversation read-only: user prompts plus every
// task with its terminal state and the state-specific actions (retry,
// login guidance, "confirm the browser stopped generating"). It is shared
// by the current session page and the history detail page; the caller
// decides which actions are available.

import { useState } from 'react'
import type { ConversationDetail, Task } from '../types'
import { Markdown, normalizeMarkdown } from '../lib/markdown'
import { providerLabel } from '../lib/provider'
import { hasSuccess } from '../lib/turn'
import { Button, Spinner } from './ui'

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)
  if (!text) return null
  return (
    <button
      type="button"
      aria-label="复制"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
          setCopied(true)
          setTimeout(() => setCopied(false), 1200)
        } catch { void 0 }
      }}
      className="rounded-md border border-line bg-surface px-2 py-1 text-xs text-ink-soft hover:bg-subtle"
    >
      {copied ? '已复制' : '复制'}
    </button>
  )
}

interface TurnListProps {
  conv: ConversationDetail
  quarantined: boolean
  busy?: boolean
  /** transient guidance while a login is queued/running */
  loginHint?: string
  onRetry?: (task: Task) => void
  onAcknowledge?: (task: Task) => void
  onLogin?: () => void
}

const statusLabel: Record<Task['status'], string> = {
  pending: '排队中',
  running: '生成中',
  succeeded: '已完成',
  failed: '失败',
  auth_required: '需要登录',
  unknown_outcome: '结果未确认',
  canceled: '已取消',
}

const statusBadge: Record<Task['status'], string> = {
  pending: 'bg-subtle text-ink-soft',
  running: 'bg-info-soft text-info-ink',
  succeeded: 'bg-ok-soft text-ok-ink',
  failed: 'bg-danger-soft text-danger-ink',
  auth_required: 'bg-warn-soft text-warn-ink',
  unknown_outcome: 'bg-alert-soft text-alert-ink',
  canceled: 'bg-subtle text-ink-faint',
}

function TaskCard({
  task,
  convHasSuccess,
  busy,
  loginHint,
  onRetry,
  onAcknowledge,
  onLogin,
  label,
}: {
  task: Task
  convHasSuccess: boolean
  busy?: boolean
  loginHint?: string
  onRetry?: (task: Task) => void
  onAcknowledge?: (task: Task) => void
  onLogin?: () => void
  label: string
}) {
  const meta = [
    task.requested_model || task.resolved_model
      ? `模型：${task.resolved_model || task.requested_model}`
      : '',
    task.thinking ? `思考模式：${task.thinking}` : '',
    task.latency_ms > 0 ? `${task.latency_ms}ms` : '',
  ]
    .filter(Boolean)
    .join(' · ')

  return (
    <div className={task.status === 'succeeded' ? 'space-y-2' : 'rounded-lg border border-line bg-subtle p-3'}>
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${statusBadge[task.status]}`}>
          {statusLabel[task.status]}
        </span>
        {meta && <span className="text-xs text-ink-faint">{meta}</span>}
      </div>

      {task.status === 'pending' && <Spinner label="等待执行…" />}
      {task.status === 'running' && <Spinner label="正在生成…" />}

      {task.status === 'succeeded' && task.result && (
        <div className="text-[15px] leading-7 text-ink">
          <div className="mb-1 text-xs font-medium text-ink-faint">{label}</div>
          <div className="group relative">
            <Markdown content={task.result} />
            <div className="mt-2 flex justify-end opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
              <CopyButton text={normalizeMarkdown(task.result)} />
            </div>
          </div>
        </div>
      )}

      {task.status === 'failed' && (
        <p className="text-sm text-danger-ink">{task.error_message || '请求执行失败'}</p>
      )}

      {task.status === 'canceled' && <p className="text-sm text-ink-soft">已取消，可以重试。</p>}

      {task.status === 'auth_required' && (
        <div className="text-sm text-ink-soft">
          <p>
            {convHasSuccess
              ? '该会话已有成功回答，已归档为只读；登录后请新建会话。'
              : `需要登录 ${label} 才能继续此会话。`}
          </p>
          {!convHasSuccess && (
            <div className="mt-2 flex flex-wrap gap-2">
              {onRetry && (
                <Button variant="secondary" disabled={busy} onClick={() => onRetry(task)}>
                  重试
                </Button>
              )}
              {onLogin && (
                <Button disabled={busy} onClick={onLogin}>
                  去登录
                </Button>
              )}
            </div>
          )}
          {loginHint && <p className="mt-2 text-accent-strong">{loginHint}</p>}
        </div>
      )}

      {task.status === 'unknown_outcome' && (
        <div className="text-sm text-ink-soft">
          <p>
            无法确认结果：请求可能已经提交到 {label}。为安全起见，会话已归档、{label} 已暂停，且不能直接重试；请确认浏览器已停止生成。
          </p>
          {!task.unknown_acknowledged_at && onAcknowledge && (
            <Button variant="secondary" className="mt-2" disabled={busy} onClick={() => onAcknowledge(task)}>
              确认浏览器已停止生成
            </Button>
          )}
          {task.unknown_acknowledged_at && (
            <p className="mt-2 text-ok-ink">已确认，已恢复使用。可新建会话继续。</p>
          )}
        </div>
      )}
    </div>
  )
}

export function TurnList({ conv, quarantined, busy, loginHint, onRetry, onAcknowledge, onLogin }: TurnListProps) {
  const convHasSuccess = hasSuccess(conv.turns)
  const label = providerLabel(conv.provider)
  return (
    <section aria-label="对话内容" className="mx-auto w-full max-w-3xl space-y-6">
      {quarantined && (
        <div role="status" className="rounded-md border border-warn-line bg-warn-soft p-3 text-sm text-warn-ink">
          {label} 已暂停：存在尚未确认的结果。请确认浏览器已停止生成后再继续。
        </div>
      )}
      {conv.turns.length === 0 && (
        <div className="py-10 text-center text-sm text-ink-faint">还没有消息，输入问题开始对话。</div>
      )}
      {conv.turns.map((turn) => (
        <div key={turn.id} className="space-y-2">
          <div className="flex justify-end">
            <div className="group flex max-w-[85%] items-start gap-2 rounded-2xl rounded-br-none border border-line bg-hover px-4 py-2.5 text-sm leading-6 text-ink">
              <span className="flex-1 whitespace-pre-wrap">{turn.prompt}</span>
              <CopyButton text={turn.prompt} />
            </div>
          </div>
          {turn.tasks.map((task) => (
            <TaskCard
              key={task.id}
              task={task}
              convHasSuccess={convHasSuccess}
              busy={busy}
              loginHint={loginHint}
              onRetry={onRetry}
              onAcknowledge={onAcknowledge}
              onLogin={onLogin}
              label={label}
            />
          ))}
        </div>
      ))}
    </section>
  )
}
