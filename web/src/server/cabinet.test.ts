import { describe, expect, it } from 'vitest'

import { loadCabinet, mcpUrlFor, usagePercent } from './cabinet'

const request = new Request('http://localhost:3000/cabinet', {
  headers: { cookie: 'session=abc' },
})

const me = {
  user: { id: '1', login: 'ivan', email: 'i@ya.ru', name: 'Иван' },
  account: { connected: true },
  usage: { calls_today: 12, limit: 100 },
}

describe('loadCabinet', () => {
  it('без сессии (401) возвращает anonymous', async () => {
    const fetchMock = (async () => new Response('{}', { status: 401 })) as typeof fetch
    await expect(loadCabinet(request, 'http://api', fetchMock)).resolves.toEqual({
      status: 'anonymous',
    })
  })

  it('с сессией отдаёт данные /api/me и пересылает cookie', async () => {
    let seen: Headers | undefined
    const fetchMock = (async (url: string, init: RequestInit) => {
      seen = new Headers(init.headers)
      expect(url).toBe('http://api/api/me')
      return Response.json(me)
    }) as unknown as typeof fetch
    await expect(loadCabinet(request, 'http://api', fetchMock)).resolves.toEqual({
      status: 'ok',
      me,
    })
    expect(seen?.get('cookie')).toBe('session=abc')
  })

  it('при сбое API возвращает error', async () => {
    const down = (async () => {
      throw new Error('down')
    }) as typeof fetch
    await expect(loadCabinet(request, 'http://api', down)).resolves.toEqual({
      status: 'error',
    })
    const broken = (async () => new Response('', { status: 500 })) as typeof fetch
    await expect(loadCabinet(request, 'http://api', broken)).resolves.toEqual({
      status: 'error',
    })
  })
})

describe('mcpUrlFor', () => {
  it('берёт домен и схему из заголовков прокси', () => {
    const proxied = new Request('http://web:3000/', {
      headers: { 'x-forwarded-host': 'rumcp.example', 'x-forwarded-proto': 'https' },
    })
    expect(mcpUrlFor(proxied)).toBe('https://rumcp.example/mcp')
  })
})

describe('usagePercent', () => {
  it('ограничивает 0–100 и не делит на ноль', () => {
    expect(usagePercent(12, 100)).toBe(12)
    expect(usagePercent(150, 100)).toBe(100)
    expect(usagePercent(5, 0)).toBe(0)
  })
})
