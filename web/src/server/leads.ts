/** Заявка на коннектор, которого ещё нет: `POST /api/leads` сервиса на Go. */

export type LeadResult = { ok: true } | { ok: false; error: string }

export const LEAD_ERROR = 'Не получилось отправить. Попробуйте ещё раз.'
export const LEAD_EMAIL_ERROR = 'Проверьте адрес почты.'

/** Отправляет заявку и переводит ответ API в понятный результат для формы. */
export async function submitLead(
  email: string,
  service: string,
  fetchImpl: typeof fetch = fetch,
): Promise<LeadResult> {
  const trimmed = email.trim()
  if (!trimmed) return { ok: false, error: LEAD_EMAIL_ERROR }

  try {
    const response = await fetchImpl('/api/leads', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ email: trimmed, service }),
    })
    if (response.status === 400) return { ok: false, error: LEAD_EMAIL_ERROR }
    if (!response.ok) return { ok: false, error: LEAD_ERROR }
    return { ok: true }
  } catch {
    return { ok: false, error: LEAD_ERROR }
  }
}
