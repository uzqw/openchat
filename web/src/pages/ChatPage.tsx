import { AssistantRuntimeProvider } from '@assistant-ui/react'
import { Link } from 'react-router-dom'
import { AssistantThread } from '../components/assistant-ui/thread'
import { Button, Card, ErrorBox, Spinner } from '../components/ui'
import { useOpenChatRuntime } from '../assistant/useOpenChatRuntime'
import { hasSuccess } from '../lib/turn'

export function ChatPage() {
  const {
    runtime,
    snapshot,
    conv,
    model,
    setModel,
    thinking,
    setThinking,
    busy,
    error,
    loginHint,
    retry,
    acknowledge,
    startLogin,
    newConversation,
  } = useOpenChatRuntime()

  if (!snapshot) {
    return (
      <div className="mx-auto max-w-3xl px-4 py-16 text-center">
        <Spinner label="加载中…" />
        {error && (
          <div className="mt-4">
            <ErrorBox>{error}</ErrorBox>
          </div>
        )}
      </div>
    )
  }

  const quarantined = snapshot.quarantined
  const archived = conv?.status === 'archived'
  const convHasSuccess = conv ? hasSuccess(conv.turns) : false

  // collect tasks that need action buttons (outside thread messages)
  const actionableTasks = (() => {
    if (!conv) return []
    const out: Array<{ task: import('../types').Task; turnPrompt: string }> = []
    for (const turn of conv.turns) {
      for (const task of turn.tasks) {
        if (['failed', 'auth_required', 'canceled'].includes(task.status) || (task.status === 'unknown_outcome' && !task.unknown_acknowledged_at)) {
          // only show auth retry when no success (backend rule)
          if (task.status === 'auth_required' && convHasSuccess) continue
          out.push({ task, turnPrompt: turn.prompt.slice(0, 30) })
        }
      }
    }
    return out
  })()

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <div className="mx-auto w-full max-w-3xl px-4">
        {archived && (
          <div role="status" className="mb-4 rounded-md border border-slate-300 bg-slate-100 p-3 text-sm text-slate-700">
            只读历史：该会话已归档，不能继续提问。
            <Button className="ml-3" variant="secondary" onClick={newConversation}>
              新建会话
            </Button>
          </div>
        )}

        {error && (
          <div className="mb-4">
            <ErrorBox>{error}</ErrorBox>
          </div>
        )}

        {/* login hint banner when a login is in progress */}
        {loginHint && !archived && (
          <div role="status" className="mb-4 rounded-md border border-sky-200 bg-sky-50 p-3 text-sm text-sky-800">
            {loginHint}
          </div>
        )}

        <AssistantThread
          models={snapshot.models}
          model={model}
          setModel={setModel}
          thinking={thinking}
          setThinking={setThinking}
          busy={busy}
          quarantined={quarantined}
          archived={archived}
          conversationMessages={(() => {
            if (!conv) return []
            const msgs: { role: string; text: string }[] = []
            for (const turn of conv.turns) {
              msgs.push({ role: 'user', text: turn.prompt })
              for (const task of turn.tasks) {
                if (task.status === 'succeeded' && task.result) msgs.push({ role: 'assistant', text: task.result })
                else if (task.error_message) msgs.push({ role: 'assistant', text: task.error_message })
              }
            }
            return msgs
          })()}
        />

        {/* task-specific actions: rendered below thread to keep thread pure */}
        {actionableTasks.length > 0 && (
          <Card className="mt-4 space-y-3">
            <h3 className="text-sm font-semibold text-slate-700">待处理任务</h3>
            {actionableTasks.map(({ task }) => (
              <div key={task.id} className="flex flex-wrap items-center gap-2 rounded-md border border-slate-200 bg-slate-50 p-3 text-sm">
                <span className="rounded-full bg-slate-200 px-2 py-0.5 text-xs">
                  {task.status === 'failed' && '失败'}
                  {task.status === 'canceled' && '已取消'}
                  {task.status === 'auth_required' && '需要登录'}
                  {task.status === 'unknown_outcome' && '结果未知'}
                </span>
                <span className="flex-1 truncate text-slate-600">
                  {task.error_message || (task.status === 'auth_required' ? '需要登录 Gemini' : task.status === 'unknown_outcome' ? '结果未知，请确认 Chrome 已空闲' : '可重试')}
                </span>
                {task.status === 'unknown_outcome' && !task.unknown_acknowledged_at && (
                  <Button variant="secondary" disabled={busy} onClick={() => acknowledge(task)}>
                    确认 Chrome 已空闲
                  </Button>
                )}
                {(task.status === 'failed' || task.status === 'canceled' || task.status === 'auth_required') && (
                  <>
                    {!convHasSuccess && task.status === 'auth_required' && (
                      <Button disabled={busy} onClick={() => startLogin()}>
                        去登录
                      </Button>
                    )}
                    <Button variant="secondary" disabled={busy} onClick={() => retry(task)}>
                      重试
                    </Button>
                  </>
                )}
              </div>
            ))}
          </Card>
        )}

        {quarantined && (
          <p className="mt-4 text-sm text-amber-700">
            Gemini 已隔离：请先确认结果未知的任务的 Chrome 已空闲（见上方或
            <Link className="text-sky-600 underline" to="/history">
              历史记录
            </Link>
            ）。
          </p>
        )}
      </div>
    </AssistantRuntimeProvider>
  )
}
