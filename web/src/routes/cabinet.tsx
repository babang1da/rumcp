import { createFileRoute, redirect, useRouter } from '@tanstack/react-router'
import { useState } from 'react'

import { CopyField } from '../components/CopyField'
import { Layout, LoginButton } from '../components/Layout'
import { usagePercent } from '../server/cabinet'
import { getCabinet } from '../server/functions'

export const Route = createFileRoute('/cabinet')({
  loader: async () => {
    const { state, mcpUrl } = await getCabinet()
    if (state.status === 'anonymous') {
      throw redirect({ href: '/oauth/yandex/login' })
    }
    return { state, mcpUrl }
  },
  head: () => ({ meta: [{ title: 'Кабинет — RuMCP' }, { name: 'robots', content: 'noindex' }] }),
  component: Cabinet,
})

function Cabinet() {
  const { state, mcpUrl } = Route.useLoaderData()

  if (state.status !== 'ok') {
    return (
      <Layout>
        <h1>Кабинет</h1>
        <p>Не удалось загрузить данные. Обновите страницу через минуту.</p>
        <a className="button" href="/cabinet">Обновить</a>
      </Layout>
    )
  }

  const { me } = state
  const { account, usage } = me
  const percent = usagePercent(usage.calls_today, usage.limit)
  const remaining = Math.max(0, usage.limit - usage.calls_today)
  const who = me.user.name || me.user.login

  return (
    <Layout>
      <h1>Кабинет</h1>
      <p className="lead who">{who}</p>

      <section className="block" aria-labelledby="acc">
        <h2 id="acc">Аккаунт Яндекса</h2>
        {account.connected ? (
          <>
            <p>
              Подключён: <strong>{me.user.login}</strong>
              {account.connected_at && <> с {formatDate(account.connected_at)}</>}.
            </p>
            <Disconnect />
          </>
        ) : (
          <>
            <p>Аккаунт не подключён. Войдите через Яндекс, чтобы ассистент мог читать данные.</p>
            <LoginButton />
          </>
        )}
      </section>

      <section className="block" aria-labelledby="addr">
        <h2 id="addr">Адрес MCP</h2>
        <CopyField label="Вставьте в ассистента" value={mcpUrl} />
        <p className="hint">
          Как добавить: <a href="/docs/claude">Claude</a>, <a href="/docs/chatgpt">ChatGPT</a>,{' '}
          <a href="/docs/cursor">Cursor</a>.
        </p>
      </section>

      <section className="block" aria-labelledby="use">
        <h2 id="use">Вызовы за сутки</h2>
        <p>
          Использовано {usage.calls_today} из {usage.limit}, осталось {remaining}.
        </p>
        <div
          className="meter"
          role="progressbar"
          aria-label="Дневной лимит вызовов"
          aria-valuemin={0}
          aria-valuemax={usage.limit}
          aria-valuenow={Math.min(usage.calls_today, usage.limit)}
        >
          <span style={{ width: `${percent}%` }} />
        </div>
        <p className="hint">Счётчик обнуляется в 00:00 UTC.</p>
      </section>
    </Layout>
  )
}

function Disconnect() {
  const router = useRouter()
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)

  async function disconnect() {
    if (!window.confirm('Отключить аккаунт Яндекса? Ассистент потеряет доступ к данным.')) return
    setBusy(true)
    setFailed(false)
    try {
      const response = await fetch('/api/disconnect', { method: 'POST' })
      if (!response.ok) throw new Error(String(response.status))
      await router.invalidate()
    } catch {
      setFailed(true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <button type="button" className="button button-danger" onClick={disconnect} disabled={busy}>
        {busy ? 'Отключаем…' : 'Отключить аккаунт'}
      </button>
      {failed && <p className="error" role="alert">Не получилось отключить. Попробуйте ещё раз.</p>}
    </>
  )
}

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' })
}
