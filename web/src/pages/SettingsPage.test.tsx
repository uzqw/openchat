// Settings page: backend/Bridge/login state rendering and the login gate
// mirror of the backend rules (quarantine / write guard / success-ful
// active conversation disable the shared-tab login action).

import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { SettingsPage } from './SettingsPage'
import { jsonResponse, makeConversation, makeSnapshot, makeTask, makeTurn, m, stubFetch } from '../test/helpers'
import type { ProviderSnapshot } from '../types'

beforeEach(() => {
  vi.restoreAllMocks()
})

afterEach(() => {
  vi.unstubAllGlobals()
})

function renderSettings(snap: ProviderSnapshot, activeConversation = false) {
  stubFetch([
    { match: m('GET', '/api/providers'), handler: () => jsonResponse({ default_site: 'gemini', providers: [snap] }) },
    {
      match: m('GET', '/api/conversations'),
      handler: () => {
        const items = activeConversation
          ? [{ id: 'c1', title: '进行中', status: 'active', created: '2026-01-01T00:00:00Z' }]
          : []
        return jsonResponse({ items, page: 1, perPage: 1, totalItems: items.length, totalPages: items.length ? 1 : 0 })
      },
    },
  ])
  return render(
    <MemoryRouter initialEntries={['/settings']}>
      <SettingsPage />
    </MemoryRouter>,
  )
}

describe('SettingsPage', () => {
  it('renders backend, Bridge, login state and the model list', async () => {
    renderSettings(
      makeSnapshot({
        version: '1.8.7',
        bridge: 'Bridge Extension 1.0.23',
        logged_in: true,
        models: ['gemini-2.5-flash', 'gemini-2.5-pro'],
        login_operation: 'succeeded',
      }),
    )

    expect(await screen.findByText('1.8.7')).toBeInTheDocument()
    expect(screen.getByText('Bridge Extension 1.0.23')).toBeInTheDocument()
    expect(screen.getByText('已登录')).toBeInTheDocument()
    expect(screen.getByText('gemini-2.5-flash')).toBeInTheDocument()
    expect(screen.getByText('gemini-2.5-pro')).toBeInTheDocument()
    // already logged in: the login action is disabled (a redundant login
    // hangs the OpenCLI side and would wedge the FIFO queue)
    expect(screen.getByText('当前已登录，无需登录操作。')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '去登录' })).toBeDisabled()
  })

  it('disables login while quarantined', async () => {
    renderSettings(makeSnapshot({ quarantined: true }))
    expect(await screen.findByText(/已隔离/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '去登录' })).toBeDisabled()
  })

  it('disables login when the active conversation already succeeded', async () => {
    const conv = makeConversation('c1', [
      makeTurn({
        id: 'tu1',
        prompt: '问',
        tasks: [makeTask({ id: 't1', status: 'succeeded', result: '答' })],
        current_task: makeTask({ id: 't1', status: 'succeeded', result: '答' }),
      }),
    ])
    stubFetch([
      { match: m('GET', '/api/providers'), handler: () => jsonResponse({ default_site: 'gemini', providers: [makeSnapshot()] }) },
      {
        match: m('GET', '/api/conversations'),
        handler: () =>
          jsonResponse({
            items: [{ id: 'c1', title: conv.title, status: 'active', created: '2026-01-01T00:00:00Z' }],
            page: 1,
            perPage: 1,
            totalItems: 1,
            totalPages: 1,
          }),
      },
      { match: m('GET', '/api/conversations/c1'), handler: () => jsonResponse(conv) },
    ])
    render(
      <MemoryRouter initialEntries={['/settings']}>
        <SettingsPage />
      </MemoryRouter>,
    )
    expect(await screen.findByText(/已有成功回答/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '去登录' })).toBeDisabled()
  })
})
