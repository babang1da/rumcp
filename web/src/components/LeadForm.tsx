import { useState } from 'react'
import type { FormEvent } from 'react'

import { submitLead } from '../server/leads'

type FormState = 'idle' | 'busy' | 'done' | 'error'

/** Форма заявки на коннектор, которого ещё нет: пишет в `POST /api/leads`. */
export function LeadForm({ service, name }: { service: string; name: string }) {
  const [email, setEmail] = useState('')
  const [state, setState] = useState<FormState>('idle')
  const [error, setError] = useState('')

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setState('busy')
    setError('')

    const result = await submitLead(email, service)
    if (result.ok) {
      setState('done')
      setEmail('')
    } else {
      setState('error')
      setError(result.error)
    }
  }

  if (state === 'done') {
    return (
      <p className="note" role="status">
        Спасибо! Записали адрес — напишем, когда {name} заработает.
      </p>
    )
  }

  return (
    <form className="lead-form" onSubmit={submit}>
      <label htmlFor="lead-email">Почта для ответа</label>
      <div className="copy-row">
        <input
          id="lead-email"
          type="email"
          name="email"
          autoComplete="email"
          required
          value={email}
          onChange={(event) => setEmail(event.currentTarget.value)}
          placeholder="you@example.ru"
        />
        <button type="submit" className="button" disabled={state === 'busy'}>
          {state === 'busy' ? 'Отправляем…' : 'Сообщить о запуске'}
        </button>
      </div>
      {state === 'error' && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <p className="hint">Одно письмо о запуске. Без рассылок, адрес не передаём третьим лицам.</p>
    </form>
  )
}
