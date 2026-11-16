/**
 * Публичный адрес сервиса без слэша на конце. Основной источник — `PUBLIC_URL`;
 * запасной — заголовки прокси: берётся первое значение, хост проверяется.
 */
export function baseUrlFor(
  request: Request,
  publicUrl: string | undefined = process.env.PUBLIC_URL,
): string {
  if (publicUrl) return publicUrl.replace(/\/+$/, '')

  const url = new URL(request.url)
  const first = (name: string) =>
    request.headers.get(name)?.split(',')[0].trim() || undefined
  const host = first('x-forwarded-host') ?? first('host')
  const validHost = host && /^[a-z0-9.-]+(:\d{1,5})?$/i.test(host) ? host : url.host
  const proto =
    first('x-forwarded-proto') === 'http'
      ? 'http'
      : first('x-forwarded-proto') === 'https'
        ? 'https'
        : url.protocol.replace(':', '')

  return `${proto}://${validHost}`
}
