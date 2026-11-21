import type { DocClient } from '../content/docs'
import type { Connector } from '../content/connectors'

/** Schema.org SoftwareApplication: что за сервис и сколько стоит. */
export function softwareApplicationJsonLd(connector: Connector, url: string) {
  return {
    '@context': 'https://schema.org',
    '@type': 'SoftwareApplication',
    name: `RuMCP — ${connector.h1}`,
    applicationCategory: 'BusinessApplication',
    operatingSystem: 'Any',
    url,
    description: connector.description,
    inLanguage: 'ru',
    offers: {
      '@type': 'Offer',
      price: '0',
      priceCurrency: 'RUB',
    },
  }
}

/** Schema.org FAQPage: вопросы и ответы со страницы коннектора. */
export function faqPageJsonLd(connector: Connector) {
  return {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: connector.faq.map((item) => ({
      '@type': 'Question',
      name: item.question,
      acceptedAnswer: {
        '@type': 'Answer',
        text: item.answer,
      },
    })),
  }
}

/** Разметка страницы коннектора: SoftwareApplication + FAQPage. */
export function connectorJsonLd(connector: Connector, url: string): unknown[] {
  return [softwareApplicationJsonLd(connector, url), faqPageJsonLd(connector)]
}

/** Schema.org HowTo: шаги подключения ассистента на страницах /docs/$client. */
export function howToJsonLd(doc: DocClient, url: string) {
  return {
    '@context': 'https://schema.org',
    '@type': 'HowTo',
    name: doc.title,
    description: doc.lead,
    url,
    inLanguage: 'ru',
    step: doc.steps.map((step, index) => ({
      '@type': 'HowToStep',
      position: index + 1,
      text: step,
    })),
  }
}
