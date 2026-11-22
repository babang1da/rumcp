# RuMCP

Hosted MCP connectors to Russian services — a Composio-style web service for the RU stack.
One remote MCP URL, OAuth sign-in, no tokens to generate, nothing to self-host.

## Product

- **User:** marketers, SEO specialists, agencies and founders who work in Claude / ChatGPT / Cursor and need their Yandex data inside the assistant.
- **Promise:** connect a Yandex account in one minute; the assistant reads Metrika, Webmaster, Wordstat and Direct.
- **Wedge:** the whole Yandex marketing stack behind one connector. Existing hosted offers cover ads only (Direct); the rest is self-hosted open source.

## Подключить за 2 минуты

1. **Адрес MCP:** `https://rumcp.212-108-82-65.sslip.io/mcp`
2. **Вход через Яндекс:** при подключении откроется окно Яндекса — разрешите доступ к Метрике и Вебмастеру. Токены создавать не нужно.
3. **Добавьте адрес в ассистента** — пошаговые инструкции: [Claude](https://rumcp.212-108-82-65.sslip.io/docs/claude), [ChatGPT](https://rumcp.212-108-82-65.sslip.io/docs/chatgpt), [Cursor](https://rumcp.212-108-82-65.sslip.io/docs/cursor).

## Примеры промптов

Готовые промпты для Метрики и Вебмастера (все сценарии: [https://rumcp.212-108-82-65.sslip.io/prompts](https://rumcp.212-108-82-65.sslip.io/prompts)).

### 1. Анализ падения органики

_Визиты из поиска упали, и нужно понять, откуда: сезонность, технические проблемы сайта или потеря позиций по запросам._

> Посмотри визиты из органического поиска за последние 8 недель по неделям и скажи, с какой недели началось падение и насколько оно глубокое. Сравни источники трафика до и после падения: изменилась ли доля органики относительно рекламы и прямых заходов. Если падение началось резко с конкретной даты, проверь в Вебмастере диагностику и страницы, исключённые из индекса, и перечисли возможные технические причины. В конце дай короткий список из 3–5 шагов, что проверить в первую очередь.

### 2. Поиск неэффективных страниц

_Найти страницы с высоким трафиком, но плохими поведенческими факторами и низкой конверсией, чтобы исправить точки потери лидов._

> Выгрузи из Яндекс Метрики топ-20 страниц входа за последние 30 дней по числу визитов. Для каждой страницы покажи показатель отказов, среднее время и процент конверсии по ключевым целям. Выдели страницы, где отказы выше 60% или конверсия в 2 раза ниже средней по сайту при трафике от 200 визитов. Для худших страниц проверь в Вебмастере основные поисковые запросы и сформируй 3 рекомендации по оптимизации контента.

### 3. Когорты по источникам

_Оценить качество каналов привлечения: какие источники приводят вовлечённых пользователей с высокой конверсией и повторными визитами, а какие дают пустой трафик._

> Построй когортный отчёт по источникам визитов в Яндекс Метрике за последние 12 недель с группировкой по неделе первого визита. Сравни каналы (поисковые системы, реклама, прямые заходы, переходы по ссылкам) по глубине просмотра, проценту повторных визитов на 2-й и 4-й неделе и конверсии в заявку. Укажи, какие каналы дают максимальную долгосрочную отдачу, а где привлекается невовлечённый трафик.

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
```

Compose publishes no host ports (traffic goes through the proxy over the Docker network). Reach the DB via `exec`:

```bash
docker compose exec postgres psql -U rumcp -d rumcp                  # database
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
