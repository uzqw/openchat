import { useState } from 'react'
import {
  ComposerPrimitive,
  MessagePrimitive,
  ThreadPrimitive,
} from '@assistant-ui/react'
import { useMessagePartText } from '@assistant-ui/react'
import { Link } from 'react-router-dom'
import { Markdown } from '../../lib/markdown'

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
      className="rounded-md border border-slate-200 bg-white px-2 py-1 text-xs text-slate-600 hover:bg-slate-50"
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
        <CopyButton text={text} label="复制原文" />
      </div>
    </div>
  )
}

function UserMessage() {
  return (
    <MessagePrimitive.Root className="flex justify-end py-2">
      <div className="max-w-[85%] rounded-2xl rounded-br-none bg-sky-600 px-4 py-2.5 text-sm leading-6 text-white shadow">
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
  return (
    <MessagePrimitive.Root className="flex flex-col gap-2 py-2">
      <div className="max-w-[85%] rounded-2xl rounded-bl-none border border-slate-200 bg-white px-4 py-3 shadow-sm">
        <div className="text-[15px] leading-7">
          <MessagePrimitive.Parts
            components={{
              Text: MarkdownText,
            }}
          />
        </div>
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
        const md = messages.map((m) => `${m.role === 'user' ? 'User' : 'Assistant'}: ${m.text}`).join('\n\n---\n\n')
        try {
          await navigator.clipboard.writeText(md)
          setCopied(true)
          setTimeout(() => setCopied(false), 1500)
        } catch {
          // ignore
        }
      }}
      className="rounded-md border border-slate-200 bg-white px-3 py-1.5 text-xs text-slate-600 hover:bg-slate-50"
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
  busy,
  quarantined,
  archived,
  conversationMessages,
}: {
  models: string[]
  model: string
  setModel: (v: string) => void
  thinking: string
  setThinking: (v: string) => void
  busy?: boolean
  quarantined?: boolean
  archived?: boolean
  conversationMessages?: { role: string; text: string }[]
}) {
  return (
    <ThreadPrimitive.Root className="flex h-[calc(100vh-8rem)] flex-col rounded-xl border border-slate-200 bg-slate-50">
      {conversationMessages && conversationMessages.length > 0 && (
        <div className="flex justify-end border-b border-slate-200 bg-white px-4 py-2">
          <CopyConversationButton messages={conversationMessages} />
        </div>
      )}
      <ThreadPrimitive.Viewport className="flex-1 overflow-y-auto px-4 py-4">
        <ThreadPrimitive.Empty>
          <div className="flex h-full items-center justify-center py-16 text-sm text-slate-500">
            {quarantined ? (
              <div className="space-y-2 text-center">
                <p>Gemini 已隔离，暂时无法创建新会话。</p>
                <p>
                  <Link className="text-sky-600 underline" to="/history">
                    前往历史记录确认 Chrome 已空闲
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

      <div className="border-t border-slate-200 bg-white p-3">
        <label htmlFor="prompt-input" className="mb-1 block text-sm font-medium text-slate-700">
          消息
        </label>
        {/* model/thinking controls - kept from original ChatPage for TOS compliance */}
        <div className="mb-3 flex flex-wrap items-center gap-3">
          <label className="flex items-center gap-2 text-xs text-slate-600">
            模型
            <select
              aria-label="模型"
              value={model}
              disabled={!!busy}
              onChange={(e) => setModel(e.target.value)}
              className="rounded-md border border-slate-300 bg-white px-2 py-1 text-xs"
            >
              <option value="">沿用当前模型（默认）</option>
              {models.map((m) => (
                <option key={m} value={m}>
                  {m}
                </option>
              ))}
            </select>
          </label>
          <label className="flex items-center gap-2 text-xs text-slate-600">
            思考模式
            <select
              aria-label="思考模式"
              value={thinking}
              disabled={!!busy}
              onChange={(e) => setThinking(e.target.value)}
              className="rounded-md border border-slate-300 bg-white px-2 py-1 text-xs"
            >
              <option value="">不改变网站当前值</option>
              <option value="standard">standard</option>
              <option value="extended">extended</option>
            </select>
          </label>
          {quarantined && <span className="text-xs text-amber-700">Gemini 已隔离，无法发送</span>}
          {archived && <span className="text-xs text-slate-500">会话已归档</span>}
        </div>

        <ComposerPrimitive.Root className="flex items-end gap-2">
          <ComposerPrimitive.Input
            placeholder={quarantined ? 'Gemini 已隔离，无法发送' : '输入问题，Enter 发送，Shift+Enter 换行'}
            disabled={!!busy || !!quarantined || !!archived}
            autoFocus
            // provide id so <label htmlFor> binds for accessibility/tests
            id="prompt-input"
            className="max-h-32 min-h-[44px] flex-1 resize-none rounded-xl border border-slate-300 bg-white px-3 py-2.5 text-sm placeholder:text-slate-400 focus-visible:outline-2 focus-visible:outline-sky-600 disabled:bg-slate-100"
          />
          <ComposerPrimitive.Send className="inline-flex h-11 items-center justify-center rounded-xl bg-sky-600 px-5 text-sm font-medium text-white hover:bg-sky-700 disabled:cursor-not-allowed disabled:bg-sky-300">
            发送
          </ComposerPrimitive.Send>
          <ComposerPrimitive.Cancel className="inline-flex h-11 items-center justify-center rounded-xl border border-slate-300 bg-white px-4 text-sm text-slate-700 hover:bg-slate-50">
            取消
          </ComposerPrimitive.Cancel>
        </ComposerPrimitive.Root>
        <p className="mt-1 text-[11px] text-slate-400">Enter 发送 · Shift+Enter 换行 · 模型/思考模式在发送时生效</p>
      </div>
    </ThreadPrimitive.Root>
  )
}
