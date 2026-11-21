import { Link } from '@tanstack/react-router'

export function Layout({ children }: { children: React.ReactNode }) {
  return (
    <>
      <a className="skip" href="#content">
        К содержимому
      </a>
      <header className="site-header">
        <Link to="/" className="brand">
          RuMCP
        </Link>
        <nav aria-label="Основная">
          <Link to="/docs/$client" params={{ client: 'claude' }}>
            Инструкции
          </Link>
          <Link to="/cabinet">Кабинет</Link>
        </nav>
      </header>
      <main id="content">{children}</main>
      <footer className="site-footer">
        <p>RuMCP — MCP-коннекторы к сервисам Яндекса. Ассистент только читает данные.</p>
      </footer>
    </>
  )
}

/** Вход идёт на сервис Go, поэтому обычная ссылка, а не маршрут роутера. */
export function LoginButton({
  children = 'Войти через Яндекс',
  href = '/oauth/yandex/login',
}: {
  children?: React.ReactNode
  href?: string
}) {
  return (
    <a className="button" href={href}>
      {children}
    </a>
  )
}
