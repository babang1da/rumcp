import { Link, createFileRoute, notFound } from '@tanstack/react-router'

import { CodeSnippet } from '../components/CodeSnippet'
import { CopyField } from '../components/CopyField'
import { Layout, LoginButton } from '../components/Layout'
import { docClients, findDoc } from '../content/docs'
import { getMcpUrl } from '../server/functions'

export const Route = createFileRoute('/docs/$client')({
  loader: async ({ params }) => {
    const doc = findDoc(params.client)
    if (!doc) throw notFound()
    return { slug: doc.slug, mcpUrl: await getMcpUrl() }
  },
  head: ({ params }) => {
    const doc = findDoc(params.client)
    return { meta: doc ? [{ title: `${doc.title} — RuMCP` }, { name: 'description', content: doc.lead }] : [] }
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
        <p>
          Нужен вход через Яндекс — без него ассистент не получит доступ к данным. <LoginButton>Войти через Яндекс</LoginButton>
        </p>
      </article>
    </Layout>
  )
}
