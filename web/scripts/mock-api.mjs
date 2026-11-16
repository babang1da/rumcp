// Мок `/api/me` для проверки кабинета без БД и входа через Яндекс.
// Запуск: node scripts/mock-api.mjs  (порт 8080, или MOCK_PORT)
// Cookie `session=1` — вошедший пользователь, без неё — 401.
import http from 'node:http'

const me = {
  user: { id: '1', login: 'ivan', email: 'ivan@ya.ru', name: 'Иван' },
  account: { connected: true, connected_at: '2026-11-10T10:00:00Z' },
  usage: { calls_today: 12, limit: 100 },
}

http
  .createServer((req, res) => {
    const signedIn = (req.headers.cookie ?? '').includes('session=1')
    res.setHeader('content-type', 'application/json')
    if (!signedIn) return res.writeHead(401).end('{"error":"unauthorized"}')
    if (req.url === '/api/me') return res.end(JSON.stringify(me))
    if (req.url === '/api/disconnect') return res.end('{"connected":false}')
    res.writeHead(404).end('{}')
  })
  .listen(Number(process.env.MOCK_PORT ?? 8080))
