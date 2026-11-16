import { createFileRoute } from '@tanstack/react-router'

import { sitemapXml } from '../seo/sitemap'
import { baseUrlFor } from '../server/url'

export const Route = createFileRoute('/sitemap.xml')({
  server: {
    handlers: {
      GET: ({ request }) =>
        new Response(sitemapXml(baseUrlFor(request)), {
          headers: { 'Content-Type': 'application/xml; charset=utf-8' },
        }),
    },
  },
})
