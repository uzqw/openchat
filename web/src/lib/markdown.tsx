// Gemini Markdown rendering. react-markdown never emits raw HTML and
// strips javascript:/vbscript:/data: URLs via its default urlTransform,
// so the Gemini result can be rendered verbatim without an XSS surface.

import { useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import rehypeKatex from 'rehype-katex'
import remarkGfm from 'remark-gfm'
import remarkMath from 'remark-math'

// Gemini 常把列表、表格和 ASCII 图直接拼在一起。只修复这些已知的
// 结构性丢失，不把普通正文当成 HTML 或任意 Markdown 执行。
// Shared by the Markdown copy action so copied content matches the rendered source.
// eslint-disable-next-line react-refresh/only-export-components
export function normalizeMarkdown(content: string): string {
  const isFence = (line: string) => /^\s*(`{3,}|~{3,})/.test(line)
  const isSectionHeading = (line: string) => /^[一二三四五六七八九十]+、\s+.+$/.test(line.trim())
  const isDiagramLine = (line: string) => {
    const diagramText = line.replace(/\[[^\]]+\]\([^)]*\)/g, '')
    const trimmed = diagramText.trim()
    return (
      /\[[^\]]+\].*\[[^\]]+\]/.test(diagramText) ||
      (/^[\\/| ]+$/.test(trimmed) && /[\\/|]/.test(trimmed))
    )
  }

  // OpenCLI's confirmed display wrapper is not part of the Gemini answer.
  const prepared: string[] = []
  let inFence = false
  for (const rawLine of content.replace(/^💬 /, '').split('\n')) {
    if (isFence(rawLine)) {
      inFence = !inFence
      prepared.push(rawLine)
      continue
    }
    if (!inFence && /^\s*\d+[.)]\s+/.test(rawLine)) {
      // Gemini occasionally puts "1. ...  2. ..." on one physical line.
      prepared.push(...rawLine.replace(/\s{2,}(?=\d+[.)]\s+)/g, '\n').split('\n'))
    } else if (!inFence && isSectionHeading(rawLine)) {
      prepared.push(`## ${rawLine.trim()}`)
    } else {
      prepared.push(rawLine)
    }
  }

  // Indented diagrams are otherwise parsed partly as list paragraphs and
  // partly as code. Fence only the obvious ASCII-art runs.
  const lines: string[] = []
  inFence = false
  let inDiagram = false
  for (const line of prepared) {
    if (isFence(line)) {
      if (inDiagram) {
        lines.push('```')
        inDiagram = false
      }
      lines.push(line)
      inFence = !inFence
      continue
    }
    const diagram = !inFence && isDiagramLine(line)
    if (diagram && !inDiagram) {
      lines.push('```text')
      inDiagram = true
    } else if (!diagram && inDiagram) {
      lines.push('```')
      inDiagram = false
    }
    lines.push(line)
  }
  if (inDiagram) lines.push('```')

  const out: string[] = []
  let inPipeTable = false
  let inTabTable = false
  const isSep = (line: string) => /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$/.test(line)
  const cells = (line: string, separator: string) => line.split(separator).map((c) => c.trim())
  const isPipeRow = (line: string) => line.includes('|') && cells(line, '|').filter((c) => c !== '').length >= 2
  const isTabRow = (line: string) => line.includes('\t') && cells(line, '\t').filter((c) => c !== '').length >= 2
  const asPipeRow = (line: string) => `| ${cells(line, '\t').map((c) => c.replaceAll('|', '\\|')).join(' | ')} |`
  const separateBlock = () => {
    if (out.length > 0 && out[out.length - 1].trim() !== '') out.push('')
  }

  inFence = false
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    if (isFence(line)) {
      out.push(line)
      inFence = !inFence
      inPipeTable = false
      inTabTable = false
      continue
    }
    if (inFence) {
      out.push(line)
      continue
    }

    const normalizedLine = line.replace(/[｜│┃]/g, '|')
    const nextLine = (lines[i + 1] ?? '').replace(/[｜│┃]/g, '|')
    const tab = isTabRow(normalizedLine)
    const nextIsTab = isTabRow(nextLine)
    if (inTabTable && !tab) {
      if (line.trim() !== '') separateBlock()
      inTabTable = false
    }
    if (tab && (inTabTable || nextIsTab)) {
      if (!inTabTable) {
        separateBlock()
        out.push(asPipeRow(normalizedLine))
        const n = Math.max(cells(normalizedLine, '\t').length, 2)
        out.push('| ' + Array(n).fill('---').join(' | ') + ' |')
      } else {
        out.push(asPipeRow(normalizedLine))
      }
      inTabTable = true
      continue
    }

    const pipe = isPipeRow(normalizedLine)
    const sep = isSep(normalizedLine)
    const nextIsPipe = isPipeRow(nextLine)
    if (inPipeTable && !pipe) {
      if (line.trim() !== '') separateBlock()
      inPipeTable = false
    }
    if (pipe && (inPipeTable || nextIsPipe)) {
      if (!inPipeTable) {
        separateBlock()
        if (!sep && nextIsPipe && !isSep(nextLine)) {
          const n = Math.max(cells(normalizedLine, '|').filter((c) => c !== '').length, 2)
          out.push(normalizedLine)
          out.push('| ' + Array(n).fill('---').join(' | ') + ' |')
          inPipeTable = true
          continue
        }
      }
      out.push(normalizedLine)
      inPipeTable = true
      continue
    }
    out.push(normalizedLine)
  }
  return out.join('\n')
}

function Pre(props: React.HTMLAttributes<HTMLPreElement>) {
  const { children } = props as { children: React.ReactNode }
  const ref = useRef<HTMLPreElement>(null)
  const [copied, setCopied] = useState(false)
  async function copy() {
    const text = ref.current?.querySelector('code')?.innerText ?? ''
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
        remarkPlugins={[remarkGfm, remarkMath]}
        rehypePlugins={[rehypeKatex]}
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
