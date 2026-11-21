import { useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'

import { connectSetup, docClients } from '../content/docs'
import type { DocClient } from '../content/docs'
import { CodeSnippet } from './CodeSnippet'

/** Блок подключения к ассистентам: вкладки Claude Desktop, ChatGPT и Cursor с готовыми конфигами. */
export function ConnectBlock({ mcpUrl, active }: { mcpUrl: string; active?: DocClient['slug'] }) {
  const [current, setCurrent] = useState<DocClient['slug']>(active ?? 'claude')
  const tabsRef = useRef<Array<HTMLButtonElement | null>>([])

  function onTabKey(event: React.KeyboardEvent<HTMLButtonElement>, index: number) {
    const last = docClients.length - 1
    let next: number | null = null
    if (event.key === 'ArrowRight') next = index === last ? 0 : index + 1
    if (event.key === 'ArrowLeft') next = index === 0 ? last : index - 1
    if (event.key === 'Home') next = 0
    if (event.key === 'End') next = last
    if (next === null) return
    event.preventDefault()
    setCurrent(docClients[next].slug)
    tabsRef.current[next]?.focus()
  }

  return (
    <div className="connect">
      <div className="connect-tabs" role="tablist" aria-label="Куда подключить">
        {docClients.map((client, index) => (
          <button
            key={client.slug}
            ref={(el) => {
              tabsRef.current[index] = el
            }}
            type="button"
            role="tab"
            id={`connect-tab-${client.slug}`}
            aria-selected={current === client.slug}
            aria-controls={`connect-panel-${client.slug}`}
            tabIndex={current === client.slug ? 0 : -1}
            onClick={() => setCurrent(client.slug)}
            onKeyDown={(event) => onTabKey(event, index)}
          >
            {client.tabName ?? client.name}
          </button>
        ))}
      </div>

      {/* Панели рендерятся все: скрытые остаются в разметке для SSR и ссылок на инструкции. */}
      {docClients.map((client) => {
        const setup = connectSetup[client.slug]
        return (
          <div
            key={client.slug}
            role="tabpanel"
            id={`connect-panel-${client.slug}`}
            aria-labelledby={`connect-tab-${client.slug}`}
            hidden={current !== client.slug}
          >
            <p className="hint">{setup.lead}</p>
            {client.config && <CodeSnippet code={client.config(mcpUrl)} file={client.configFile!} />}
            <ol className="steps">
              {setup.steps.map((step) => (
                <li key={step}>{step}</li>
              ))}
            </ol>
            <p className="hint">
              Полная инструкция: <Link to="/docs/$client" params={{ client: client.slug }}>{client.name}</Link>.
            </p>
          </div>
        )
      })}
    </div>
  )
}
