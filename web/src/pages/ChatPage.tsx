import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { AssistantRuntimeProvider } from '@assistant-ui/react'
import { Link, useNavigate } from 'react-router-dom'
import { AssistantThread } from '../components/assistant-ui/thread'
import { Button, Card, ErrorBox, Skeleton } from '../components/ui'
import { useOpenChatRuntime } from '../assistant/useOpenChatRuntime'
import { providerLabel } from '../lib/provider'
import { hasSuccess } from '../lib/turn'

function SitePicker({
  providers,
  siteChoice,
  disabled,
  onPick,
}: {
  providers: { site: string }[]
  siteChoice: string
  disabled: boolean
  onPick: (site: string) => void
}) {
  if (providers.length <= 1) return null
  return (
    <div role="group" aria-label="站点" className="flex shrink-0 overflow-hidden rounded-md border border-line">
      {providers.map((p) => (
        <button
          key={p.site}
          type="button"
          aria-pressed={siteChoice === p.site}
          disabled={disabled}
          onClick={() => onPick(p.site)}
          className={
            siteChoice === p.site
              ? 'bg-accent-fill min-h-9 px-2.5 py-2 text-xs font-medium text-white'
              : 'bg-subtle min-h-9 px-2.5 py-2 text-xs text-ink-soft hover:bg-hover'
          }
        >
          {providerLabel(p.site)}
        </button>
      ))}
    </div>
  )
}

export function ChatPage({ conversationId }: { conversationId?: string }) {
  const navigate = useNavigate()
  const {
    runtime,
    snapshot,
    providers,
    defaultSite,
    setNextSite,
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
    reload,
  } = useOpenChatRuntime(conversationId)

  // site for the next new conversation; remembered in localStorage, falls back to backend default.
  // providers load async, so the saved choice can only be validated once they arrive
  const SITE_KEY = 'openchat.site'
  const [siteChoice, setSiteChoice] = useState(defaultSite)
  useEffect(() => {
    const saved = localStorage.getItem(SITE_KEY)
    setSiteChoice(saved && providers.some((p) => p.site === saved) ? saved : defaultSite)
  }, [providers, defaultSite])

  // mobile header slot (rendered by App's top nav bar)
  const [titleSlot, setTitleSlot] = useState<HTMLElement | null>(null)
  useEffect(() => {
    setTitleSlot(document.getElementById('mobile-title-slot'))
  }, [])
  useEffect(() => {
    setNextSite(siteChoice)
  }, [siteChoice, setNextSite])

  function pickSite(site: string) {
    localStorage.setItem(SITE_KEY, site)
    setSiteChoice(site)
  }

  if (!snapshot) {
    return (
      <div
        role="status"
        aria-label="加载中"
        className="flex h-full min-h-0 flex-col gap-2 px-2 py-2 sm:gap-3 sm:px-5 sm:py-4 lg:px-8"
      >
        {/* skeleton of the loaded chat: title row, a message pair, composer */}
        <div className="hidden shrink-0 items-center gap-2 lg:flex">
          <Skeleton className="h-6 w-48" />
          <Skeleton className="h-9 w-24" />
          <Skeleton className="h-9 w-20" />
        </div>
        <div className="flex flex-1 flex-col justify-end gap-4">
          <div className="flex justify-end">
            <Skeleton className="h-10 w-2/3 max-w-sm rounded-2xl" />
          </div>
          <div className="flex justify-start">
            <Skeleton className="h-28 w-3/4 max-w-md rounded-2xl" />
          </div>
        </div>
        <Skeleton className="mx-auto h-24 w-full max-w-3xl shrink-0 rounded-2xl" />
        {error && (
          <div className="shrink-0">
            <ErrorBox>{error}</ErrorBox>
            <div className="mt-2">
              <Button variant="secondary" onClick={reload}>
                重试
              </Button>
            </div>
          </div>
        )}
      </div>
    )
  }

  const quarantined = snapshot.quarantined
  const archived = conv?.status === 'archived'
  const resumable = !!conv?.remote_id
  const label = providerLabel(snapshot.site)
  const convHasSuccess = conv ? hasSuccess(conv.turns) : false
  const conversationBusy = !!conv?.turns.some((turn) => turn.tasks.some((task) => task.status === 'pending' || task.status === 'running'))
  const pageBusy = busy || conversationBusy

  async function onNewConversation(site: string) {
    const created = await newConversation(site)
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

  // status badge tokens mirroring TurnList's statusBadge
  const taskBadge: Record<string, string> = {
    failed: 'bg-danger-soft text-danger-ink',
    canceled: 'bg-subtle text-ink-faint',
    auth_required: 'bg-warn-soft text-warn-ink',
    unknown_outcome: 'bg-alert-soft text-alert-ink',
  }

  return (
    <AssistantRuntimeProvider runtime={runtime}>
      <div className="flex h-full min-h-0 flex-col gap-2 px-2 py-2 sm:gap-3 sm:px-5 sm:py-4 lg:px-8">
        {titleSlot &&
          createPortal(
            <>
              <h1 className="min-w-0 flex-1 truncate text-sm font-semibold">{conv?.title || '新会话'}</h1>
              <SitePicker providers={providers} siteChoice={siteChoice} disabled={pageBusy || quarantined} onPick={pickSite} />
              <Button
                className="shrink-0"
                variant="secondary"
                disabled={pageBusy || quarantined}
                onClick={() => void onNewConversation(siteChoice)}
              >
                新会话
              </Button>
            </>,
            titleSlot,
          )}
        <div className="hidden shrink-0 items-center gap-2 lg:flex">
          <h1 className="min-w-0 flex-1 truncate text-base font-semibold sm:text-lg">{conv?.title || '新会话'}</h1>
          <SitePicker providers={providers} siteChoice={siteChoice} disabled={pageBusy || quarantined} onPick={pickSite} />
          <Button className="shrink-0" variant="secondary" disabled={pageBusy || quarantined} onClick={() => void onNewConversation(siteChoice)}>
            新建会话
          </Button>
        </div>

        {archived && (
          <div role="status" className="shrink-0 rounded-lg border border-line-strong bg-hover p-3 text-sm text-ink-soft">
            {resumable ? (
              <>
                该会话已归档。点击「继续对话」恢复 {label} 远端会话后即可继续提问。
                <Button className="ml-3" disabled={pageBusy || quarantined} onClick={onResume}>
                  继续对话
                </Button>
              </>
            ) : (
              <>只读历史：该会话未保存 {label} 远端会话，不能继续提问。</>
            )}
          </div>
        )}

        {error && (
          <div className="shrink-0">
            <ErrorBox>{error}</ErrorBox>
          </div>
        )}

        {/* login hint banner when a login is in progress */}
        {loginHint && !archived && (
          <div role="status" className="shrink-0 rounded-lg border border-info-line bg-info-soft p-3 text-sm text-info-ink">
            {loginHint}
          </div>
        )}

        <AssistantThread
          models={snapshot.models}
          model={model}
          setModel={setModel}
          thinking={thinking}
          setThinking={setThinking}
          providerLabel={label}
          modelPick={snapshot.model_pick}
          thinkingSupported={snapshot.thinking_supported}
          busy={pageBusy}
          quarantined={quarantined}
          archived={archived}
          resetKey={conv?.id ?? 'new'}
          conversationMessages={(() => {
            if (!conv) return []
            const msgs: { role: string; text: string }[] = []
            for (const turn of conv.turns) {
              msgs.push({ role: 'user', text: turn.prompt })
              for (const task of turn.tasks) {
                if (task.status === 'succeeded' && task.result) msgs.push({ role: 'assistant', text: task.result })
              }
            }
            return msgs
          })()}
        />

        {/* task-specific actions: rendered below thread to keep thread pure */}
        {actionableTasks.length > 0 && (
          <Card className="mx-auto w-full max-w-3xl shrink-0 space-y-3">
            <h3 className="text-sm font-semibold text-ink-soft">待处理任务</h3>
            {actionableTasks.map(({ task }) => (
              <div key={task.id} className="flex flex-wrap items-center gap-2 rounded-md border border-line bg-subtle p-3 text-sm">
                <span className={`rounded-full px-2 py-0.5 text-xs ${taskBadge[task.status]}`}>
                  {task.status === 'failed' && '失败'}
                  {task.status === 'canceled' && '已取消'}
                  {task.status === 'auth_required' && '需要登录'}
                  {task.status === 'unknown_outcome' && '结果未确认'}
                </span>
                {task.status === 'unknown_outcome' && !task.unknown_acknowledged_at && (
                  <Button variant="secondary" disabled={pageBusy} onClick={() => acknowledge(task)}>
                    确认浏览器已停止生成
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
          <p className="mx-auto w-full max-w-3xl shrink-0 pb-2 text-sm text-warn-ink">
            {label} 已暂停：请先确认相关任务的浏览器已停止生成（见上方或
            <Link className="text-accent underline" to="/history">
              历史记录
            </Link>
            ）。
          </p>
        )}
      </div>
    </AssistantRuntimeProvider>
  )
}
