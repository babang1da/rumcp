import { describe, expect, it } from 'vitest'

import { connectSetup, docClients, findDoc } from './docs'

describe('docs', () => {
  it('есть инструкции для Claude, ChatGPT и Cursor', () => {
    expect(docClients.map((d) => d.slug)).toEqual(['claude', 'chatgpt', 'cursor'])
  })

  it('каждая инструкция упоминает MCP и адрес', () => {
    for (const doc of docClients) {
      expect(doc.title).toMatch(/RuMCP/)
      expect(doc.steps.join(' ')).toMatch(/MCP|mcp/i)
    }
  })

  it('конфиг Cursor содержит адрес', () => {
    expect(findDoc('cursor')?.config?.('https://x.test/mcp')).toContain(
      '"url": "https://x.test/mcp"',
    )
  })

  it('конфиг Claude Desktop содержит адрес и тип http', () => {
    const config = findDoc('claude')?.config?.('https://x.test/mcp')
    expect(config).toContain('"type": "http"')
    expect(config).toContain('"url": "https://x.test/mcp"')
  })

  it('каждый конфиг парсится, ведёт на адрес MCP и имеет имя файла', () => {
    for (const doc of docClients) {
      if (!doc.config) continue
      expect(doc.configFile, doc.slug).toBeTruthy()
      const parsed = JSON.parse(doc.config('https://x.test/mcp')) as {
        mcpServers: { rumcp: { url: string } }
      }
      expect(parsed.mcpServers.rumcp.url).toBe('https://x.test/mcp')
    }
  })

  it('у ChatGPT нет файла конфигурации — только адрес MCP', () => {
    expect(findDoc('chatgpt')?.config).toBeUndefined()
  })

  it('у блока подключения есть текст для каждого ассистента', () => {
    for (const doc of docClients) {
      expect(connectSetup[doc.slug].lead).toBeTruthy()
      expect(connectSetup[doc.slug].steps.length).toBeGreaterThan(0)
    }
  })

  it('неизвестный клиент не находится', () => {
    expect(findDoc('nope')).toBeUndefined()
  })
})
