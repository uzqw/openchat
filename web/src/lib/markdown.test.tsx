// Markdown safety: Gemini output is rendered verbatim but raw HTML must
// never become executable DOM and dangerous URL schemes must be dropped
// (prompts §6 "渲染时防止 XSS"). react-markdown turns raw HTML into
// escaped text (skipHtml=false renders raw nodes as text) and strips
// javascript:/vbscript:/data: URLs through its default urlTransform.

import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Markdown, normalizeMarkdown } from './markdown'

describe('Markdown', () => {
  it('renders normal Markdown faithfully', () => {
    const content = '# 标题\n\n**加粗** 和 `code`'
    const { container } = render(<Markdown content={content} />)
    expect(container.querySelector('h1')?.textContent).toBe('标题')
    expect(container.querySelector('strong')?.textContent).toBe('加粗')
    expect(container.querySelector('code')?.textContent).toBe('code')
  })

  it('renders inline and display math', () => {
    const content = '内联 $L_{KD}$。\n\n$$\nq_i = \\frac{\\exp(z_i/T)}{\\sum_j \\exp(z_j/T)}\n$$'
    const { container } = render(<Markdown content={content} />)
    expect(container.querySelector('.katex')).not.toBeNull()
    expect(container.querySelector('.katex-display')).not.toBeNull()
    expect(Array.from(container.querySelectorAll('annotation')).map((node) => node.textContent)).toContain(
      'q_i = \\frac{\\exp(z_i/T)}{\\sum_j \\exp(z_j/T)}',
    )
    expect(container.querySelector('.katex-html')?.textContent).not.toContain('q_i =')
  })

  it('does not mistake currency amounts for inline math', () => {
    const content = '价格 $10/月升级到 $30/月；公式 $L_{KD}$'
    const { container } = render(<Markdown content={content} />)

    expect(container.querySelector('p')?.textContent).toContain('$10/月升级到 $30/月；公式')
    expect(container.querySelectorAll('.katex')).toHaveLength(1)
  })

  it('renders GFM tables and repairs common Gemini table output', () => {
    const content = '1. 财务指标\n\n指标｜Q1\n营收｜$1B'
    const { container } = render(<Markdown content={content} />)
    const table = container.querySelector('table')
    expect(table).not.toBeNull()
    expect(table?.querySelectorAll('tr')).toHaveLength(2)
    expect(table?.querySelector('th')?.textContent).toBe('指标')
    expect(table?.querySelector('td')?.textContent).toBe('营收')
  })

  it('renders tab-separated tables returned by browser text extraction', () => {
    const content = 'Document\t类别\tQ1\nTAC\t流量获取成本\t$15.23B\n研发\t模型研发\t$15.11B'
    const { container } = render(<Markdown content={content} />)
    const table = container.querySelector('table')
    expect(table).not.toBeNull()
    expect(table?.querySelectorAll('tr')).toHaveLength(3)
    expect(table?.querySelectorAll('td')[2]?.textContent).toBe('$15.23B')
  })

  it('keeps malformed Gemini sections, diagrams, and table boundaries readable', () => {
    const content = `💬 一、 单 Agent 的瓶颈

三、 业务场景
场景领域\t角色\t价值
研究\tSearch Agent\t可追溯
四、 工程挑战

1. 主从编排 (Hierarchical)       2. 管道流水线 (Pipeline)
    [Orchestrator]                [Agent A] -> [Agent B]
     /      |      \\
 [Worker] [Worker] [Worker]`
    const { container } = render(<Markdown content={content} />)

    expect(container.querySelector('h2')?.textContent).toBe('一、 单 Agent 的瓶颈')
    expect(container.querySelectorAll('table tr')).toHaveLength(2)
    expect(container.querySelector('table')?.textContent).not.toContain('四、')
    expect(container.querySelectorAll('li')).toHaveLength(2)
    expect(container.querySelector('pre')?.textContent).toContain('[Orchestrator]')
    expect(container.textContent).not.toContain('💬')

    // The answer copy action uses the same normalized source as the renderer.
    const copied = normalizeMarkdown(content)
    expect(copied).toContain('## 一、 单 Agent 的瓶颈')
    expect(copied).toContain('```text')
    expect(copied).toContain('| 场景领域 | 角色 | 价值 |')
  })

  it('keeps fenced code exact and renders formulas', () => {
    const source = 'const value = left | right\n1. this is code\n2. still code'
    const content = `~~~ts\n${source}\n~~~\n\n$$\nf(x) = \\left|x\\right|\n$$`
    const { container } = render(<Markdown content={content} />)

    expect(container.querySelector('pre code')?.textContent).toBe(`${source}\n`)
    expect(normalizeMarkdown(content)).toBe(content)
    expect(container.querySelector('.katex')).not.toBeNull()
  })

  it('recovers legacy extracted ASCII diagrams as one code block', () => {
    const content = `说明：\n\n+-----+\n| left | right |\n+-----+\n\n公式：\n$$\n\\left|x\\right|\n$$`
    const { container } = render(<Markdown content={content} />)

    expect(container.querySelectorAll('pre code')).toHaveLength(1)
    expect(container.querySelector('pre code')?.textContent).toBe('+-----+\n| left | right |\n+-----+\n')
    expect(container.querySelector('.katex')).not.toBeNull()
    expect(normalizeMarkdown(content)).toContain('```text\n+-----+\n| left | right |\n+-----+\n```')
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

  it('strips Grok UI noise and recovers flattened diagram lines', () => {
    // Real grok.com extraction: one flattened line with UI artifacts.
    const content =
      'Worked for 24s 适合，但不要二选一。 各自适合什么 LangChain / LangGraph Temporal 本质 Agent / 图编排框架 持久工作流引擎 客服原型 很快 偏重。' +
      '\\u2060Temporal 推荐架构（生产常见） text Copy Copied 渠道(Web/企微/电话) → API（无状态） → Temporal Workflow（每个会话一个）' +
      ' ├ Signal：用户新消息 / 人工接管 / 结束会话 ├ Timer：超时关闭、SLA └ Activity：转人工、写工单 45 sources'
    const { container } = render(<Markdown content={content} />)

    expect(container.textContent).not.toContain('Worked for 24s')
    expect(container.textContent).not.toContain('\\u2060')
    expect(container.textContent).not.toContain('text Copy Copied')
    expect(container.textContent).not.toContain('Copy Copied')
    expect(container.textContent).not.toContain('45 sources')
    // the diagram is split back into lines, each starting with its tree marker
    const diagram = container.textContent ?? ''
    expect(diagram).toContain('├ Signal：用户新消息')
    expect(diagram).toContain('├ Timer：超时关闭')
    expect(diagram).toContain('└ Activity：转人工')

    // the copy action uses the same cleaned source
    const copied = normalizeMarkdown(content)
    expect(copied).not.toContain('Worked for')
    expect(copied).not.toContain('\\u2060')
    expect(copied).not.toContain('Copy Copied')
    expect(copied).not.toContain('sources')
    expect(copied).toContain('\n├ Signal：用户新消息')
  })

  it('keeps a short Grok answer intact after stripping the thinking prefix', () => {
    const content = 'Worked for 5s Hi — what’s up?'
    const { container } = render(<Markdown content={content} />)
    expect(container.textContent).toContain('Hi — what’s up?')
    expect(container.textContent).not.toContain('Worked for')
  })
})
