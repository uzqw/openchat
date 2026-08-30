// History pages: the list renders conversations with status badges, the
// detail is read-only (no input) and still offers "confirm Chrome is
// idle" for unknown-outcome tasks so quarantine can be lifted.

import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter, Route, Routes } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { HistoryDetailPage, HistoryPage } from './HistoryPage'
import { ISO, jsonResponse, makeConversation, makeSnapshot, makeTask, makeTurn, m, stubFetch } from '../test/helpers'
import type { Conversation, ProviderSnapshot } from '../types'

beforeEach(() => {
  vi.restoreAllMocks()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('HistoryPage', () => {
  it('lists conversations with status badges', async () => {
    const conversations: Conversation[] = [
      { id: 'c1', title: '第一问', status: 'active', created: '2026-01-01T00:00:00Z' },
      { id: 'c2', title: '第二问', status: 'archived', created: '2026-01-02T00:00:00Z' },
    ]
    stubFetch([
      {
        match: m('GET', '/api/conversations'),
        handler: () =>
          jsonResponse({ items: conversations, page: 1, perPage: 200, totalItems: 2, totalPages: 1 }),
      },
    ])
    render(
      <MemoryRouter initialEntries={['/history']}>
        <HistoryPage />
      </MemoryRouter>,
    )

    expect(await screen.findByText('第一问')).toBeInTheDocument()
    expect(screen.getByText('第二问')).toBeInTheDocument()
    expect(screen.getByText('当前')).toBeInTheDocument()
    expect(screen.getByText('已归档')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /第一问/ })).toHaveAttribute('href', '/history/c1')
  })

  it('shows an empty state when there is no history', async () => {
    stubFetch([
      {
        match: m('GET', '/api/conversations'),
        handler: () => jsonResponse({ items: [], page: 1, perPage: 200, totalItems: 0, totalPages: 0 }),
      },
    ])
    render(
      <MemoryRouter initialEntries={['/history']}>
        <HistoryPage />
      </MemoryRouter>,
    )
    expect(await screen.findByText(/还没有历史会话/)).toBeInTheDocument()
  })

  it('offers 继续对话 for resumable archived conversations and navigates to /chat/:id', async () => {
    const conversations: Conversation[] = [
      { id: 'c1', title: '第一问', status: 'active', created: '2026-01-01T00:00:00Z' },
      { id: 'c2', title: '第二问', status: 'archived', remote_id: 'aaaa1111aaaa1111', created: '2026-01-02T00:00:00Z' },
    ]
    const calls: string[] = []
    stubFetch([
      {
        match: m('GET', '/api/conversations'),
        handler: () =>
          jsonResponse({ items: conversations, page: 1, perPage: 200, totalItems: 2, totalPages: 1 }),
      },
      {
        match: (mm, p) => mm === 'POST' && p === '/api/conversations/c2/resume',
        handler: () => {
          calls.push('resume')
          return jsonResponse({ id: 'c2', title: '第二问', status: 'active', remote_id: 'aaaa1111aaaa1111', created: '2026-01-02T00:00:00Z' })
        },
      },
    ])
    const user = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/history']}>
        <Routes>
          <Route path="/history" element={<HistoryPage />} />
          <Route path="/chat/:id" element={<div>chat page for :id</div>} />
        </Routes>
      </MemoryRouter>,
    )

    const btn = await screen.findByRole('button', { name: '继续对话' })
    expect(btn).toBeEnabled()
    await user.click(btn)
    expect(calls).toEqual(['resume'])
    expect(await screen.findByText('chat page for :id')).toBeInTheDocument()
  })

  it('disables 继续对话 for archived conversations without a remote session', async () => {
    const conversations: Conversation[] = [
      { id: 'c1', title: '旧会话', status: 'archived', created: '2026-01-01T00:00:00Z' },
    ]
    stubFetch([
      {
        match: m('GET', '/api/conversations'),
        handler: () =>
          jsonResponse({ items: conversations, page: 1, perPage: 200, totalItems: 1, totalPages: 1 }),
      },
    ])
    render(
      <MemoryRouter initialEntries={['/history']}>
        <HistoryPage />
      </MemoryRouter>,
    )
    const btn = await screen.findByRole('button', { name: '继续对话' })
    expect(btn).toBeDisabled()
  })
})

describe('HistoryDetailPage', () => {
  const readOnlyConv = () =>
    makeConversation('c1', [
      makeTurn({
        id: 'tu1',
        prompt: '什么是 SQLite？',
        tasks: [makeTask({ id: 't1', status: 'succeeded', result: 'SQLite 是嵌入式数据库。' })],
        current_task: makeTask({ id: 't1', status: 'succeeded', result: 'SQLite 是嵌入式数据库。' }),
      }),
    ])

  function renderDetail(conv: ReturnType<typeof makeConversation>, snap: ProviderSnapshot = makeSnapshot()) {
    stubFetch([
      { match: m('GET', '/api/conversations/c1'), handler: () => jsonResponse(conv) },
      { match: m('GET', '/api/providers/gemini'), handler: () => jsonResponse(snap) },
      {
        match: (mm, p) => mm === 'POST' && p === '/api/tasks/t1/acknowledge-unknown',
        handler: () => new Response(null, { status: 204 }),
      },
    ])
    return render(
      <MemoryRouter initialEntries={['/history/c1']}>
        <Routes>
          <Route path="/history/:id" element={<HistoryDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )
  }

  it('is read-only: renders the result without any input or send button', async () => {
    renderDetail(readOnlyConv())
    expect(await screen.findByText('SQLite 是嵌入式数据库。')).toBeInTheDocument()
    expect(screen.getByText(/只读历史/)).toBeInTheDocument()
    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '发送' })).not.toBeInTheDocument()
    // no retry on an archived conversation
    expect(screen.queryByRole('button', { name: '重试' })).not.toBeInTheDocument()
  })

  it('offers the confirm-Chrome-idle action for unknown outcomes', async () => {
    const conv = readOnlyConv()
    const unknownTask = makeTask({ status: 'unknown_outcome', error_code: 'unknown_outcome' })
    conv.turns = [makeTurn({ id: 'tu2', prompt: '可能失败', tasks: [unknownTask], current_task: unknownTask })]
    const snap = makeSnapshot({ quarantined: true })

    const user = userEvent.setup()
    renderDetail(conv, snap)
    expect(await screen.findByText(/请求可能已经提交/, {}, { timeout: 6000 })).toBeInTheDocument()

    const ack = screen.getByRole('button', { name: '确认 Chrome 已空闲' })
    await user.click(ack)
    // ack is served; the page reloads the conversation (still read-only)
    expect(screen.getByText(/只读历史/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '重试' })).not.toBeInTheDocument()
  })

  it('offers 继续对话 when the conversation has a saved remote session', async () => {
    const conv = readOnlyConv()
    conv.remote_id = 'aaaa1111aaaa1111'
    const calls: string[] = []
    stubFetch([
      { match: m('GET', '/api/conversations/c1'), handler: () => jsonResponse(conv) },
      { match: m('GET', '/api/providers/gemini'), handler: () => jsonResponse(makeSnapshot()) },
      {
        match: (mm, p) => mm === 'POST' && p === '/api/conversations/c1/resume',
        handler: () => {
          calls.push('resume')
          return jsonResponse({ id: 'c1', title: '什么是 SQLite？', status: 'active', remote_id: 'aaaa1111aaaa1111', created: ISO })
        },
      },
    ])
    const user = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/history/c1']}>
        <Routes>
          <Route path="/history/:id" element={<HistoryDetailPage />} />
          <Route path="/chat/:id" element={<div>chat page for :id</div>} />
        </Routes>
      </MemoryRouter>,
    )
    const btn = await screen.findByRole('button', { name: '继续对话' })
    await user.click(btn)
    expect(calls).toEqual(['resume'])
    expect(await screen.findByText('chat page for :id')).toBeInTheDocument()
  })

  it('hides 继续对话 without a saved remote session', async () => {
    renderDetail(readOnlyConv())
    expect(await screen.findByText('SQLite 是嵌入式数据库。')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '继续对话' })).not.toBeInTheDocument()
  })
})
