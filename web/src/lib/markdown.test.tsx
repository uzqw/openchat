// Markdown safety: Gemini output is rendered verbatim but raw HTML must
// never become executable DOM and dangerous URL schemes must be dropped
// (prompts §6 "渲染时防止 XSS"). react-markdown turns raw HTML into
// escaped text (skipHtml=false renders raw nodes as text) and strips
// javascript:/vbscript:/data: URLs through its default urlTransform.

import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Markdown } from './markdown'

describe('Markdown', () => {
  it('renders normal Markdown faithfully', () => {
    const content = '# 标题\n\n**加粗** 和 `code`'
    const { container } = render(<Markdown content={content} />)
    expect(container.querySelector('h1')?.textContent).toBe('标题')
    expect(container.querySelector('strong')?.textContent).toBe('加粗')
    expect(container.querySelector('code')?.textContent).toBe('code')
  })

  it('never turns raw HTML into executable DOM', () => {
    const content = '<script>alert(1)</script>\n<img src=x onerror="alert(2)">\n<div onmouseover="alert(3)">hi</div>'
    const { container } = render(<Markdown content={content} />)
    expect(container.querySelector('script')).toBeNull()
    expect(container.querySelector('img')).toBeNull()
    expect(container.querySelector('[onerror]')).toBeNull()
    expect(container.querySelector('[onmouseover]')).toBeNull()
    // the raw HTML only survives as escaped text, never as markup
    expect(container.innerHTML).toContain('&lt;script&gt;')
    expect(container.innerHTML).not.toContain('<script>')
  })

  it('strips dangerous URL schemes from links', () => {
    const content = '[点击](javascript:alert(1)) 和 [安全](https://example.com)'
    const { container } = render(<Markdown content={content} />)
    const links = Array.from(container.querySelectorAll('a'))
    expect(links.length).toBeGreaterThan(0)
    for (const a of links) {
      const href = a.getAttribute('href') ?? ''
      expect(href.toLowerCase()).not.toMatch(/^javascript:/)
      expect(href.toLowerCase()).not.toMatch(/^data:/)
      expect(href.toLowerCase()).not.toMatch(/^vbscript:/)
    }
    // the safe link keeps its URL
    expect(container.querySelector('a[href="https://example.com"]')).not.toBeNull()
  })
})
