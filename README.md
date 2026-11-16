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

## Architecture

One repository, two services, one database, one domain.

- `web/` — TanStack Start (React, SSR): landing, SEO pages «MCP для <сервис>», connection instructions, cabinet. Talks to the Go API only; it never opens a database connection.
- `server/` — Go: MCP bridge (Streamable HTTP, official MCP Go SDK), OAuth 2.1 for MCP clients, Yandex OAuth sign-in, encryption of upstream tokens, call log, daily call limit, cabinet API. One connector = one directory `server/internal/connectors/<name>/` with its tool list.
- **Postgres** — users, connected accounts (tokens encrypted at rest), call log. The Go service owns the schema and the migrations in `server/migrations`; `web/` has no access to it.
- **One domain, proxy-level routing:** `/mcp`, `/oauth/*`, `/.well-known/*`, `/api/*` → Go; everything else → `web`.
- **Prod:** Dokploy — two containers plus Postgres. Branches: `develop` → PR → `main`.

```
.
├── docker-compose.yml          # postgres + server + web
├── server/                     # Go service
│   ├── cmd/server/             # entry point
│   ├── internal/config/        # settings from env
│   ├── internal/connectors/    # connector contract, one directory per service
│   ├── internal/httpapi/       # HTTP routes
│   └── migrations/             # schema migrations, owned by the Go service
└── web/                        # TanStack Start app (SSR)
    └── src/routes/             # pages and server routes
```

Read-only tools by default; write tools are annotated and added per connector later.

## Local development

Everything runs in Docker Compose:

```bash
cp .env.example .env   # optional, defaults work as is
docker compose up -d
curl -f localhost:8080/healthz   # Go service
curl -f localhost:3000/healthz   # web
```

Services without Docker:

```bash
cd server && DATABASE_URL='postgres://rumcp:rumcp@localhost:5432/rumcp?sslmode=disable' go run ./cmd/server
cd web && npm install && npm run dev
```

Checks:

```bash
cd server && go vet ./... && go test ./...
cd web && npm ci && npm run build && npm test
```

Migrations use golang-migrate file names (`server/migrations/NNNN_name.up.sql` / `.down.sql`) and are applied with the `migrate` CLI against `DATABASE_URL`.

CI runs on every pull request to `develop` on a self-hosted runner: `go vet` and `go test` in `server/`, `npm run build` and `npm test` in `web/`. The runner's host, compose service, labels and recovery steps: [`docs/ops/self-hosted-runner.md`](docs/ops/self-hosted-runner.md).

## Monetisation

Free tier with a daily tool-call limit; paid plan in rubles once demand is measured. No billing in v1.
