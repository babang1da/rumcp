import { describe, expect, it } from 'vitest'

import { docClients, findDoc } from './docs'

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

  it('неизвестный клиент не находится', () => {
    expect(findDoc('nope')).toBeUndefined()
  })
})
