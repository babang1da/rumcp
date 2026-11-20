import { PUBLIC_PAGES, canonicalUrl } from './pages'

function escapeXml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

/** sitemap.xml со всеми публичными страницами. */
export function sitemapXml(siteUrl: string, pages: string[] = PUBLIC_PAGES): string {
  const urls = pages
    .map((path) => `  <url>\n    <loc>${escapeXml(canonicalUrl(siteUrl, path))}</loc>\n  </url>`)
    .join('\n')

  return `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${urls}
</urlset>
`
}

/** robots.txt: закрывает кабинет и служебные маршруты, указывает на sitemap. */
export function robotsTxt(siteUrl: string): string {
  return `User-agent: *
Allow: /
Disallow: /cabinet
Disallow: /healthz
Disallow: /api/

Sitemap: ${canonicalUrl(siteUrl, '/sitemap.xml')}
`
}
