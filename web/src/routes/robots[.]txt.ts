import { createFileRoute } from '@tanstack/react-router'

import { robotsTxt } from '../seo/sitemap'
import { baseUrlFor } from '../server/url'

export const Route = createFileRoute('/robots.txt')({
  server: {
    handlers: {
      GET: ({ request }) =>
        new Response(robotsTxt(baseUrlFor(request)), {
          headers: { 'Content-Type': 'text/plain; charset=utf-8' },
        }),
    },
  },
})
