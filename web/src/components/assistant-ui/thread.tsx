import { useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import {
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
  useAuiState,
} from '@assistant-ui/react'
import { useMessagePartText } from '@assistant-ui/react'
import { Link } from 'react-router-dom'
import { Markdown, normalizeMarkdown } from '../../lib/markdown'
import { providerLabel } from '../../lib/provider'
import { ProgressBar, Skeleton } from '../ui'
import type { TaskMeta } from '../../assistant/convert'

function SlidersIcon({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      className={className}
      aria-hidden="true"
    >
      <line x1="4" y1="21" x2="4" y2="14" />
      <line x1="4" y1="10" x2="4" y2="3" />
      <line x1="12" y1="21" x2="12" y2="12" />
      <line x1="12" y1="8" x2="12" y2="3" />
      <line x1="20" y1="21" x2="20" y2="16" />
      <line x1="20" y1="12" x2="20" y2="3" />
      <line x1="1" y1="14" x2="7" y2="14" />
      <line x1="9" y1="8" x2="15" y2="8" />
      <line x1="17" y1="16" x2="23" y2="16" />
    </svg>
  )
}

function CopyButton({ text, label = '复制' }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false)
  if (!text) return null
  return (
    <button
      type="button"
      aria-label={label}
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text)
          setCopied(true)
          setTimeout(() => setCopied(false), 1200)
        } catch {
          // ignore
        }
      }}
      className="rounded-md border border-line bg-surface px-2 py-1 text-xs text-ink-soft hover:bg-subtle"
    >
      {copied ? '已复制' : label}
    </button>
  )
}

function UserText() {
  const p = useMessagePartText() as unknown as { text: string } | null
  const text = p?.text ?? ''
  return (
    <span className="group flex items-start gap-2 whitespace-pre-wrap">
      <span className="flex-1">{text}</span>
      <span className="shrink-0 opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
        <CopyButton text={text} />
      </span>
    </span>
  )
}

function MarkdownText() {
  const part = useMessagePartText() as unknown as { text: string } | null
  const text = part?.text ?? ''
  if (!text) return null
  return (
    <div className="group relative">
      <Markdown content={text} />
      <div className="mt-2 flex justify-end opacity-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100">
        <CopyButton text={normalizeMarkdown(text)} label="复制 Markdown" />
      </div>
    </div>
  )
}

function UserMessage() {
  return (
    <MessagePrimitive.Root className="mx-auto flex w-full max-w-3xl justify-end py-3">
      <div className="max-w-[85%] rounded-2xl rounded-br-none border border-line bg-hover px-4 py-2.5 text-sm leading-6 text-ink">
        <MessagePrimitive.Parts
          components={{
            Text: UserText,
          }}
        />
      </div>
    </MessagePrimitive.Root>
  )
}

function AssistantMessage() {
  // task metadata (provider, model, latency) rides on the message custom
  // metadata; the placeholder message only carries the provider.
  const meta = useAuiState((s) => s.optional.message?.metadata?.custom ?? null) as unknown as TaskMeta | null
  const label = providerLabel(meta?.provider)
  const metaLine = [
    meta?.requested_model || meta?.resolved_model
      ? `模型：${meta.resolved_model || meta.requested_model}`
      : '',
    meta?.thinking ? `思考模式：${meta.thinking}` : '',
    meta && meta.latency_ms > 0 ? `${meta.latency_ms}ms` : '',
  ]
    .filter(Boolean)
    .join(' · ')
  // while a task is queued/running, show live progress instead of the
  // placeholder text: a status badge, skeleton lines and an indeterminate bar
  const inProgress = meta?.status === 'pending' || meta?.status === 'running'
  return (
    <MessagePrimitive.Root className="mx-auto flex w-full max-w-3xl flex-col gap-2 py-4">
      <div className="w-full max-w-3xl">
        <div className="mb-1 flex flex-wrap items-center gap-x-2 gap-y-0.5">
          <span className="text-xs font-medium text-ink-faint">{label}</span>
          {metaLine && <span className="text-xs text-ink-faint">{metaLine}</span>}
        </div>
        {inProgress ? (
          <div role="status" className="space-y-2">
            <span
              className={`inline-block rounded-full px-2 py-0.5 text-xs font-medium ${
                meta?.status === 'pending' ? 'bg-subtle text-ink-soft' : 'bg-info-soft text-info-ink'
              }`}
            >
              {meta?.status === 'pending' ? '排队中' : '正在生成'}
            </span>
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-5/6" />
            <Skeleton className="h-4 w-2/3" />
            <ProgressBar label={meta?.status === 'pending' ? '排队中' : '正在生成'} />
          </div>
        ) : (
          <div className="text-[15px] leading-7 text-ink">
            <MessagePrimitive.Parts
              components={{
                Text: MarkdownText,
              }}
            />
          </div>
        )}
      </div>
    </MessagePrimitive.Root>
  )
}

function CopyConversationButton({ messages }: { messages: { role: string; text: string }[] }) {
  const [copied, setCopied] = useState(false)
  if (messages.length === 0) return null
  return (
    <button
      type="button"
      aria-label="复制会话"
      onClick={async () => {
        const md = messages
          .map((m) => `${m.role === 'user' ? 'User' : 'Assistant'}: ${m.role === 'assistant' ? normalizeMarkdown(m.text) : m.text}`)
          .join('\n\n---\n\n')
        try {
          await navigator.clipboard.writeText(md)
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        } catch {
          // ignore
        }
      }}
      className="rounded-md border border-line bg-surface px-3 py-1.5 text-xs text-ink-soft hover:bg-subtle"
    >
      {copied ? '已复制会话' : '复制会话'}
    </button>
  )
}

export function AssistantThread({
  models,
  model,
  setModel,
  thinking,
  setThinking,
  providerLabel = 'Gemini',
  modelPick = true,
  thinkingSupported = true,
  busy,
  quarantined,
  archived,
  conversationMessages,
  resetKey,
}: {
  models: string[]
  model: string
  setModel: (v: string) => void
  thinking: string
  setThinking: (v: string) => void
  // provider capability flags from the snapshot: the selectors are
  // shown only when the adapter supports the knobs
  providerLabel?: string
  modelPick?: boolean
  thinkingSupported?: boolean
  busy?: boolean
  quarantined?: boolean
  archived?: boolean
  conversationMessages?: { role: string; text: string }[]
  /** changes when the conversation changes → reset auto-hide state */
  resetKey?: string
}) {
  // hide the composer while scrolling down (mobile reading), show again on scroll up
  const [composerHidden, setComposerHidden] = useState(false)
  const lastScrollY = useRef(0)
  // compact options control: an icon button that opens a popover with the
  // model/thinking selectors (TOS compliance block, kept disabled-while-busy)
  const [optionsOpen, setOptionsOpen] = useState(false)
  const [popoverPos, setPopoverPos] = useState<{ bottom: number; left: number } | null>(null)
  const optionsBtnRef = useRef<HTMLButtonElement>(null)
  const popoverRef = useRef<HTMLDivElement>(null)
  // new conversation → show the composer again and resync the scroll baseline
  useEffect(() => {
    setComposerHidden(false)
    setOptionsOpen(false)
    lastScrollY.current = 0
  }, [resetKey])
  // the composer collapses on scroll-down; take the popover with it
  useEffect(() => {
    if (composerHidden) setOptionsOpen(false)
  }, [composerHidden])
  // close the popover on outside click or Escape
  useEffect(() => {
    if (!optionsOpen) return
    function onDown(e: MouseEvent) {
      const t = e.target as Node
      if (optionsBtnRef.current?.contains(t) || popoverRef.current?.contains(t)) return
      setOptionsOpen(false)
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOptionsOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [optionsOpen])
  function toggleOptions() {
    if (!optionsOpen && optionsBtnRef.current) {
      const r = optionsBtnRef.current.getBoundingClientRect()
      // anchor the popover's left edge to the button, clamped to the viewport
      // (on mobile the action row wraps, so the button can sit at the left edge)
      const W = 288 // w-72
      const left = Math.min(Math.max(r.left, 8), window.innerWidth - W - 8)
      setPopoverPos({ bottom: window.innerHeight - r.top + 8, left })
    }
    setOptionsOpen((o) => !o)
  }
  function onViewportScroll(e: React.UIEvent<HTMLDivElement>) {
    const el = e.currentTarget
    const y = el.scrollTop
    const delta = y - lastScrollY.current
    lastScrollY.current = y
    if (Math.abs(delta) < 8) return
    const nearBottom = el.scrollHeight - y - el.clientHeight < 80
    const nearTop = y < 80
    setComposerHidden(delta > 0 && !nearBottom && !nearTop)
  }
  return (
    <ThreadPrimitive.Root className="flex min-h-[28rem] min-w-0 flex-1 flex-col bg-transparent">
      {conversationMessages && conversationMessages.length > 0 && (
        <div className="shrink-0 border-b border-line/80 px-3 py-2 sm:px-6">
          <div className="mx-auto flex w-full max-w-3xl justify-end">
            <CopyConversationButton messages={conversationMessages} />
          </div>
        </div>
      )}
      <ThreadPrimitive.Viewport onScroll={onViewportScroll} className="min-h-0 flex-1 overflow-y-auto px-3 py-3 sm:px-6 sm:py-5">
        <ThreadPrimitive.Empty>
          <div className="mx-auto flex h-full min-h-[20rem] w-full max-w-3xl items-center justify-center py-16 text-sm text-ink-faint">
            {quarantined ? (
              <div className="space-y-2 text-center">
                <p>{providerLabel} 已暂停，暂时无法创建新会话。</p>
                <p>
                  <Link className="text-accent underline" to="/history">
                    前往历史记录确认浏览器已停止生成
                  </Link>
                </p>
              </div>
            ) : (
              <p>还没有会话。输入问题开始对话。</p>
            )}
          </div>
        </ThreadPrimitive.Empty>

        <ThreadPrimitive.Messages
          components={{
            UserMessage,
            AssistantMessage,
          }}
        />

        {/* archived/quarantined banners are handled by ChatPage to avoid duplicate text for tests */}
      </ThreadPrimitive.Viewport>

      <div
        className={
          'shrink-0 overflow-hidden border-t border-line/80 bg-subtle/90 px-3 transition-all duration-200 sm:px-6 ' +
          (composerHidden ? 'max-h-0 border-transparent p-0 opacity-0' : 'max-h-96 py-3')
        }
      >
        <div className="mx-auto w-full max-w-3xl">
          <div className="rounded-2xl border border-line bg-surface p-2.5 shadow-sm">
            <label htmlFor="prompt-input" className="sr-only">
              消息
            </label>
            <ComposerPrimitive.Root className="flex flex-wrap items-end gap-2 pt-2">
              <ComposerPrimitive.Input
                placeholder={quarantined ? `${providerLabel} 已暂停，无法发送` : '输入问题，Enter 发送，Shift+Enter 换行'}
                disabled={!!busy || !!quarantined || !!archived}
                autoFocus
                id="prompt-input"
                className="max-h-32 min-h-[44px] min-w-[12rem] flex-1 resize-none rounded-xl border border-line bg-subtle px-3 py-2.5 text-[16px] sm:text-sm placeholder:text-ink-faint focus-visible:outline-2 focus-visible:outline-accent disabled:bg-subtle"
              />
              <div className="flex shrink-0 items-center gap-2">
                {/* compact options control: opens the model/thinking popover
                    (TOS compliance block — selectors stay reachable and
                    disabled-while-busy) */}
                <button
                  ref={optionsBtnRef}
                  type="button"
                  aria-label="选项"
                  aria-expanded={optionsOpen}
                  aria-haspopup="dialog"
                  onClick={toggleOptions}
                  className="inline-flex h-11 w-11 items-center justify-center rounded-xl border border-line bg-subtle text-ink-soft hover:bg-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent"
                >
                  <SlidersIcon className="h-4 w-4" />
                </button>
                <ComposerPrimitive.Send className="inline-flex h-11 items-center justify-center rounded-xl bg-accent-fill px-5 text-sm font-medium text-white hover:bg-accent-fill-strong focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-60">
                  发送
                </ComposerPrimitive.Send>
                {/* cancel is only meaningful while a run is in progress */}
                {busy && (
                  <ComposerPrimitive.Cancel className="inline-flex h-11 items-center justify-center rounded-xl border border-line-strong bg-surface px-4 text-sm text-ink-soft hover:bg-subtle focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-accent">
                    取消
                  </ComposerPrimitive.Cancel>
                )}
              </div>
            </ComposerPrimitive.Root>
          </div>
          <p className="mt-1 px-1 text-[11px] text-ink-faint">Enter 发送 · Shift+Enter 换行 · 模型/思考模式在发送时生效</p>
        </div>
      </div>

      {/* model/thinking popover — portaled so the composer's overflow-hidden
          collapse animation can't clip it; positioned above the options button */}
      {optionsOpen && popoverPos &&
        createPortal(
          <div
            ref={popoverRef}
            role="dialog"
            aria-label="选项"
            className="fixed z-50 w-72 max-w-[calc(100vw-1rem)] rounded-xl border border-line bg-surface p-3 shadow-lg"
            style={{ bottom: popoverPos.bottom, left: popoverPos.left }}
          >
            <div className="mb-2 flex items-center justify-between gap-2">
              <span className="text-[11px] font-semibold uppercase tracking-wider text-ink-faint">选项</span>
              {quarantined && <span className="text-xs text-warn-ink">{providerLabel} 已暂停，无法发送</span>}
              {archived && <span className="text-xs text-ink-faint">会话已归档</span>}
            </div>
            {modelPick && (
              <label className="mb-2 block text-xs text-ink-soft">
                模型
                <select
                  aria-label="模型"
                  value={model}
                  disabled={!!busy}
                  onChange={(e) => setModel(e.target.value)}
                  className="mt-1 w-full rounded-md border border-line bg-subtle px-2 py-1.5 text-[16px] sm:text-xs focus-visible:outline-2 focus-visible:outline-accent"
                >
                  <option value="">沿用当前模型（默认）</option>
                  {models.map((m) => (
                    <option key={m} value={m}>
                      {m}
                    </option>
                  ))}
                </select>
              </label>
            )}
            {thinkingSupported && (
              <label className="block text-xs text-ink-soft">
                思考模式
                <select
                  aria-label="思考模式"
                  value={thinking}
                  disabled={!!busy}
                  onChange={(e) => setThinking(e.target.value)}
                  className="mt-1 w-full rounded-md border border-line bg-subtle px-2 py-1.5 text-[16px] sm:text-xs focus-visible:outline-2 focus-visible:outline-accent"
                >
                  <option value="">保持网站当前设置</option>
                  <option value="standard">standard</option>
                  <option value="extended">extended</option>
                </select>
              </label>
            )}
          </div>,
          document.body,
        )}
    </ThreadPrimitive.Root>
  )
}
