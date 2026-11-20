import { describe, expect, it } from 'vitest'

import { connectors, findConnector } from './connectors'

describe('connectors', () => {
  it('есть страницы для Метрики, Вебмастера, Вордстата и Директа', () => {
    expect(connectors.map((c) => c.slug)).toEqual([
      'yandex-metrika',
      'yandex-webmaster',
      'yandex-wordstat',
      'yandex-direct',
    ])
  })

  it('пути страниц совпадают со slug и лежат в /mcp/', () => {
    for (const connector of connectors) {
      expect(connector.path).toBe(`/mcp/${connector.slug}`)
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
      expect(connector.h1).toMatch(/^MCP для /)
      expect(connector.lead.length).toBeGreaterThan(60)
      expect(connector.tools.length).toBeGreaterThanOrEqual(4)
      expect(connector.steps.length).toBeGreaterThanOrEqual(3)
      expect(connector.faq.length).toBeGreaterThanOrEqual(4)
    }
  })

  it('«скоро» только у Вордстата и Директа, у них есть service для заявки', () => {
    const soon = connectors.filter((c) => !c.ready).map((c) => c.slug)
    expect(soon).toEqual(['yandex-wordstat', 'yandex-direct'])

    for (const connector of connectors) {
      expect(connector.service).toBeTruthy()
    }
  })

  it('неизвестный коннектор не находится', () => {
    expect(findConnector('yandex-search')).toBeUndefined()
    expect(findConnector('yandex-metrika')?.ready).toBe(true)
  })
})
