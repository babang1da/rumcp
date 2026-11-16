/** Загрузка данных кабинета: web ходит только в `/api/*` сервиса на Go, не в БД. */

export type Me = {
  user: { id: string; login: string; email: string; name: string }
  account: {
    connected: boolean
    connected_at?: string
    expires_at?: string
    scope?: string
  }
  usage: { calls_today: number; limit: number }
}

export type CabinetState =
  | { status: 'anonymous' }
  | { status: 'ok'; me: Me }
  | { status: 'error' }

export const DEFAULT_API_URL = 'http://localhost:8080'

/**
 * Адрес MCP. Основной источник — `PUBLIC_URL` (адрес сервиса без `/mcp`);
 * запасной — заголовки прокси: берётся первое значение, хост проверяется.
 */
export function mcpUrlFor(
  request: Request,
  publicUrl: string | undefined = process.env.PUBLIC_URL,
): string {
  if (publicUrl) return `${publicUrl.replace(/\/+$/, '')}/mcp`

  const url = new URL(request.url)
  const first = (name: string) =>
    request.headers.get(name)?.split(',')[0].trim() || undefined
  const host = first('x-forwarded-host') ?? first('host')
  const validHost = host && /^[a-z0-9.-]+(:\d{1,5})?$/i.test(host) ? host : url.host
  const proto = first('x-forwarded-proto') === 'http' ? 'http' : first('x-forwarded-proto') === 'https' ? 'https' : url.protocol.replace(':', '')
  return `${proto}://${validHost}/mcp`
}

/** Дата одинаково на сервере и в браузере: зона задана явно. */
export function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString('ru-RU', {
    day: 'numeric',
    month: 'long',
    year: 'numeric',
    timeZone: 'UTC',
  })
}

export async function loadCabinet(
  request: Request,
  apiUrl: string = process.env.API_URL ?? DEFAULT_API_URL,
  fetchImpl: typeof fetch = fetch,
): Promise<CabinetState> {
  try {
    const response = await fetchImpl(`${apiUrl}/api/me`, {
      headers: { cookie: request.headers.get('cookie') ?? '' },
      signal: AbortSignal.timeout(3000),
    })
    if (response.status === 401) return { status: 'anonymous' }
    if (!response.ok) return { status: 'error' }
    return { status: 'ok', me: (await response.json()) as Me }
  } catch {
    return { status: 'error' }
  }
}

/** Доля дневного лимита, 0–100. */
export function usagePercent(used: number, limit: number): number {
  if (limit <= 0) return 0
  return Math.min(100, Math.round((used / limit) * 100))
}
