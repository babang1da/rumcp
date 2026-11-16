import { HeadContent, Scripts, createRootRoute } from '@tanstack/react-router'

import { metrikaCounterId, metrikaSnippet, webmasterVerificationMeta } from '../seo/counters'
import appCss from '../styles.css?url'

/** Счётчик и подтверждение Вебмастера приходят только из env: пустое значение — тегов нет. */
const metrikaId = metrikaCounterId(import.meta.env.VITE_METRIKA_ID)
const verification = webmasterVerificationMeta(import.meta.env.VITE_WEBMASTER_VERIFICATION)

export const Route = createRootRoute({
  head: () => ({
    meta: [
      {
        charSet: 'utf-8',
      },
      {
        name: 'viewport',
        content: 'width=device-width, initial-scale=1',
      },
      {
        title: 'RuMCP — MCP-коннекторы к сервисам Яндекса',
      },
      {
        name: 'description',
        content:
          'Удалённый MCP-сервер для Яндекс Метрики и Вебмастера: один адрес, вход через Яндекс, без установки.',
      },
      ...(verification ? [verification] : []),
    ],
    links: [
      {
        rel: 'stylesheet',
        href: appCss,
      },
    ],
  }),
  shellComponent: RootDocument,
})

function RootDocument({ children }: { children: React.ReactNode }) {
  return (
    <html lang="ru">
      <head>
        <HeadContent />
      </head>
      <body>
        {children}

        {metrikaId && (
          <>
            <script dangerouslySetInnerHTML={{ __html: metrikaSnippet(metrikaId) }} />
            <noscript>
              <div>
                <img
                  src={`https://mc.yandex.ru/watch/${metrikaId}`}
                  style={{ position: 'absolute', left: '-9999px' }}
                  alt=""
                />
              </div>
            </noscript>
          </>
        )}

        <Scripts />
      </body>
    </html>
  )
}
