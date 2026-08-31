// Frontend behavior tests for the current-session page (prompts §6 and
// the prompt's frontend test list). The page runs against a stubbed fetch
// implementing the v1 REST API in memory; no real backend or Gemini is
// involved. Polling uses real timers (POLL_INTERVAL_MS = 800ms), so tests
// that follow a task to a terminal state take a couple of seconds.

import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ChatPage } from './ChatPage'
import { ISO, jsonResponse, makeConversation, makeSnapshot, makeTask, makeTurn, m, stubFetch } from '../test/helpers'
import type { Conversation, ConversationDetail, ProviderSnapshot, Task, Turn } from '../types'

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms))

function renderChat() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <ChatPage />
    </MemoryRouter>,
  )
}

function renderChatAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <ChatPage conversationId={path.replace('/chat/', '')} />
    </MemoryRouter>,
  )
}

/** In-memory fake backend for one test. */
class FakeBackend {
  conv: ConversationDetail | null = null
  listItems: Conversation[] | null = null
  snap: ProviderSnapshot = makeSnapshot()
  providers: ProviderSnapshot[] | null = null
  lastCreateBody: unknown = null
  /** queue of turn states served by GET /api/turns/:id, consumed in order */
  pollQueue: Task[] = []
  turn: Turn = makeTurn()
  getTurnCalls = 0
  lastTurnBody: unknown = null
  lastIdempotencyKey: string | null = null

  routes(): Parameters<typeof stubFetch>[0] {
    return [
      {
        match: m('GET', '/api/providers'),
        handler: () =>
          jsonResponse({ default_site: 'gemini', providers: this.providers ?? [this.snap] }),
      },
      {
        match: m('GET', '/api/conversations'),
        handler: () =>
          jsonResponse({
            items: this.listItems ?? (this.conv ? [this.toList()] : []),
            page: 1,
            perPage: 200,
            totalItems: this.listItems?.length ?? (this.conv ? 1 : 0),
            totalPages: this.listItems ? 1 : this.conv ? 1 : 0,
          }),
      },
      {
        match: m('POST', '/api/conversations'),
        handler: (_, init) => {
          this.lastCreateBody = init.body ? JSON.parse(String(init.body)) : {}
          this.conv = makeConversation('c1')
          return jsonResponse(this.toList(), 201)
        },
      },
      { match: m('GET', '/api/conversations/c1'), handler: () => jsonResponse(this.conv) },
      {
        match: (mm, p) => mm === 'POST' && p === '/api/conversations/c1/turns',
        handler: (_, init) => {
          const headers = new Headers(init.headers)
          this.lastIdempotencyKey = headers.get('Idempotency-Key')
          this.lastTurnBody = JSON.parse(String(init.body))
          const body = this.lastTurnBody as { prompt: string }
          const task = makeTask({ id: 't1', status: 'pending' })
          this.turn = makeTurn({ id: 'tu1', conversation: 'c1', prompt: body.prompt, tasks: [task], current_task: task })
          this.conv = makeConversation('c1', [...(this.conv?.turns ?? []), this.turn])
          return jsonResponse(this.turn, 202)
        },
      },
      {
        match: m('GET', '/api/turns/tu1'),
        handler: () => {
          this.getTurnCalls += 1
          const task = this.pollQueue.length > 0 ? (this.pollQueue.shift() as Task) : (this.turn.current_task as Task)
          this.turn = { ...this.turn, tasks: [task], current_task: task }
          // keep the conversation status and earlier turns intact
          const status = this.conv?.status ?? 'active'
          const earlier = (this.conv?.turns ?? []).filter((t) => t.id !== 'tu1')
          this.conv = makeConversation('c1', [...earlier, this.turn])
          this.conv.status = status
          return jsonResponse(this.turn)
        },
      },
      {
        match: (mm, p) => mm === 'POST' && p === '/api/tasks/t1/retry',
        handler: () => {
          // mirror the backend: a new retry task replaces the current one
          const task = makeTask({ id: 't2', turn: 'tu1', status: 'pending' })
          this.turn = { ...this.turn, tasks: [...this.turn.tasks, task], current_task: task }
          this.conv = makeConversation('c1', [this.turn])
          return jsonResponse(task, 202)
        },
      },
      {
        match: (mm, p) => mm === 'POST' && p === '/api/tasks/t1/acknowledge-unknown',
        handler: () => {
          // mirror the backend: stamp the acknowledgment and lift quarantine
          const task = makeTask({
            id: 't1',
            status: 'unknown_outcome',
            error_code: 'unknown_outcome',
            unknown_acknowledged_at: ISO,
          })
          this.turn = { ...this.turn, tasks: [task], current_task: task }
          this.conv = makeConversation('c1', [this.turn])
          this.conv.status = 'archived'
          this.snap = makeSnapshot({ ...this.snap, quarantined: false })
          return new Response(null, { status: 204 })
        },
      },
      {
        match: (mm, p) => mm === 'POST' && p === '/api/providers/gemini/login',
        handler: () => {
          this.snap = makeSnapshot({ ...this.snap, login_operation: 'queued' })
          return jsonResponse({ login_operation: 'queued' }, 202)
        },
      },
    ]
  }

  toList() {
    return {
      id: this.conv!.id,
      title: this.conv!.title,
      status: this.conv!.status,
      provider: this.conv!.provider,
      created: this.conv!.created,
    }
  }
}

beforeEach(() => {
  vi.restoreAllMocks()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('ChatPage', () => {
  it('loads the empty state and submits a question through pending/running/succeeded, stopping the poll at terminal', async () => {
    const backend = new FakeBackend()
    stubFetch(backend.routes())
    const user = userEvent.setup()
    renderChat()

    // empty state: no active conversation yet
    expect(await screen.findByText(/还没有会话/)).toBeInTheDocument()
    const textarea = screen.getByLabelText('消息')
    expect(textarea).toBeEnabled()

    // submit a question
    await user.type(textarea, '你好，Gemini')
    await user.click(screen.getByRole('button', { name: '发送' }))

    // pending is shown right after the turn is created
    expect(await screen.findByText(/排队中/)).toBeInTheDocument()
    expect(screen.getByText('你好，Gemini')).toBeInTheDocument()

    // running appears once polling observes it
    backend.pollQueue.push(
      makeTask({ id: 't1', status: 'running' }),
      makeTask({ id: 't1', status: 'succeeded', result: '**你好**，世界！', latency_ms: 42 }),
    )
    expect(await screen.findByText(/正在生成/, {}, { timeout: 6000 })).toBeInTheDocument()

    // succeeded renders the Markdown result and the generation timing
    expect(await screen.findByText('你好', { selector: 'strong' }, { timeout: 6000 })).toBeInTheDocument()
    expect(screen.getByText(/世界/)).toBeInTheDocument()
    expect(await screen.findByText('42ms', {}, { timeout: 6000 })).toBeInTheDocument()

    // polling stopped at the terminal state: no further GET /turns calls
    const getTurnCount = backend.getTurnCalls
    await sleep(2000)
    expect(backend.getTurnCalls).toBe(getTurnCount)
  })

  it('creates a new conversation from the header button', async () => {
    const backend = new FakeBackend()
    const fetchStub = stubFetch(backend.routes())
    const user = userEvent.setup()
    renderChat()
    await screen.findByText(/还没有会话/)

    await user.click(screen.getByRole('button', { name: '新建会话' }))
    // the button POSTs /api/conversations and loads the fresh conversation
    expect(fetchStub.calls).toContain('POST /api/conversations')
    expect(await screen.findByText('新会话')).toBeInTheDocument()
    expect(screen.getByLabelText('消息')).toBeEnabled()
  })

  it('creates a new conversation on the picked site provider', async () => {
    const backend = new FakeBackend()
    backend.snap = makeSnapshot({ site: 'grok', model_pick: false, thinking_supported: false, models: [] })
    backend.providers = [
      makeSnapshot(),
      makeSnapshot({ site: 'grok', model_pick: false, thinking_supported: false, models: [] }),
    ]
    const fetchStub = stubFetch(backend.routes())
    const user = userEvent.setup()
    renderChat()
    await screen.findByText(/还没有会话/)

    await user.click(screen.getByRole('button', { name: 'Grok' }))
    await user.click(screen.getByRole('button', { name: '新建会话' }))
    expect(fetchStub.calls).toContain('POST /api/conversations')
    expect(backend.lastCreateBody).toEqual({ provider: 'grok' })
    expect(await screen.findByText('新会话')).toBeInTheDocument()
  })

  it('finds the active conversation even when a newer archived conversation is listed first', async () => {
    const backend = new FakeBackend()
    backend.conv = makeConversation('c1', [
      makeTurn({
        id: 'tu1',
        prompt: '已恢复的会话',
        tasks: [makeTask({ id: 't1', status: 'succeeded', result: '历史回答' })],
        current_task: makeTask({ id: 't1', status: 'succeeded', result: '历史回答' }),
      }),
    ])
    backend.listItems = [
      { id: 'c2', title: '更新的归档会话', status: 'archived', provider: 'gemini', created: ISO },
      backend.toList(),
    ]
    stubFetch(backend.routes())
    renderChat()

    expect(await screen.findByText('已恢复的会话')).toBeInTheDocument()
    expect(await screen.findByText('历史回答')).toBeInTheDocument()
  })

  it('disables new conversation while the loaded conversation has a pending task', async () => {
    const backend = new FakeBackend()
    backend.conv = makeConversation('c1', [
      makeTurn({
        tasks: [makeTask({ id: 't1', status: 'running' })],
        current_task: makeTask({ id: 't1', status: 'running' }),
      }),
    ])
    stubFetch(backend.routes())
    renderChat()

    await screen.findByText('你好')
    expect(screen.getByRole('button', { name: '新建会话' })).toBeDisabled()
  })

  it('loads a pinned conversation at /chat/:id and offers 继续对话 when archived and resumable', async () => {
    const backend = new FakeBackend()
    backend.conv = makeConversation('c1', [
      makeTurn({
        id: 'tu1',
        prompt: '第一问',
        tasks: [makeTask({ id: 't1', status: 'succeeded', result: '回答一' })],
        current_task: makeTask({ id: 't1', status: 'succeeded', result: '回答一' }),
      }),
    ])
    backend.conv.status = 'archived'
    backend.conv.remote_id = 'aaaa1111aaaa1111'
    stubFetch(backend.routes())
    renderChatAt('/chat/c1')

    expect(await screen.findByText('第一问')).toBeInTheDocument()
    expect(await screen.findByText('回答一')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '继续对话' })).toBeInTheDocument()
    // archived: input disabled until resumed
    expect(screen.getByLabelText('消息')).toBeDisabled()
  })

  it('stops polling when the component unmounts', async () => {
    const backend = new FakeBackend()
    stubFetch(backend.routes())
    const user = userEvent.setup()
    const view = renderChat()
    await screen.findByText(/还没有会话/)

    await user.type(screen.getByLabelText('消息'), '问题')
    await user.click(screen.getByRole('button', { name: '发送' }))
    await screen.findByText(/排队中/)

    // the backend never reaches a terminal state
    backend.pollQueue.push(makeTask({ id: 't1', status: 'running' }))
    await screen.findByText(/正在生成/, {}, { timeout: 6000 })

    view.unmount()
    await sleep(400) // let any in-flight request settle
    const afterUnmount = backend.getTurnCalls
    await sleep(2200) // several poll intervals
    expect(backend.getTurnCalls).toBe(afterUnmount)
  })

  it('shows auth_required guidance for a first turn, then a successful login retry', async () => {
    const backend = new FakeBackend()
    stubFetch(backend.routes())
    const user = userEvent.setup()
    renderChat()
    await screen.findByText(/还没有会话/)

    await user.type(screen.getByLabelText('消息'), '问题')
    await user.click(screen.getByRole('button', { name: '发送' }))
    await screen.findByText(/排队中/)

    // first turn needs a login
    backend.pollQueue.push(makeTask({ id: 't1', status: 'auth_required', error_message: '需要登录' }))
    await screen.findByText(/需要登录 Gemini/)
    expect(screen.getByRole('button', { name: '去登录' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试' })).toBeInTheDocument()

    // login flow: queued → running → succeeded
    await user.click(screen.getByRole('button', { name: '去登录' }))
    expect(await screen.findByText(/等待执行/, {}, { timeout: 6000 })).toBeInTheDocument()

    const loginSnap: ProviderSnapshot = makeSnapshot({
      ...backend.snap,
      login_operation: 'running',
    })
    backend.snap = loginSnap
    expect(await screen.findByText(/请在可见 Chrome 中完成 Gemini 登录/, {}, { timeout: 6000 })).toBeInTheDocument()

    backend.snap = makeSnapshot({ ...backend.snap, login_operation: 'succeeded' })
    expect(await screen.findByText(/登录成功/, {}, { timeout: 6000 })).toBeInTheDocument()

    // retry after login succeeds
    await user.click(screen.getByRole('button', { name: '重试' }))
    backend.pollQueue.push(
      makeTask({ id: 't2', turn: 'tu1', status: 'running' }),
      makeTask({ id: 't2', turn: 'tu1', status: 'succeeded', result: '登录后的回答' }),
    )
    expect(await screen.findByText('登录后的回答', {}, { timeout: 6000 })).toBeInTheDocument()
  })

  it('archives the conversation when auth_required follows a success and blocks retry', async () => {
    const backend = new FakeBackend()
    backend.conv = makeConversation('c1', [
      makeTurn({
        id: 'tu0',
        prompt: '第一问',
        tasks: [makeTask({ id: 't0', status: 'succeeded', result: '先前回答' })],
        current_task: makeTask({ id: 't0', status: 'succeeded', result: '先前回答' }),
      }),
    ])
    stubFetch(backend.routes())
    const user = userEvent.setup()
    renderChat()

    // the active conversation renders with its prior success
    expect(await screen.findByText('先前回答')).toBeInTheDocument()

    await user.type(screen.getByLabelText('消息'), '第二问')
    await user.click(screen.getByRole('button', { name: '发送' }))
    await screen.findByText(/排队中/)

    // second turn hits auth_required → backend archives the conversation
    backend.pollQueue.push(makeTask({ id: 't1', status: 'auth_required', error_message: '需要登录' }))
    backend.conv = { ...backend.conv!, status: 'archived' }
    expect(await screen.findByText(/只读历史/, {}, { timeout: 6000 })).toBeInTheDocument()
    expect(await screen.findByText(/已归档为只读；登录后请新建会话/, {}, { timeout: 6000 })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重试' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '新建会话' })).toBeInTheDocument()
    // input is disabled on an archived conversation
    expect(screen.getByLabelText('消息')).toBeDisabled()
  })

  it('handles unknown_outcome: archives, quarantines, blocks direct retry and lifts via acknowledge', async () => {
    const backend = new FakeBackend()
    stubFetch(backend.routes())
    const user = userEvent.setup()
    renderChat()
    await screen.findByText(/还没有会话/)

    await user.type(screen.getByLabelText('消息'), '问题')
    await user.click(screen.getByRole('button', { name: '发送' }))
    await screen.findByText(/排队中/)

    // unknown outcome quarantines Gemini and archives the conversation
    backend.pollQueue.push(makeTask({ id: 't1', status: 'unknown_outcome', error_code: 'unknown_outcome' }))
    backend.snap = makeSnapshot({ ...backend.snap, quarantined: true })
    backend.conv = { ...backend.conv!, status: 'archived' }
    expect(await screen.findByText(/请求可能已经提交/, {}, { timeout: 6000 })).toBeInTheDocument()
    expect(await screen.findByText(/不可直接重试/, {}, { timeout: 6000 })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重试' })).not.toBeInTheDocument()
    expect(screen.getByRole('button', { name: '确认 Chrome 已空闲' })).toBeInTheDocument()
    expect(screen.getByText(/只读历史/)).toBeInTheDocument()
    expect(screen.getByLabelText('消息')).toBeDisabled()

    // acknowledging the unknown clears quarantine
    await user.click(screen.getByRole('button', { name: '确认 Chrome 已空闲' }))
    await waitFor(() => {
      expect(backend.snap.quarantined).toBe(false)
    })
    expect(await screen.findByText(/已确认，隔离已解除/, {}, { timeout: 6000 })).toBeInTheDocument()

    // a new conversation can be started afterwards
    backend.snap = makeSnapshot({ ...backend.snap, quarantined: false })
    await user.click(screen.getByRole('button', { name: '新建会话' }))
    expect(screen.getByLabelText('消息')).toBeEnabled()
  })

  it('offers model/thinking selection without faking per-model capabilities and sends the chosen values', async () => {
    const backend = new FakeBackend()
    backend.snap = makeSnapshot({ models: ['gemini-2.5-flash', 'gemini-2.5-pro'] })
    stubFetch(backend.routes())
    const user = userEvent.setup()
    renderChat()
    await screen.findByText(/还没有会话/)

    // thinking only exposes the honest fixed options (no per-model matrix)
    const thinking = screen.getByRole('combobox', { name: '思考模式' })
    const thinkingOptions = Array.from(thinking.querySelectorAll('option')).map((o) => o.textContent)
    expect(thinkingOptions).toEqual(['不改变网站当前值', 'standard', 'extended'])

    const model = screen.getByRole('combobox', { name: '模型' })
    const modelOptions = Array.from(model.querySelectorAll('option')).map((o) => o.textContent)
    expect(modelOptions).toEqual(['沿用当前模型（默认）', 'gemini-2.5-flash', 'gemini-2.5-pro'])

    await user.selectOptions(model, 'gemini-2.5-pro')
    await user.selectOptions(thinking, 'extended')
    await user.type(screen.getByLabelText('消息'), '模型与思考')
    await user.click(screen.getByRole('button', { name: '发送' }))
    await screen.findByText(/排队中/)

    await waitFor(() => {
      expect(backend.lastTurnBody).toEqual({ prompt: '模型与思考', model: 'gemini-2.5-pro', thinking: 'extended' })
      expect(backend.lastIdempotencyKey).toBeTruthy()
    })
  })

  it('hides model/thinking selectors for a provider without those knobs (grok)', async () => {
    const backend = new FakeBackend()
    backend.snap = makeSnapshot({
      site: 'grok',
      model_pick: false,
      thinking_supported: false,
      models: [],
    })
    stubFetch(backend.routes())
    const user = userEvent.setup()
    renderChat()
    await screen.findByText(/还没有会话/)

    expect(screen.queryByRole('combobox', { name: '模型' })).not.toBeInTheDocument()
    expect(screen.queryByRole('combobox', { name: '思考模式' })).not.toBeInTheDocument()

    // a plain grok ask still sends, without model/thinking
    await user.type(screen.getByLabelText('消息'), '你好 Grok')
    await user.click(screen.getByRole('button', { name: '发送' }))
    await screen.findByText(/排队中/)
    await waitFor(() => {
      expect(backend.lastTurnBody).toEqual({ prompt: '你好 Grok' })
    })
  })

  it('labels a Grok conversation and shows its generation timing', async () => {
    const backend = new FakeBackend()
    backend.snap = makeSnapshot({ site: 'grok', model_pick: false, thinking_supported: false, models: [] })
    backend.conv = makeConversation('c1', [
      makeTurn({
        id: 'tu1',
        conversation: 'c1',
        prompt: '你好 Grok',
        tasks: [makeTask({ id: 't1', status: 'succeeded', result: 'Grok 回答', latency_ms: 1234 })],
      }),
    ])
    backend.conv.provider = 'grok'
    stubFetch(backend.routes())
    renderChat()

    expect(await screen.findByText('Grok 回答', {}, { timeout: 6000 })).toBeInTheDocument()
    expect(screen.getByText('Grok', { selector: 'span.text-slate-400' })).toBeInTheDocument()
    expect(screen.getByText('1234ms')).toBeInTheDocument()
  })

  it('shows the quarantine hint instead of an input when there is no active conversation', async () => {
    const backend = new FakeBackend()
    backend.snap = makeSnapshot({ quarantined: true })
    stubFetch(backend.routes())
    renderChat()

    expect(await screen.findByText(/暂时无法创建新会话/)).toBeInTheDocument()
    expect(screen.getByText(/前往历史记录确认 Chrome 已空闲/)).toBeInTheDocument()
    expect(screen.getByLabelText('消息')).toBeDisabled()
  })
})
