import { Link } from '@tanstack/react-router'

import { connectors } from '../content/connectors'
import type { Connector } from '../content/connectors'
import { ConnectBlock } from './ConnectBlock'
import { CopyField } from './CopyField'
import { Layout, LoginButton } from './Layout'
import { LeadForm } from './LeadForm'

/** Страница одного коннектора: описание, инструменты, шаги, FAQ и заявка для «скоро». */
export function ConnectorPage({ connector, mcpUrl }: { connector: Connector; mcpUrl: string }) {
  return (
    <Layout>
      <nav className="tabs" aria-label="Коннекторы">
        {connectors.map((item) => (
          <Link
            key={item.slug}
            to="/connectors/$slug"
            params={{ slug: item.slug }}
            activeProps={{ 'aria-current': 'page' }}
          >
            {item.name}
          </Link>
        ))}
      </nav>

      <article className="doc">
        <h1>{connector.h1}</h1>
        <p className="lead">{connector.lead}</p>

        {connector.ready ? (
          <CopyField label="Адрес MCP" value={mcpUrl} />
        ) : (
          <p className="status-soon">
            <strong>Скоро.</strong> Коннектор запустится, когда Яндекс откроет доступ к API.
          </p>
        )}

        <section className="block" aria-labelledby="tools">
          <h2 id="tools">Что умеет</h2>
          {connector.toolsNote && <p className="hint">{connector.toolsNote}</p>}
          <ul className="tools">
            {connector.tools.map((tool) => (
              <li key={tool.name}>
                <strong>{tool.name}.</strong> {tool.text}
              </li>
            ))}
          </ul>
        </section>

        <section className="block" aria-labelledby="steps">
          <h2 id="steps">{connector.ready ? 'Как подключить' : 'Как получить доступ'}</h2>
          <ol className="steps">
            {connector.steps.map((step) => (
              <li key={step}>{step}</li>
            ))}
          </ol>
          {connector.ready && <ConnectBlock mcpUrl={mcpUrl} />}
        </section>

        {!connector.ready && (
          <section className="block" aria-labelledby="wait">
            <h2 id="wait">Сообщить о запуске</h2>
            <LeadForm service={connector.service} name={connector.name} />
          </section>
        )}

        <section className="block" aria-labelledby="faq">
          <h2 id="faq">Частые вопросы</h2>
          {connector.faq.map((item) => (
            <div className="faq" key={item.question}>
              <h3>{item.question}</h3>
              <p>{item.answer}</p>
            </div>
          ))}
        </section>

        {connector.ready && (
          <p>
            Готовы попробовать? <LoginButton href="/oauth/yandex/login" />
          </p>
        )}
      </article>
    </Layout>
  )
}
