import { describe, expect, it } from 'vitest'

import { connectors } from '../content/connectors'
import { docClients } from '../content/docs'
import { connectorJsonLd, faqPageJsonLd, howToJsonLd, softwareApplicationJsonLd } from './jsonld'
import { PUBLIC_PAGES, canonicalUrl, pageHead } from './pages'
import { robotsTxt, sitemapXml } from './sitemap'

const site = 'https://rumcp.example'

describe('PUBLIC_PAGES', () => {
  it('перечисляет главную, четыре коннектора и инструкции', () => {
    expect(PUBLIC_PAGES).toContain('/')
    for (const connector of connectors) {
      expect(PUBLIC_PAGES).toContain(connector.path)
    }
    expect(PUBLIC_PAGES).toContain('/docs/claude')
    expect(PUBLIC_PAGES).toContain('/docs/chatgpt')
    expect(PUBLIC_PAGES).toContain('/docs/cursor')
  })

  it('не отдаёт в поиск кабинет и служебные маршруты', () => {
    expect(PUBLIC_PAGES).not.toContain('/cabinet')
    expect(PUBLIC_PAGES).not.toContain('/healthz')
  })
})

describe('canonicalUrl', () => {
  it('склеивает адрес без двойных слэшей', () => {
    expect(canonicalUrl('https://rumcp.example/', '/connectors/yandex-metrika')).toBe(
      'https://rumcp.example/connectors/yandex-metrika',
    )
  })
})

describe('pageHead', () => {
  const head = pageHead({
    title: 'MCP для Яндекс Метрики — RuMCP',
    description: 'Описание страницы.',
    path: '/connectors/yandex-metrika',
    siteUrl: site,
  })

  it('отдаёт свой title и description', () => {
    expect(head.meta).toContainEqual({ title: 'MCP для Яндекс Метрики — RuMCP' })
    expect(head.meta).toContainEqual({ name: 'description', content: 'Описание страницы.' })
  })

  it('ставит абсолютный canonical', () => {
    expect(head.links).toEqual([
      { rel: 'canonical', href: 'https://rumcp.example/connectors/yandex-metrika' },
    ])
  })

  it('заполняет Open Graph', () => {
    for (const property of ['og:title', 'og:description', 'og:url', 'og:type']) {
      expect(head.meta.some((tag) => 'property' in tag && tag.property === property)).toBe(true)
    }
    expect(head.meta).toContainEqual({
      property: 'og:url',
      content: 'https://rumcp.example/connectors/yandex-metrika',
    })
  })

  it('без JSON-LD скриптов нет', () => {
    expect(head.scripts).toEqual([])
  })
})

describe('jsonld', () => {
  const connector = connectors[0]
  const url = canonicalUrl(site, connector.path)

  it('SoftwareApplication — валидный JSON с типом и адресом', () => {
    const jsonLd = softwareApplicationJsonLd(connector, url)
    expect(JSON.parse(JSON.stringify(jsonLd))).toMatchObject({
      '@context': 'https://schema.org',
      '@type': 'SoftwareApplication',
      url,
      applicationCategory: 'BusinessApplication',
      offers: { price: '0', priceCurrency: 'RUB' },
    })
  })

  it('FAQPage перечисляет все вопросы страницы', () => {
    const jsonLd = faqPageJsonLd(connector)
    expect(jsonLd.mainEntity).toHaveLength(connector.faq.length)
    expect(jsonLd.mainEntity[0]).toEqual({
      '@type': 'Question',
      name: connector.faq[0].question,
      acceptedAnswer: { '@type': 'Answer', text: connector.faq[0].answer },
    })
  })

  it('разметка страницы — оба блока и без undefined в JSON', () => {
    const blocks = connectorJsonLd(connector, url)
    expect(blocks).toHaveLength(2)

    const serialized = blocks.map((block) => JSON.stringify(block))
    for (const json of serialized) {
      expect(json).not.toContain('undefined')
      expect(JSON.parse(json)).toBeTruthy()
    }
  })
})

describe('howToJsonLd', () => {
  const doc = docClients[0]
  const url = canonicalUrl(site, `/docs/${doc.slug}`)

  it('HowTo — валидный JSON с типом, адресом и языком', () => {
    const jsonLd = howToJsonLd(doc, url)
    expect(JSON.parse(JSON.stringify(jsonLd))).toMatchObject({
      '@context': 'https://schema.org',
      '@type': 'HowTo',
      url,
      inLanguage: 'ru',
      name: doc.title,
      description: doc.lead,
    })
  })

  it('перечисляет все шаги инструкции по порядку', () => {
    const jsonLd = howToJsonLd(doc, url)
    expect(jsonLd.step).toHaveLength(doc.steps.length)
    expect(jsonLd.step[0]).toEqual({
      '@type': 'HowToStep',
      position: 1,
      text: doc.steps[0],
    })
    expect(jsonLd.step.map((step) => step.position)).toEqual(
      doc.steps.map((_, index) => index + 1),
    )
  })

  it('для каждой инструкции — валидный JSON без undefined', () => {
    for (const d of docClients) {
      const json = JSON.stringify(howToJsonLd(d, canonicalUrl(site, `/docs/${d.slug}`)))
      expect(json).not.toContain('undefined')
      expect(JSON.parse(json)).toBeTruthy()
    }
  })
})

describe('sitemapXml', () => {
  const xml = sitemapXml(site)

  it('начинается с xml-заголовка и urlset', () => {
    expect(xml.startsWith('<?xml version="1.0" encoding="UTF-8"?>')).toBe(true)
    expect(xml).toContain('<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">')
    expect(xml.trimEnd().endsWith('</urlset>')).toBe(true)
  })

  it('перечисляет все публичные страницы абсолютными адресами', () => {
    expect(xml.match(/<loc>/g)).toHaveLength(PUBLIC_PAGES.length)
    expect(xml).toContain('<loc>https://rumcp.example/connectors/yandex-metrika</loc>')
    expect(xml).toContain('<loc>https://rumcp.example/connectors/yandex-webmaster</loc>')
    expect(xml).toContain('<loc>https://rumcp.example/connectors/metrika-for-claude</loc>')
    expect(xml).toContain('<loc>https://rumcp.example/connectors/webmaster-for-agents</loc>')
    expect(xml).toContain('<loc>https://rumcp.example/</loc>')
    expect(xml).not.toContain('/cabinet')
  })

  it('экранирует спецсимволы в адресе', () => {
    expect(sitemapXml('https://rumcp.example/?a&b', ['/x'])).toContain('?a&amp;b')
  })
})

describe('robotsTxt', () => {
  const robots = robotsTxt(site)

  it('ссылается на sitemap', () => {
    expect(robots).toContain('Sitemap: https://rumcp.example/sitemap.xml')
  })

  it('закрывает кабинет и API, разрешает остальное', () => {
    expect(robots).toContain('Allow: /')
    expect(robots).toContain('Disallow: /cabinet')
    expect(robots).toContain('Disallow: /api/')
  })
})
