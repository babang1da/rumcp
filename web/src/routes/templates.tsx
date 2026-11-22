import { createFileRoute } from '@tanstack/react-router'

import { CopyField } from '../components/CopyField'
import { Layout, LoginButton } from '../components/Layout'
import { reportTemplates } from '../content/templates'
import { pageHead } from '../seo/pages'
import { getSiteUrl } from '../server/functions'

export const Route = createFileRoute('/templates')({
  loader: () => getSiteUrl(),
  head: ({ loaderData }) => {
    if (!loaderData) return {}

    return pageHead({
      title: 'Шаблоны отчётов Яндекс Метрики — RuMCP',
      description:
        'Готовые шаблоны отчётов Яндекс Метрики для Claude Desktop и Cursor: промпт под копирование, вывод Markdown-таблицей и графики через Claude Artifacts. Подключите RuMCP одним входом.',
      path: '/templates',
      siteUrl: loaderData,
    })
  },
  component: TemplatesPage,
})

function TemplatesPage() {
  return (
    <Layout>
      <article className="doc">
        <h1>Шаблоны отчётов Яндекс Метрики</h1>
        <p className="lead">
          Готовый формат отчёта для клиента или руководства: скопируйте промпт, вставьте в ассистента
          с подключённой Метрикой — и получите таблицу, которую можно сразу показывать.
        </p>

        {reportTemplates.map((template) => (
          <section
            key={template.id}
            className="block template"
            aria-labelledby={`template-${template.id}`}
          >
            <h2 id={`template-${template.id}`}>{template.title}</h2>
            <p className="scenario-task">
              <strong>Задача.</strong> {template.task}
            </p>
            <CopyField label="Промпт для ассистента" value={template.prompt} />
            <div className="template-output">
              <h3>Формат вывода</h3>
              <pre>
                <code>{template.outputFormat}</code>
              </pre>
            </div>
            <p className="template-note">{template.note}</p>
            <div className="actions">
              <LoginButton>Подключить RuMCP</LoginButton>
            </div>
          </section>
        ))}
      </article>
    </Layout>
  )
}
