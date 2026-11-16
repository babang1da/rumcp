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

/** Адрес MCP на том же домене, с которого пришёл запрос. */
export function mcpUrlFor(request: Request): string {
  const headers = request.headers
  const host = headers.get('x-forwarded-host') ?? headers.get('host')
  const url = new URL(request.url)
  const proto =
    headers.get('x-forwarded-proto')?.split(',')[0].trim() ??
    url.protocol.replace(':', '')
  return `${proto}://${host ?? url.host}/mcp`
}

export async function loadCabinet(
  request: Request,
  apiUrl: string = process.env.API_URL ?? DEFAULT_API_URL,
  fetchImpl: typeof fetch = fetch,
): Promise<CabinetState> {
  try {
    const response = await fetchImpl(`${apiUrl}/api/me`, {
      headers: { cookie: request.headers.get('cookie') ?? '' },
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
