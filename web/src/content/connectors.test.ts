import { describe, expect, it } from 'vitest'

import { connectors, findConnector } from './connectors'

describe('connectors', () => {
  it('есть страницы для Метрики, Вебмастера, Вордстата и Директа', () => {
    expect(connectors.map((c) => c.slug)).toEqual([
      'yandex-metrika',
      'yandex-webmaster',
      'yandex-wordstat',
      'yandex-direct',
      'metrika-for-claude',
      'webmaster-for-agents',
      'wordstat-for-claude',
      'direct-for-claude',
    ])
  })

  it('пути страниц совпадают со slug и лежат в /connectors/', () => {
    for (const connector of connectors) {
      expect(connector.path).toBe(`/connectors/${connector.slug}`)
    }
  })

  it('title и description уникальны — страницы не дублируют друг друга', () => {
    const titles = connectors.map((c) => c.title)
    const descriptions = connectors.map((c) => c.description)

    expect(new Set(titles).size).toBe(connectors.length)
    expect(new Set(descriptions).size).toBe(connectors.length)
  })

  it('у каждой страницы есть h1, описание, инструменты, шаги и FAQ', () => {
    for (const connector of connectors) {
      expect(connector.h1).toMatch(/^MCP /)
      expect(connector.lead.length).toBeGreaterThan(60)
      expect(connector.tools.length).toBeGreaterThanOrEqual(4)
      expect(connector.steps.length).toBeGreaterThanOrEqual(3)
      expect(connector.faq.length).toBeGreaterThanOrEqual(4)
    }
  })

  it('содержит целевые SEO-запросы для Метрики и Вебмастера', () => {
    const metrika = findConnector('yandex-metrika')!
    expect(metrika.title).toContain('MCP коннектор Яндекс Метрика')
    expect(metrika.description).toContain('MCP коннектор Яндекс Метрика')
    expect(metrika.h1).toBe('MCP коннектор Яндекс Метрика')

    const webmaster = findConnector('yandex-webmaster')!
    expect(webmaster.title).toContain('MCP Яндекс Вебмастер')
    expect(webmaster.description).toContain('MCP Яндекс Вебмастер')
    expect(webmaster.h1).toBe('MCP Яндекс Вебмастер')
  })

  it('новые страницы ведут на реальные инструменты Метрики и Вебмастера под целевые запросы', () => {
    const forClaude = findConnector('metrika-for-claude')!
    expect(forClaude.ready).toBe(true)
    expect(forClaude.h1).toMatch(/^MCP /)
    expect(forClaude.title).toContain('Claude Desktop')
    expect(forClaude.description).toContain('Метрику к Claude Desktop')
    expect(forClaude.tools.map((t) => t.name)).toEqual(
      findConnector('yandex-metrika')!.tools.map((t) => t.name),
    )

    const forAgents = findConnector('webmaster-for-agents')!
    expect(forAgents.ready).toBe(true)
    expect(forAgents.h1).toMatch(/^MCP /)
    expect(forAgents.title).toContain('агентов')
    expect(forAgents.description).toContain('для ИИ-агентов')
    expect(forAgents.tools.map((t) => t.name)).toEqual(
      findConnector('yandex-webmaster')!.tools.map((t) => t.name),
    )

    for (const connector of [forClaude, forAgents]) {
      expect(connector.tools.some((t) => /Директ|Вордстат/.test(t.name))).toBe(false)
    }
  })

  it('страницы «для Claude» про Вордстат и Директ честно «скоро», как и их коннекторы', () => {
    for (const [slug, base, word] of [
      ['wordstat-for-claude', 'yandex-wordstat', 'Вордстат'],
      ['direct-for-claude', 'yandex-direct', 'Директ'],
    ] as const) {
      const page = findConnector(slug)!
      expect(page.ready).toBe(findConnector(base)!.ready)
      expect(page.service).toBe(findConnector(base)!.service)
      expect(page.h1).toMatch(/^MCP /)
      expect(page.title).toContain('Claude')
      expect(page.title).toContain(word)
      expect(page.title.length).toBeLessThanOrEqual(60)
      expect(page.description.length).toBeLessThanOrEqual(160)
    }
  })

  it('«скоро» только у Вордстата и Директа, у них есть service для заявки', () => {
    const soon = connectors.filter((c) => !c.ready).map((c) => c.slug)
    expect(soon).toEqual([
      'yandex-wordstat',
      'yandex-direct',
      'wordstat-for-claude',
      'direct-for-claude',
    ])

    for (const connector of connectors) {
      expect(connector.service).toBeTruthy()
    }
  })

  it('неизвестный коннектор не находится', () => {
    expect(findConnector('yandex-search')).toBeUndefined()
    expect(findConnector('yandex-metrika')?.ready).toBe(true)
  })
})
