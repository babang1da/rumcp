# RuMCP

Hosted MCP connectors to Russian services — a Composio-style web service for the RU stack.
One remote MCP URL, OAuth sign-in, no tokens to generate, nothing to self-host.

## Product

- **User:** marketers, SEO specialists, agencies and founders who work in Claude / ChatGPT / Cursor and need their Yandex data inside the assistant.
- **Promise:** connect a Yandex account in one minute; the assistant reads Metrika, Webmaster, Wordstat and Direct.
- **Wedge:** the whole Yandex marketing stack behind one connector. Existing hosted offers cover ads only (Direct); the rest is self-hosted open source.

## Connector order

| # | Connector | Why this order |
|---|-----------|----------------|
| 1 | Yandex Metrika | OAuth scope only, no API approval |
| 2 | Yandex Webmaster | OAuth scope only, no API approval |
| 3 | Yandex Wordstat | API access is granted on request |
| 4 | Yandex Direct | API access needs an approved application; read-only first, writes behind confirmation |

Later, by demand: VK Ads, Avito, Wildberries, Ozon, amoCRM, Bitrix24.

## Architecture (v1)

- One Next.js app (TypeScript): landing, cabinet, `/api/mcp` (Streamable HTTP, MCP TypeScript SDK).
- MCP auth: OAuth 2.1 for the MCP client; Yandex OAuth for the upstream account.
- Postgres: users, connected accounts (upstream tokens encrypted at rest), tool-call log (usage and limits).
- Read-only tools by default; write tools are annotated and added per connector later.
- Connectors live in `src/connectors/<name>/`, each exporting its tool list — adding a service touches one directory.
- Prod: Dokploy. Branches: `develop` → PR → `main`.

## Monetisation

Free tier with a daily tool-call limit; paid plan in rubles once demand is measured. No billing in v1.
