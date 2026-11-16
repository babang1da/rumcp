import { useId, useState } from 'react'

export function CopyField({ value, label }: { value: string; label: string }) {
  const id = useId()
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Буфер недоступен: адрес можно выделить и скопировать вручную.
    }
  }

  return (
    <div className="copy-field">
      <label htmlFor={id}>{label}</label>
      <div className="copy-row">
        <input id={id} readOnly value={value} onFocus={(e) => e.currentTarget.select()} />
        <button type="button" className="button button-quiet" onClick={copy}>
          {copied ? 'Скопировано' : 'Скопировать'}
        </button>
      </div>
      <span className="sr-only" role="status">
        {copied ? 'Адрес скопирован' : ''}
      </span>
    </div>
  )
}
