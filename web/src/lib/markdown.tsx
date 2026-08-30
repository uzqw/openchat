// Gemini Markdown rendering. react-markdown never emits raw HTML and
// strips javascript:/vbscript:/data: URLs via its default urlTransform,
// so the Gemini result can be rendered verbatim without an XSS surface.

import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

export function Markdown({ content }: { content: string }) {
  return (
    <div className="markdown text-[15px] leading-7">
      <ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown>
    </div>
  )
}
