import { connectors } from '../content/connectors'
import { docClients } from '../content/docs'

/**
 * Публичные страницы сайта: единственный источник для sitemap и перелинковки.
 * Кабинет и служебные маршруты (`/healthz`, `/api/*`) сюда не входят.
 */
export const PUBLIC_PAGES: string[] = [
  '/',
  '/prompts',
  ...connectors.map((connector) => connector.path),
  ...docClients.map((doc) => `/docs/${doc.slug}`),
]

/** Абсолютный адрес страницы: canonical требует полный URL. */
export function canonicalUrl(siteUrl: string, path: string): string {
  const base = siteUrl.replace(/\/+$/, '')
  return `${base}${path}`
}

export type PageHeadInput = {
  title: string
  description: string
  /** Путь страницы, например `/mcp/yandex-metrika`. */
  path: string
  /** Публичный адрес сервиса без слэша на конце. */
  siteUrl: string
  jsonLd?: unknown[]
}

/** Мета-теги страницы: title, description, Open Graph, canonical и JSON-LD. */
export function pageHead(input: PageHeadInput) {
  const url = canonicalUrl(input.siteUrl, input.path)

  return {
    meta: [
      { title: input.title },
      { name: 'description', content: input.description },
      { property: 'og:type', content: 'website' },
      { property: 'og:site_name', content: 'RuMCP' },
      { property: 'og:locale', content: 'ru_RU' },
      { property: 'og:title', content: input.title },
      { property: 'og:description', content: input.description },
      { property: 'og:url', content: url },
    ],
    links: [{ rel: 'canonical', href: url }],
    scripts: (input.jsonLd ?? []).map((data) => ({
      type: 'application/ld+json',
      children: JSON.stringify(data),
    })),
  }
}
