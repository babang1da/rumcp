import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/')({ component: Home })

function Home() {
  return (
    <main>
      <h1>RuMCP</h1>
      <p>
        Удалённый MCP-сервер к Яндекс Метрике и Вебмастеру. Подключите аккаунт
        Яндекса — ассистент прочитает данные сам.
      </p>
      <p>Сервис готовится к запуску.</p>
    </main>
  )
}
