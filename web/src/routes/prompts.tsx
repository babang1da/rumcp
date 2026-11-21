import { createFileRoute } from '@tanstack/react-router'

import { CopyField } from '../components/CopyField'
import { Layout, LoginButton } from '../components/Layout'
import { promptScenarios } from '../content/prompts'
import { pageHead } from '../seo/pages'
import { getSiteUrl } from '../server/functions'

export const Route = createFileRoute('/prompts')({
  loader: () => getSiteUrl(),
  head: ({ loaderData }) => {
    if (!loaderData) return {}

    return pageHead({
      title: 'Готовые промпты для Яндекс Метрики — RuMCP',
      description:
        'Готовые сценарии и промпты для Яндекс Метрики: анализ падения органики, источники трафика и цели. Скопируйте промпт в Claude, ChatGPT или Cursor и подключите Метрику одним входом.',
      path: '/prompts',
      siteUrl: loaderData,
    })
  },
  component: PromptsPage,
})

function PromptsPage() {
  return (
    <Layout>
      <article className="doc">
        <h1>Готовые сценарии и промпты для Метрики</h1>
        <p className="lead">
          Скопируйте промпт и вставьте в ассистента с подключённой Метрикой — он сам вызовет нужные
          инструменты и ответит данными вашего сайта.
        </p>

        {promptScenarios.map((scenario) => (
          <section
            key={scenario.id}
            className="block scenario"
            aria-labelledby={`scenario-${scenario.id}`}
          >
            <h2 id={`scenario-${scenario.id}`}>{scenario.title}</h2>
            <p className="scenario-task">
              <strong>Задача.</strong> {scenario.task}
            </p>
            <CopyField label="Промпт для ассистента" value={scenario.prompt} />
            <div className="dialog">
              <h3>Пример диалога</h3>
              {scenario.dialog.map((line, index) => (
                <p key={index} className="dialog-line" data-role={line.role}>
                  <span className="dialog-role">{line.role === 'user' ? 'Вы' : 'Ассистент'}</span>
                  {line.text}
                </p>
              ))}
            </div>
            <div className="actions">
              <LoginButton>Подключить Метрику</LoginButton>
            </div>
          </section>
        ))}
      </article>
    </Layout>
  )
}
