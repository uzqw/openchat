// Gemini Markdown rendering. react-markdown never emits raw HTML and
// strips javascript:/vbscript:/data: URLs via its default urlTransform,
// so the Gemini result can be rendered verbatim without an XSS surface.

import { useRef, useState } from 'react'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

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
  return (
    <div className="markdown text-[15px] leading-7">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          table: ({ children }) => (
            <div className="overflow-x-auto">
              <table>{children}</table>
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
        {content}
      </ReactMarkdown>
    </div>
  )
}
