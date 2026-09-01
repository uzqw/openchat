import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import { AssistantRuntimeProvider } from '@assistant-ui/react'
import { Link, useNavigate } from 'react-router-dom'
import { AssistantThread } from '../components/assistant-ui/thread'
import { Button, Card, ErrorBox, Spinner } from '../components/ui'
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
    <div role="group" aria-label="站点" className="flex shrink-0 overflow-hidden rounded-md border border-slate-200">
      {providers.map((p) => (
        <button
          key={p.site}
          type="button"
          aria-pressed={siteChoice === p.site}
          disabled={disabled}
          onClick={() => onPick(p.site)}
          className={
            siteChoice === p.site
              ? 'bg-sky-600 px-3 py-1.5 text-xs font-medium text-white'
              : 'bg-slate-50 px-3 py-1.5 text-xs text-slate-600 hover:bg-slate-100'
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
      <div className="flex min-h-full flex-col items-center justify-center px-4 py-16 text-center">
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
          <div role="status" className="shrink-0 rounded-lg border border-slate-300 bg-slate-100 p-3 text-sm text-slate-700">
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
          <div role="status" className="shrink-0 rounded-lg border border-sky-200 bg-sky-50 p-3 text-sm text-sky-800">
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
            <h3 className="text-sm font-semibold text-slate-700">待处理任务</h3>
            {actionableTasks.map(({ task }) => (
              <div key={task.id} className="flex flex-wrap items-center gap-2 rounded-md border border-slate-200 bg-slate-50 p-3 text-sm">
                <span className="rounded-full bg-slate-200 px-2 py-0.5 text-xs">
                  {task.status === 'failed' && '失败'}
                  {task.status === 'canceled' && '已取消'}
                  {task.status === 'auth_required' && '需要登录'}
                  {task.status === 'unknown_outcome' && '结果未知'}
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
          <p className="mx-auto w-full max-w-3xl shrink-0 pb-2 text-sm text-amber-700">
            {label} 已隔离：请先确认结果未知的任务的 Chrome 已空闲（见上方或
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
