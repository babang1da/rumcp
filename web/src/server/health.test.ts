import { describe, expect, it } from 'vitest'

import { healthResponse } from './health'

describe('healthResponse', () => {
  it('отвечает 200 и статусом ok', async () => {
    const response = healthResponse()

    expect(response.status).toBe(200)
    await expect(response.json()).resolves.toEqual({
      status: 'ok',
      service: 'web',
    })
  })
})
