import { describe, expect, it } from 'vitest'

import { LEAD_EMAIL_ERROR, LEAD_ERROR, submitLead } from './leads'

describe('submitLead', () => {
  it('отправляет заявку в /api/leads с почтой и сервисом', async () => {
    let seen: { url: string; init: RequestInit } | undefined
    const fetchMock = (async (url: string, init: RequestInit) => {
      seen = { url, init }
      return new Response('{"ok":true}', { status: 201 })
    }) as unknown as typeof fetch

    await expect(submitLead(' buyer@example.ru ', 'direct', fetchMock)).resolves.toEqual({
      ok: true,
    })
    expect(seen?.url).toBe('/api/leads')
    expect(seen?.init.method).toBe('POST')
    expect(JSON.parse(String(seen?.init.body))).toEqual({
      email: 'buyer@example.ru',
      service: 'direct',
    })
  })

  it('400 от API — просьба проверить почту', async () => {
    const badRequest = (async () =>
      new Response('{"error":"invalid_email"}', { status: 400 })) as unknown as typeof fetch

    await expect(submitLead('bad@', 'wordstat', badRequest)).resolves.toEqual({
      ok: false,
      error: LEAD_EMAIL_ERROR,
    })
  })

  it('ошибка сервиса и сети — общее сообщение', async () => {
    const broken = (async () => new Response('', { status: 500 })) as unknown as typeof fetch
    await expect(submitLead('buyer@example.ru', 'direct', broken)).resolves.toEqual({
      ok: false,
      error: LEAD_ERROR,
    })

    const down = (async () => {
      throw new Error('down')
    }) as typeof fetch
    await expect(submitLead('buyer@example.ru', 'direct', down)).resolves.toEqual({
      ok: false,
      error: LEAD_ERROR,
    })
  })

  it('без почты запрос не уходит', async () => {
    let called = false
    const fetchMock = (async () => {
      called = true
      return new Response('', { status: 201 })
    }) as unknown as typeof fetch

    await expect(submitLead('   ', 'direct', fetchMock)).resolves.toEqual({
      ok: false,
      error: LEAD_EMAIL_ERROR,
    })
    expect(called).toBe(false)
  })
})
