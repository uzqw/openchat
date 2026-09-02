// Shell tests: the restructured sidebar (新会话 on top, conversation list as
// the body, 历史/设置 in the footer) and the mobile/tablet conversations
// drawer (hamburger opens it; Escape, backdrop, close button and navigation
// close it). The drawer and the aside are both in the DOM in jsdom, so
// assertions are scoped with within().

import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App'
import { jsonResponse, m, stubFetch } from './test/helpers'
import type { Conversation, ConversationDetail } from './types'

const conversations: Conversation[] = [
  { id: 'c1', title: '第一问', status: 'active', provider: 'gemini', created: '2026-01-01T00:00:00Z' },
  { id: 'c2', title: '第二问', status: 'archived', provider: 'gemini', created: '2026-01-02T00:00:00Z' },
]

function routes() {
  return [
    {
      match: m('GET', '/api/providers'),
      handler: () =>
        jsonResponse({
          default_site: 'gemini',
          providers: [
            {
              site: 'gemini',
              model_pick: true,
              thinking_supported: true,
              version: '1.8.7',
              bridge: 'Bridge Extension 1.0.23',
              models: [],
              logged_in: true,
              login_operation: 'idle',
              quarantined: false,
            },
          ],
        }),
    },
    {
      match: m('GET', '/api/conversations'),
      handler: () => jsonResponse({ items: conversations, page: 1, perPage: 200, totalItems: 2, totalPages: 1 }),
    },
    {
      match: m('GET', '/api/conversations/c1'),
      handler: () =>
        jsonResponse({
          id: 'c1',
          title: '第一问',
          status: 'active',
          provider: 'gemini',
          created: '2026-01-01T00:00:00Z',
          turns: [],
        } satisfies ConversationDetail),
    },
    {
      match: m('POST', '/api/conversations'),
      handler: () => jsonResponse({ id: 'c3', title: '新会话', status: 'active', provider: 'gemini', created: '2026-01-03T00:00:00Z' }, 201),
    },
    {
      match: m('GET', '/api/conversations/c3'),
      handler: () =>
        jsonResponse({
          id: 'c3',
          title: '新会话',
          status: 'active',
          provider: 'gemini',
          created: '2026-01-03T00:00:00Z',
          turns: [],
        } satisfies ConversationDetail),
    },
  ]
}

function renderApp(path = '/') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  )
}

beforeEach(() => {
  vi.restoreAllMocks()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('App shell', () => {
  it('sidebar: 新会话 on top, conversation list as the body, 历史/设置 in the footer', async () => {
    stubFetch(routes())
    renderApp()
    const aside = document.querySelector('aside') as HTMLElement

    // 新会话 entry at the top, before the conversation list
    const newButton = within(aside).getByRole('button', { name: '新会话' })
    expect(newButton).toBeInTheDocument()
    expect(newButton.compareDocumentPosition(await within(aside).findByText('第一问')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()

    // the list is the sidebar body: active → /chat/:id, archived → /history/:id
    expect(within(aside).getByRole('link', { name: /第一问/ })).toHaveAttribute('href', '/chat/c1')
    expect(within(aside).getByRole('link', { name: /第二问/ })).toHaveAttribute('href', '/history/c2')

    // the old 当前会话/历史会话 section headers are gone; 历史/设置 live in the footer
    expect(within(aside).queryByText('当前会话')).not.toBeInTheDocument()
    expect(within(aside).queryByText('历史会话')).not.toBeInTheDocument()
    expect(within(aside).getByRole('link', { name: '历史' })).toHaveAttribute('href', '/history')
    expect(within(aside).getByRole('link', { name: '设置' })).toHaveAttribute('href', '/settings')
  })

  it('mobile: hamburger opens the conversations drawer; Escape, backdrop and close button close it', async () => {
    stubFetch(routes())
    const user = userEvent.setup()
    renderApp()

    const hamburger = screen.getByRole('button', { name: '打开会话列表' })
    expect(hamburger).toHaveAttribute('aria-expanded', 'false')

    await user.click(hamburger)
    expect(hamburger).toHaveAttribute('aria-expanded', 'true')
    const dialog = screen.getByRole('dialog', { name: '会话列表' })
    expect(within(dialog).getByRole('button', { name: '新会话' })).toBeInTheDocument()
    expect(await within(dialog).findByText('第一问')).toBeInTheDocument()
    expect(within(dialog).getByRole('link', { name: '历史' })).toBeInTheDocument()

    // Escape closes
    await user.keyboard('{Escape}')
    expect(hamburger).toHaveAttribute('aria-expanded', 'false')

    // backdrop closes
    await user.click(hamburger)
    await user.click(screen.getByTestId('drawer-backdrop'))
    expect(hamburger).toHaveAttribute('aria-expanded', 'false')

    // close button closes
    await user.click(hamburger)
    await user.click(screen.getByRole('button', { name: '关闭会话列表' }))
    expect(hamburger).toHaveAttribute('aria-expanded', 'false')
  })

  it('新会话 in the drawer creates a conversation, navigates to it and closes the drawer', async () => {
    const fetchStub = stubFetch(routes())
    const user = userEvent.setup()
    renderApp()

    await user.click(screen.getByRole('button', { name: '打开会话列表' }))
    const dialog = screen.getByRole('dialog', { name: '会话列表' })
    await user.click(within(dialog).getByRole('button', { name: '新会话' }))

    // created via the API and the chat page loads the pinned conversation
    expect(fetchStub.calls).toContain('POST /api/conversations')
    await waitFor(() => expect(fetchStub.calls).toContain('GET /api/conversations/c3'))
    // navigation closed the drawer
    expect(screen.getByRole('button', { name: '打开会话列表' })).toHaveAttribute('aria-expanded', 'false')
  })
})
