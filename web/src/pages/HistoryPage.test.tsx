// History pages: the list renders conversations with status badges, the
// detail is read-only (no input) and still offers "confirm the browser
// stopped generating" for unknown-outcome tasks so the pause can be lifted.

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
      { id: 'c1', title: '第一问', status: 'active', provider: 'gemini', created: '2026-01-01T00:00:00Z' },
      { id: 'c2', title: '第二问', status: 'archived', provider: 'gemini', created: '2026-01-02T00:00:00Z' },
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
      { id: 'c1', title: '第一问', status: 'active', provider: 'gemini', created: '2026-01-01T00:00:00Z' },
      { id: 'c2', title: '第二问', status: 'archived', provider: 'gemini', remote_id: 'aaaa1111aaaa1111', created: '2026-01-02T00:00:00Z' },
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
          return jsonResponse({ id: 'c2', title: '第二问', status: 'active', provider: 'gemini', remote_id: 'aaaa1111aaaa1111', created: '2026-01-02T00:00:00Z' })
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
      { id: 'c1', title: '旧会话', status: 'archived', provider: 'gemini', created: '2026-01-01T00:00:00Z' },
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

  it('filters the list by search query', async () => {
    const conversations: Conversation[] = [
      { id: 'c1', title: 'SQLite 索引优化', status: 'archived', provider: 'gemini', created: ISO },
      { id: 'c2', title: 'React 19 新特性', status: 'archived', provider: 'gemini', created: ISO },
    ]
    stubFetch([
      {
        match: m('GET', '/api/conversations'),
        handler: () =>
          jsonResponse({ items: conversations, page: 1, perPage: 50, totalItems: 2, totalPages: 1 }),
      },
    ])
    const user = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/history']}>
        <HistoryPage />
      </MemoryRouter>,
    )
    await screen.findByText('SQLite 索引优化')
    await user.type(screen.getByRole('searchbox', { name: '搜索历史会话' }), 'react')
    expect(screen.queryByText('SQLite 索引优化')).not.toBeInTheDocument()
    expect(screen.getByText('React 19 新特性')).toBeInTheDocument()
  })

  it('shows a no-match message when the search finds nothing', async () => {
    const conversations: Conversation[] = [
      { id: 'c1', title: 'SQLite 索引优化', status: 'archived', provider: 'gemini', created: ISO },
    ]
    stubFetch([
      {
        match: m('GET', '/api/conversations'),
        handler: () =>
          jsonResponse({ items: conversations, page: 1, perPage: 50, totalItems: 1, totalPages: 1 }),
      },
    ])
    const user = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/history']}>
        <HistoryPage />
      </MemoryRouter>,
    )
    await screen.findByText('SQLite 索引优化')
    await user.type(screen.getByRole('searchbox', { name: '搜索历史会话' }), '不存在的会话')
    expect(await screen.findByText(/没有找到匹配/)).toBeInTheDocument()
  })

  it('groups conversations by day with 今天/昨天/date headers', async () => {
    const at = (daysAgo: number) => {
      const d = new Date()
      d.setHours(0, 0, 0, 0)
      d.setDate(d.getDate() - daysAgo)
      return d.toISOString()
    }
    const conversations: Conversation[] = [
      { id: 'c1', title: '今天的问题', status: 'archived', provider: 'gemini', created: at(0) },
      { id: 'c2', title: '昨天的问题', status: 'archived', provider: 'gemini', created: at(1) },
      { id: 'c3', title: '更早的问题', status: 'archived', provider: 'gemini', created: at(3) },
    ]
    stubFetch([
      {
        match: m('GET', '/api/conversations'),
        handler: () =>
          jsonResponse({ items: conversations, page: 1, perPage: 50, totalItems: 3, totalPages: 1 }),
      },
    ])
    render(
      <MemoryRouter initialEntries={['/history']}>
        <HistoryPage />
      </MemoryRouter>,
    )
    expect(await screen.findByText('今天的问题')).toBeInTheDocument()
    expect(screen.getByText('今天')).toBeInTheDocument()
    expect(screen.getByText('昨天')).toBeInTheDocument()
    const older = new Date()
    older.setHours(0, 0, 0, 0)
    older.setDate(older.getDate() - 3)
    expect(screen.getByText(`${older.getMonth() + 1}月${older.getDate()}日`)).toBeInTheDocument()
  })

  it('loads more pages on demand', async () => {
    const all: Conversation[] = Array.from({ length: 60 }, (_, i) => ({
      id: `c${i}`,
      title: `会话 ${i}`,
      status: 'archived' as const,
      provider: 'gemini',
      created: ISO,
    }))
    stubFetch([
      {
        match: m('GET', '/api/conversations'),
        handler: (_p, _init, url) => {
          const page = Number(url.searchParams.get('page') ?? '1')
          const perPage = Number(url.searchParams.get('perPage') ?? '50')
          const items = all.slice((page - 1) * perPage, page * perPage)
          return jsonResponse({ items, page, perPage, totalItems: all.length, totalPages: Math.ceil(all.length / perPage) })
        },
      },
    ])
    const user = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/history']}>
        <HistoryPage />
      </MemoryRouter>,
    )
    expect(await screen.findByText('会话 0')).toBeInTheDocument()
    expect(screen.getAllByRole('link')).toHaveLength(50)
    await user.click(screen.getByRole('button', { name: '加载更多' }))
    expect(await screen.findByText('会话 59')).toBeInTheDocument()
    expect(screen.getAllByRole('link')).toHaveLength(60)
    expect(screen.queryByRole('button', { name: '加载更多' })).not.toBeInTheDocument()
  })

  it('loads all pages while searching so matches on later pages are found', async () => {
    const all: Conversation[] = Array.from({ length: 51 }, (_, i) => ({
      id: `c${i}`,
      title: i === 50 ? '特殊搜索目标' : `会话 ${i}`,
      status: 'archived' as const,
      provider: 'gemini',
      created: ISO,
    }))
    stubFetch([
      {
        match: m('GET', '/api/conversations'),
        handler: (_p, _init, url) => {
          const page = Number(url.searchParams.get('page') ?? '1')
          const perPage = Number(url.searchParams.get('perPage') ?? '50')
          const items = all.slice((page - 1) * perPage, page * perPage)
          return jsonResponse({ items, page, perPage, totalItems: all.length, totalPages: Math.ceil(all.length / perPage) })
        },
      },
    ])
    const user = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/history']}>
        <HistoryPage />
      </MemoryRouter>,
    )
    await screen.findByText('会话 0')
    await user.type(screen.getByRole('searchbox', { name: '搜索历史会话' }), '特殊搜索目标')
    expect(await screen.findByText('特殊搜索目标')).toBeInTheDocument()
    expect(screen.queryByText('会话 0')).not.toBeInTheDocument()
  })

  it('shows a retry button when the initial load fails and retries', async () => {
    let fail = true
    stubFetch([
      {
        match: m('GET', '/api/conversations'),
        handler: () => {
          if (fail) {
            fail = false
            return jsonResponse({ error: { code: 'boom', message: '服务器开小差了' } }, 500)
          }
          return jsonResponse({
            items: [{ id: 'c1', title: '第一问', status: 'archived', provider: 'gemini', created: ISO }],
            page: 1,
            perPage: 50,
            totalItems: 1,
            totalPages: 1,
          })
        },
      },
    ])
    const user = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/history']}>
        <HistoryPage />
      </MemoryRouter>,
    )
    expect(await screen.findByRole('alert')).toHaveTextContent('服务器开小差了')
    await user.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('第一问')).toBeInTheDocument()
  })

  it('keeps the loaded list and offers retry when loading more fails', async () => {
    const all: Conversation[] = Array.from({ length: 60 }, (_, i) => ({
      id: `c${i}`,
      title: `会话 ${i}`,
      status: 'archived' as const,
      provider: 'gemini',
      created: ISO,
    }))
    let failPage2 = true
    stubFetch([
      {
        match: m('GET', '/api/conversations'),
        handler: (_p, _init, url) => {
          const page = Number(url.searchParams.get('page') ?? '1')
          const perPage = Number(url.searchParams.get('perPage') ?? '50')
          if (page === 2 && failPage2) {
            failPage2 = false
            return jsonResponse({ error: { code: 'boom', message: '服务器开小差了' } }, 500)
          }
          const items = all.slice((page - 1) * perPage, page * perPage)
          return jsonResponse({ items, page, perPage, totalItems: all.length, totalPages: Math.ceil(all.length / perPage) })
        },
      },
    ])
    const user = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/history']}>
        <HistoryPage />
      </MemoryRouter>,
    )
    await screen.findByText('会话 0')
    await user.click(screen.getByRole('button', { name: '加载更多' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('服务器开小差了')
    // the first page stays visible
    expect(screen.getByText('会话 0')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('会话 59')).toBeInTheDocument()
    expect(screen.getAllByRole('link')).toHaveLength(60)
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
      { match: m('GET', '/api/providers'), handler: () => jsonResponse({ default_site: 'gemini', providers: [snap] }) },
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

  it('offers the confirm-browser-stopped action for unknown outcomes', async () => {
    const conv = readOnlyConv()
    const unknownTask = makeTask({ status: 'unknown_outcome', error_code: 'unknown_outcome' })
    conv.turns = [makeTurn({ id: 'tu2', prompt: '可能失败', tasks: [unknownTask], current_task: unknownTask })]
    const snap = makeSnapshot({ quarantined: true })

    const user = userEvent.setup()
    renderDetail(conv, snap)
    expect(await screen.findByText(/请求可能已经提交/, {}, { timeout: 6000 })).toBeInTheDocument()

    const ack = screen.getByRole('button', { name: '确认浏览器已停止生成' })
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
      { match: m('GET', '/api/providers'), handler: () => jsonResponse({ default_site: 'gemini', providers: [makeSnapshot()] }) },
      {
        match: (mm, p) => mm === 'POST' && p === '/api/conversations/c1/resume',
        handler: () => {
          calls.push('resume')
          return jsonResponse({ id: 'c1', title: '什么是 SQLite？', status: 'active', provider: 'gemini', remote_id: 'aaaa1111aaaa1111', created: ISO })
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

  it('shows a retry button when the detail load fails and recovers', async () => {
    let fail = true
    stubFetch([
      {
        match: m('GET', '/api/conversations/c1'),
        handler: () => {
          if (fail) {
            fail = false
            return jsonResponse({ error: { code: 'boom', message: '服务器开小差了' } }, 500)
          }
          return jsonResponse(readOnlyConv())
        },
      },
    ])
    const user = userEvent.setup()
    render(
      <MemoryRouter initialEntries={['/history/c1']}>
        <Routes>
          <Route path="/history/:id" element={<HistoryDetailPage />} />
        </Routes>
      </MemoryRouter>,
    )

    expect(await screen.findByRole('alert')).toHaveTextContent('服务器开小差了')
    expect(screen.queryByText('加载中…')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('SQLite 是嵌入式数据库。')).toBeInTheDocument()
  })

  it('shows task progress while a turn is pending or running', async () => {
    const conv = readOnlyConv()
    conv.turns = [
      makeTurn({
        id: 'tu1',
        prompt: '排队的问题',
        tasks: [makeTask({ id: 't1', status: 'pending' })],
        current_task: makeTask({ id: 't1', status: 'pending' }),
      }),
      makeTurn({
        id: 'tu2',
        prompt: '生成中的问题',
        tasks: [makeTask({ id: 't2', status: 'running' })],
        current_task: makeTask({ id: 't2', status: 'running' }),
      }),
    ]
    renderDetail(conv)

    expect(await screen.findByText('排队中')).toBeInTheDocument()
    expect(screen.getByText('生成中')).toBeInTheDocument()
    // one indeterminate bar per in-flight task; the running one also shows
    // skeleton lines where the answer will land
    expect(screen.getAllByRole('progressbar')).toHaveLength(2)
    expect(document.querySelectorAll('.animate-pulse')).toHaveLength(3)
  })

  it('hides 继续对话 without a saved remote session', async () => {
    renderDetail(readOnlyConv())
    expect(await screen.findByText('SQLite 是嵌入式数据库。')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '继续对话' })).not.toBeInTheDocument()
  })
})
