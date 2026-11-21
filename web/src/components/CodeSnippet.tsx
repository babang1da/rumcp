import { useState } from 'react'

/** Блок кода с именем файла и кнопкой копирования конфигурации в один клик. */
export function CodeSnippet({ code, file }: { code: string; file: string }) {
  const [copied, setCopied] = useState(false)

  async function copy() {
    try {
      await navigator.clipboard.writeText(code)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Буфер недоступен: конфиг можно выделить в блоке и скопировать вручную.
    }
  }

  return (
    <figure className="code-card">
      <figcaption>
        <code>{file}</code>
        <button type="button" className="button button-quiet" onClick={copy}>
          {copied ? 'Скопировано' : 'Скопировать'}
        </button>
      </figcaption>
      <pre>
        <code>{code}</code>
      </pre>
      <span className="sr-only" role="status">
        {copied ? 'Конфигурация скопирована' : ''}
      </span>
    </figure>
  )
}
