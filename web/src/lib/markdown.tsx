// Gemini Markdown rendering. react-markdown never emits raw HTML and
// strips javascript:/vbscript:/data: URLs via its default urlTransform,
// so the Gemini result can be rendered verbatim without an XSS surface.

import { useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

// 修复 Gemini 常见表格不规范：列表后缺少空行、缺少分隔行、全角竖线
// 之前 screenshot 的表格全部挤在一行就是因为 GFM 解析失败，回退成了普通段落
function normalizeMarkdown(content: string): string {
  const lines = content.split('\n')
  const out: string[] = []
  let inFence = false
  let inTabTable = false
  const isFence = (line: string) => /^\s*(`{3,}|~{3,})/.test(line)
  const isSep = (line: string) => /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$/.test(line)
  const cells = (line: string, separator: string) => line.split(separator).map((c) => c.trim())
  const isPipeRow = (line: string) => {
    if (!line.includes('|')) return false
    return cells(line, '|').filter((c) => c !== '').length >= 2
  }
  const isTabRow = (line: string) => {
    if (!line.includes('\t')) return false
    return cells(line, '\t').filter((c) => c !== '').length >= 2
  }
  const asPipeRow = (line: string) => `| ${cells(line, '\t').map((c) => c.replaceAll('|', '\\|')).join(' | ')} |`

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (isFence(line)) {
      out.push(line)
      inFence = !inFence
      inTabTable = false
      continue
    }
    if (inFence) {
      out.push(line)
      continue
    }
    const normalizedLine = line.replace(/[｜│┃]/g, '|')
    const tab = isTabRow(normalizedLine)
    const nextIsTab = isTabRow((lines[i + 1] ?? '').replace(/[｜│┃]/g, '|'))
    if (tab && (nextIsTab || inTabTable)) {
      if (!inTabTable) {
        const prev = out[out.length - 1] ?? ''
        if (out.length > 0 && prev.trim() !== '' && !isPipeRow(prev)) out.push('')
        out.push(asPipeRow(normalizedLine))
        const n = Math.max(cells(normalizedLine, '\t').length, 2)
        out.push('| ' + Array(n).fill('---').join(' | ') + ' |')
      } else {
        out.push(asPipeRow(normalizedLine))
      }
      inTabTable = true
      continue
    }
    inTabTable = false

    const pipe = isPipeRow(normalizedLine)
    const sep = isSep(normalizedLine)
    const prev = out[out.length - 1] ?? ''
    const prevIsBlank = prev.trim() === ''
    const prevIsPipe = isPipeRow(prev)
    const prevIsSep = isSep(prev)
    if (pipe && !sep && out.length > 0 && !prevIsBlank && !prevIsPipe && !prevIsSep) {
      out.push('')
    }
    out.push(normalizedLine)
    if (pipe && !sep && !prevIsPipe && !prevIsSep) {
      const next = (lines[i + 1] ?? '').replace(/[｜│┃]/g, '|')
      if (isPipeRow(next) && !isSep(next)) {
        const n = Math.max(cells(normalizedLine, '|').filter((c) => c !== '').length, 2)
        out.push('| ' + Array(n).fill('---').join(' | ') + ' |')
      }
    }
  }
  return out.join('\n')
}

function Pre(props: React.HTMLAttributes<HTMLPreElement>) {
  const { children } = props as { children: React.ReactNode }
  const ref = useRef<HTMLPreElement>(null)
  const [copied, setCopied] = useState(false)
  async function copy() {
    const text = ref.current?.innerText ?? ''
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 1200)
    } catch {
      // fallback: select
      const r = document.createRange()
      if (ref.current) {
        r.selectNodeContents(ref.current)
        const sel = window.getSelection()
        sel?.removeAllRanges()
        sel?.addRange(r)
      }
    }
  }
  return (
    <pre ref={ref} className="group relative">
      <button
        type="button"
        onClick={copy}
        aria-label="复制代码"
        className="absolute right-2 top-2 rounded-md border border-slate-600 bg-slate-800 px-2 py-1 text-xs text-slate-200 opacity-80 hover:bg-slate-700 group-hover:opacity-100"
      >
        {copied ? '已复制' : '复制'}
      </button>
      {children}
    </pre>
  )
}

export function Markdown({ content }: { content: string }) {
  const normalized = normalizeMarkdown(content)
  return (
    <div className="markdown text-[15px] leading-7">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          table: ({ children }) => (
            <div className="-mx-4 overflow-x-auto px-4">
              <table className="min-w-[600px]">{children}</table>
            </div>
          ),
          pre: Pre,
          a: ({ children, ...props }) => (
            <a {...props} target="_blank" rel="noreferrer noopener">
              {children}
            </a>
          ),
        }}
      >
        {normalized}
      </ReactMarkdown>
    </div>
  )
}
