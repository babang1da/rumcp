import { useState } from 'react'
import { Link, createFileRoute, notFound } from '@tanstack/react-router'

import { CodeSnippet } from '../components/CodeSnippet'
import { CopyField } from '../components/CopyField'
import { Layout, LoginButton } from '../components/Layout'
import { docClients, examplePrompts, findDoc } from '../content/docs'
import { howToJsonLd } from '../seo/jsonld'
import { canonicalUrl, pageHead } from '../seo/pages'
import { getMcpUrl, getSiteUrl } from '../server/functions'

export const Route = createFileRoute('/docs/$client')({
  loader: async ({ params }) => {
    const doc = findDoc(params.client)
    if (!doc) throw notFound()

    const [siteUrl, mcpUrl] = await Promise.all([getSiteUrl(), getMcpUrl()])
    return { slug: doc.slug, siteUrl, mcpUrl }
  },
  head: ({ loaderData }) => {
    if (!loaderData) return {}
    const doc = findDoc(loaderData.slug)!
    const url = canonicalUrl(loaderData.siteUrl, `/docs/${doc.slug}`)

    return pageHead({
      title: `${doc.title} — RuMCP`,
      description: doc.lead,
      path: `/docs/${doc.slug}`,
      siteUrl: loaderData.siteUrl,
      jsonLd: [howToJsonLd(doc, url)],
    })
  },
  notFoundComponent: () => (
    <Layout>
      <h1>Такой инструкции нет</h1>
      <p>Доступны Claude, ChatGPT и Cursor.</p>
    </Layout>
  ),
  component: Doc,
})

function Doc() {
  const { slug, mcpUrl } = Route.useLoaderData()
  const doc = findDoc(slug)!

  return (
    <Layout>
      <nav className="tabs" aria-label="Ассистент">
        {docClients.map((c) => (
          <Link
            key={c.slug}
            to="/docs/$client"
            params={{ client: c.slug }}
            activeProps={{ 'aria-current': 'page' }}
          >
            {c.name}
          </Link>
        ))}
      </nav>
      <article className="doc">
        <h1>{doc.title}</h1>
        <p className="lead">{doc.lead}</p>
        <CopyField label="Адрес MCP" value={mcpUrl} />
        <ol className="steps">
          {doc.steps.map((step) => (
            <li key={step}>{step}</li>
          ))}
        </ol>
        {doc.config && (
          <div>
            {doc.configHint && <p className="hint">{doc.configHint}</p>}
            <CodeSnippet code={doc.config(mcpUrl)} file={doc.configFile!} />
          </div>
        )}
        {doc.note && <p className="note">{doc.note}</p>}
        <ExamplePrompts />
        <p>
          Нужен вход через Яндекс — без него ассистент не получит доступ к данным. <LoginButton>Войти через Яндекс</LoginButton>
        </p>
      </article>
    </Layout>
  )
}

/** Примеры вопросов ассистенту: каждый копируется в буфер одной кнопкой. */
function ExamplePrompts() {
  const [copiedIndex, setCopiedIndex] = useState<number | null>(null)

  async function copy(prompt: string, index: number) {
    try {
      await navigator.clipboard.writeText(prompt)
      setCopiedIndex(index)
      setTimeout(() => setCopiedIndex(null), 2000)
    } catch {
      // Буфер недоступен: вопрос можно выделить и скопировать вручную.
    }
  }

  return (
    <section className="block" aria-labelledby="prompts-title">
      <h2 id="prompts-title">Примеры вопросов для ассистента</h2>
      <ul className="prompts">
        {examplePrompts.map((prompt, index) => (
          <li key={prompt}>
            <span>{prompt}</span>
            <button type="button" className="button button-quiet" onClick={() => copy(prompt, index)}>
              {copiedIndex === index ? 'Скопировано' : 'Скопировать'}
            </button>
          </li>
        ))}
      </ul>
      <span className="sr-only" role="status">
        {copiedIndex !== null ? 'Вопрос скопирован' : ''}
      </span>
    </section>
  )
}
