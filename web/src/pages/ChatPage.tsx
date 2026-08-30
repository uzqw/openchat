import { AssistantRuntimeProvider } from '@assistant-ui/react'
import { Link, useNavigate } from 'react-router-dom'
import { AssistantThread } from '../components/assistant-ui/thread'
import { Button, Card, ErrorBox, Spinner } from '../components/ui'
import { useOpenChatRuntime } from '../assistant/useOpenChatRuntime'
import { hasSuccess } from '../lib/turn'

export function ChatPage({ conversationId }: { conversationId?: string }) {
  const navigate = useNavigate()
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
    resumeConversation,
  } = useOpenChatRuntime(conversationId)

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
  const resumable = !!conv?.remote_id
  const convHasSuccess = conv ? hasSuccess(conv.turns) : false
  const conversationBusy = !!conv?.turns.some((turn) => turn.tasks.some((task) => task.status === 'pending' || task.status === 'running'))
  const pageBusy = busy || conversationBusy

  async function onNewConversation() {
    const created = await newConversation()
    if (created && conversationId) navigate('/')
  }

  async function onResume() {
    if (!conv) return
    await resumeConversation(conv.id)
  }

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
        <div className="mb-4 flex items-center justify-between gap-2">
          <h1 className="flex-1 truncate text-lg font-semibold">{conv?.title || '新会话'}</h1>
          <Button variant="secondary" disabled={pageBusy || quarantined} onClick={onNewConversation}>
            新建会话
          </Button>
        </div>

        {archived && (
          <div role="status" className="mb-4 rounded-md border border-slate-300 bg-slate-100 p-3 text-sm text-slate-700">
            {resumable ? (
              <>
                该会话已归档。点击「继续对话」恢复 Gemini 远端会话后即可继续提问。
                <Button className="ml-3" disabled={pageBusy || quarantined} onClick={onResume}>
                  继续对话
                </Button>
              </>
            ) : (
              <>只读历史：该会话未保存 Gemini 远端会话，不能继续提问。</>
            )}
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
          busy={pageBusy}
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
                  <Button variant="secondary" disabled={pageBusy} onClick={() => acknowledge(task)}>
                    确认 Chrome 已空闲
                  </Button>
                )}
                {(task.status === 'failed' || task.status === 'canceled' || task.status === 'auth_required') && (
                  <>
                    {!convHasSuccess && task.status === 'auth_required' && (
                      <Button disabled={pageBusy} onClick={() => startLogin()}>
                        去登录
                      </Button>
                    )}
                    <Button variant="secondary" disabled={pageBusy} onClick={() => retry(task)}>
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
