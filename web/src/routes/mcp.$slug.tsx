import { createFileRoute, notFound } from '@tanstack/react-router'

import { ConnectorPage } from '../components/ConnectorPage'
import { Layout } from '../components/Layout'
import { findConnector } from '../content/connectors'
import { connectorJsonLd } from '../seo/jsonld'
import { canonicalUrl, pageHead } from '../seo/pages'
import { getMcpUrl, getSiteUrl } from '../server/functions'

export const Route = createFileRoute('/mcp/$slug')({
  loader: async ({ params }) => {
    const connector = findConnector(params.slug)
    if (!connector) throw notFound()

    const [siteUrl, mcpUrl] = await Promise.all([getSiteUrl(), getMcpUrl()])
    return { slug: connector.slug, siteUrl, mcpUrl }
  },
  head: ({ loaderData }) => {
    if (!loaderData) return {}
    const connector = findConnector(loaderData.slug)!
    const url = canonicalUrl(loaderData.siteUrl, connector.path)

    return pageHead({
      title: connector.title,
      description: connector.description,
      path: connector.path,
      siteUrl: loaderData.siteUrl,
      jsonLd: connectorJsonLd(connector, url),
    })
  },
  notFoundComponent: () => (
    <Layout>
      <h1>Такого коннектора нет</h1>
      <p>Доступны Метрика, Вебмастер, Вордстат и Директ.</p>
    </Layout>
  ),
  component: Connector,
})

function Connector() {
  const { slug, mcpUrl } = Route.useLoaderData()
  const connector = findConnector(slug)!

  return <ConnectorPage connector={connector} mcpUrl={mcpUrl} />
}
