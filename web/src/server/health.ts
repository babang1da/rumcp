/** Ответ на `/healthz`: web-сервис жив, если отдаёт 200. */
export function healthResponse(): Response {
  return Response.json({ status: 'ok', service: 'web' })
}
