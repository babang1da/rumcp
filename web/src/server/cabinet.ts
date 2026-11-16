/** Загрузка данных кабинета: web ходит только в `/api/*` сервиса на Go, не в БД. */

import { baseUrlFor } from './url'

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

/** Адрес MCP: публичный адрес сервиса плюс `/mcp`. */
export function mcpUrlFor(
  request: Request,
  publicUrl: string | undefined = process.env.PUBLIC_URL,
): string {
  return `${baseUrlFor(request, publicUrl)}/mcp`
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
