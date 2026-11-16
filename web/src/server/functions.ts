import { createServerFn } from '@tanstack/react-start'
import { getRequest } from '@tanstack/react-start/server'

import { loadCabinet, mcpUrlFor } from './cabinet'

export const getCabinet = createServerFn({ method: 'GET' }).handler(async () => {
  const request = getRequest()
  return { state: await loadCabinet(request), mcpUrl: mcpUrlFor(request) }
})

export const getMcpUrl = createServerFn({ method: 'GET' }).handler(async () =>
  mcpUrlFor(getRequest()),
)
