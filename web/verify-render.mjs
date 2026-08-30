/* global console */

import React from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import fs from 'fs'

function normalizeMarkdown(content) {
  const lines = content.split('\n')
  const out = []
  let inTabTable = false
  const clean = (line) => line.replace(/[｜│┃]/g, '|')
  const split = (line, separator) => line.split(separator).map((c) => c.trim())
  const isSep = (line) => /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$/.test(line)
  const isPipeRow = (line) => line.includes('|') && split(line, '|').filter(Boolean).length >= 2
  const isTabRow = (line) => line.includes('\t') && split(line, '\t').filter(Boolean).length >= 2
  const asPipeRow = (line) => `| ${split(line, '\t').map((c) => c.replaceAll('|', '\\|')).join(' | ')} |`

  for (let i = 0; i < lines.length; i++) {
    const line = clean(lines[i])
    const next = clean(lines[i + 1] ?? '')
    if (isTabRow(line) && (isTabRow(next) || inTabTable)) {
      if (!inTabTable) {
        const prev = out[out.length - 1] ?? ''
        if (prev && !isPipeRow(prev)) out.push('')
        out.push(asPipeRow(line))
        out.push('| ' + Array(Math.max(split(line, '\t').length, 2)).fill('---').join(' | ') + ' |')
      } else {
        out.push(asPipeRow(line))
      }
      inTabTable = true
      continue
    }
    inTabTable = false
    const pipe = isPipeRow(line)
    const sep = isSep(line)
    const prev = out[out.length - 1] ?? ''
    if (pipe && !sep && prev && !isPipeRow(prev) && !isSep(prev)) out.push('')
    out.push(line)
    if (pipe && !sep && !isPipeRow(prev) && !isSep(prev) && isPipeRow(next) && !isSep(next)) {
      out.push('| ' + Array(Math.max(split(line, '|').filter(Boolean).length, 2)).fill('---').join(' | ') + ' |')
    }
  }
  return out.join('\n')
}

function OldMarkdown({content}) {
  return React.createElement('div',{className:'markdown'}, React.createElement(ReactMarkdown,{remarkPlugins:[remarkGfm], components:{table:({children})=>React.createElement('div',{className:'overflow-x-auto'},React.createElement('table',null,children))}}, content))
}
function NewMarkdown({content}) {
  const norm=normalizeMarkdown(content)
  return React.createElement('div',{className:'markdown'}, React.createElement(ReactMarkdown,{remarkPlugins:[remarkGfm], components:{table:({children})=>React.createElement('div',{className:'-mx-4 overflow-x-auto px-4'},React.createElement('table',{className:'min-w-[600px]'},children))}}, norm))
}

const caseA = `1. 2026年半年度核心财务指标对比\n| 财务指标 | 2026年 Q1 | 2026年 Q2 | 上半年合计(H1) |\n|---|---|---|---|\n| 总营收(Revenues) | $109.90B | $119.80B | $229.70B |\n| 营业利润 | $39.69B | $40.77B | $80.46B |\n| 净利润 | $62.58B | $112.11B | $174.69B |`
const caseB = `| 业务板块 | Q1 | Q2 | 同比 |\n| Google搜索 | $60.55B | $63.27B | +17% |\n| YouTube广告 | $9.88B | $11.06B | +13% |`
const caseC = `1. 2026年半年度核心财务指标对比\n\nDocument\t费用类别\tQ1 2026 金额\tQ2 2026 金额\n核心说明 / 驱动因素\t流量获取成本\t$15.23B\t$16.18B\n搜索量增长驱动\t分发渠道分成增加\t$28.52B\t$33.61B`

function section(title, subtitle, oldContent, newContent, raw) {
  const oldHtml = renderToStaticMarkup(React.createElement(OldMarkdown,{content:oldContent}))
  const newHtml = renderToStaticMarkup(React.createElement(NewMarkdown,{content:newContent}))
  const hasOldTable = oldHtml.includes('<table')
  const hasNewTable = newHtml.includes('<table')
  return `
  <h2 style="font-size:15px;font-weight:600;margin:24px 0 4px;color:#0f172a">${title}</h2>
  <div style="font-size:11px;color:#64748b;margin-bottom:6px">${subtitle} — 旧:hasTable=${hasOldTable} 新:hasTable=${hasNewTable}</div>
  <pre style="font-size:11px;background:#0f172a;color:#e2e8f0;padding:8px;border-radius:8px;white-space:pre-wrap;word-break:break-all;">${raw}</pre>
  <div style="display:flex;gap:12px;margin-top:10px">
    <div style="flex:1;min-width:0">
      <div style="font-size:12px;color:#dc2626;margin-bottom:4px;font-weight:600">旧渲染（无 normalize）</div>
      <div style="max-width:100%;background:white;border:1px solid #e2e8f0;border-radius:16px;padding:12px;box-shadow:0 1px 3px rgba(0,0,0,0.08);overflow:hidden">${oldHtml}</div>
    </div>
    <div style="flex:1;min-width:0">
      <div style="font-size:12px;color:#059669;margin-bottom:4px;font-weight:600">新渲染（有 normalize + 横向滚动）</div>
      <div style="max-width:100%;background:white;border:1px solid #e2e8f0;border-radius:16px;padding:12px;box-shadow:0 1px 3px rgba(0,0,0,0.08);overflow:hidden">${newHtml}</div>
    </div>
  </div>`
}

const css = fs.readFileSync('/workspace/wp/openchat/web/src/index.css','utf8').replace('@import "tailwindcss";','')
const html = `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><style>
body{font-family:system-ui;background:#f8fafc;padding:16px;max-width:860px;margin:0 auto}
${css}
</style>
<h1 style="font-size:18px;font-weight:700">Markdown 表格修复验证（本地 node 渲染，无 CDN）</h1>
<p style="font-size:13px;color:#475569">左：旧逻辑（只包 overflow-x-auto） 右：新逻辑（normalize + min-w-[600px] + max-content）</p>
${section('Case A: 列表后紧跟管道表格（无空行）','最常见的 Gemini 输出，旧逻辑会把管道符当文本挤在 <li> 里', caseA, caseA, caseA)}
${section('Case B: 缺少分隔行','Gemini 漏掉 |---| 行，GFM 要求有分隔行才算表格', caseB, caseB, caseB)}
${section('Case C: 浏览器 text extraction 的制表符表格（截图里的表现）','旧逻辑会把制表符表格当普通文本，新逻辑恢复为 GFM 表格', caseC, caseC, caseC)}
<p style="margin-top:24px;font-size:12px;color:#64748b">截图生成时间: ${new Date().toISOString()} | 本次验证使用 web/node_modules 本地 react-markdown + remark-gfm 渲染，非 CDN</p>
`
fs.writeFileSync('/tmp/verify-local.html', html)
console.log('written to /tmp/verify-local.html, length', html.length)
console.log('caseA old hasTable', renderToStaticMarkup(React.createElement(OldMarkdown,{content:caseA})).includes('<table'))
console.log('caseA new hasTable', renderToStaticMarkup(React.createElement(NewMarkdown,{content:caseA})).includes('<table'))
console.log('caseB old hasTable', renderToStaticMarkup(React.createElement(OldMarkdown,{content:caseB})).includes('<table'))
console.log('caseB new hasTable', renderToStaticMarkup(React.createElement(NewMarkdown,{content:caseB})).includes('<table'))
