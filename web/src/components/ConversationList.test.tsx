// Sidebar conversation list: shows conversations with a provider prefix,
// links active ones to /chat/:id and archived ones to /history/:id.

import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ConversationList } from './ConversationList'
import { jsonResponse, m, stubFetch } from '../test/helpers'
import type { Conversation } from '../types'

beforeEach(() => {
  vi.restoreAllMocks()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('ConversationList', () => {
  it('links active to /chat and archived to /history with provider prefix', async () => {
    const conversations: Conversation[] = [
      { id: 'c1', title: '第一问', status: 'active', provider: 'grok', created: '2026-01-01T00:00:00Z' },
      { id: 'c2', title: '第二问', status: 'archived', provider: 'gemini', created: '2026-01-02T00:00:00Z' },
    ]
    stubFetch([
      {
        match: m('GET', '/api/conversations'),
        handler: () => jsonResponse({ items: conversations, page: 1, perPage: 100, totalItems: 2, totalPages: 1 }),
      },
    ])
    render(
      <MemoryRouter>
        <ConversationList />
      </MemoryRouter>,
    )

    expect(await screen.findByText('第一问')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /第一问/ })).toHaveAttribute('href', '/chat/c1')
    expect(screen.getByRole('link', { name: /第二问/ })).toHaveAttribute('href', '/history/c2')
    expect(screen.getByText('Grok')).toBeInTheDocument()
    expect(screen.getByText('Gem')).toBeInTheDocument()
  })

  it('shows an empty hint when there are no conversations', async () => {
    stubFetch([
      {
        match: m('GET', '/api/conversations'),
        handler: () => jsonResponse({ items: [], page: 1, perPage: 100, totalItems: 0, totalPages: 0 }),
      },
    ])
    render(
      <MemoryRouter>
        <ConversationList />
      </MemoryRouter>,
    )
    expect(await screen.findByText('还没有会话')).toBeInTheDocument()
  })
})