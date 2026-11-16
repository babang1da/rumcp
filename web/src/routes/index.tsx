import { createFileRoute } from '@tanstack/react-router'

import { CopyField } from '../components/CopyField'
import { Layout, LoginButton } from '../components/Layout'
import { examplePrompts } from '../content/docs'
import { getMcpUrl } from '../server/functions'

export const Route = createFileRoute('/')({
  loader: () => getMcpUrl(),
  component: Home,
})

const connectors = [
  { name: 'Яндекс Метрика', ready: true, text: 'Счётчики, отчёты, цели, источники трафика' },
  { name: 'Яндекс Вебмастер', ready: true, text: 'Сайты, индексация, поисковые запросы, диагностика' },
  { name: 'Яндекс Вордстат', ready: false, text: 'Ждём доступ к API' },
  { name: 'Яндекс Директ', ready: false, text: 'Ждём доступ к API, сначала только чтение' },
]

function Home() {
  const mcpUrl = Route.useLoaderData()

  return (
    <Layout>
      <section className="hero">
        <h1>Данные Метрики и Вебмастера — прямо в Claude, ChatGPT и Cursor</h1>
        <p className="lead">
          Один адрес MCP, вход через Яндекс. Без токенов, без установки и без своего сервера.
          Ассистент читает данные вашего аккаунта и отвечает на вопросы про сайт.
        </p>
        <div className="actions">
          <LoginButton />
          <a className="link" href="#how">Как это работает</a>
        </div>
        <CopyField label="Адрес MCP" value={mcpUrl} />
      </section>

      <section className="block" aria-labelledby="ask">
        <h2 id="ask">О чём можно спросить</h2>
        <ul className="prompts">
          {examplePrompts.map((prompt) => (
            <li key={prompt}>{prompt}</li>
          ))}
        </ul>
      </section>

      <section className="block" id="how" aria-labelledby="how-title">
        <h2 id="how-title">Подключение за минуту</h2>
        <ol className="steps">
          <li>
            <strong>Войдите через Яндекс.</strong> Разрешите чтение Метрики и Вебмастера.
          </li>
          <li>
            <strong>Добавьте адрес MCP в ассистента.</strong> Инструкции:{' '}
            <a href="/docs/claude">Claude</a>, <a href="/docs/chatgpt">ChatGPT</a>,{' '}
            <a href="/docs/cursor">Cursor</a>.
          </li>
          <li>
            <strong>Задайте вопрос про свой сайт.</strong> Ассистент сам вызовет нужные инструменты.
          </li>
        </ol>
      </section>

      <section className="block" aria-labelledby="what">
        <h2 id="what">Что подключено</h2>
        <ul className="connectors">
          {connectors.map((c) => (
            <li key={c.name} data-ready={c.ready}>
              <span className="connector-name">{c.name}</span>
              <span className="connector-text">{c.text}</span>
              <span className="connector-state">{c.ready ? 'Работает' : 'Скоро'}</span>
            </li>
          ))}
        </ul>
      </section>

      <section className="block" aria-labelledby="terms">
        <h2 id="terms">Бесплатно, только чтение</h2>
        <p>
          Сервис бесплатный, в сутки действует лимит вызовов — остаток виден в кабинете. Ассистент
          ничего не меняет в ваших счётчиках и сайтах. Отключить аккаунт можно в кабинете в любой
          момент.
        </p>
      </section>
    </Layout>
  )
}
