import { describe, expect, it } from 'vitest'

import { metrikaCounterId, metrikaSnippet, webmasterVerificationMeta } from './counters'

describe('metrikaCounterId', () => {
  it('пустое значение не даёт счётчика', () => {
    expect(metrikaCounterId(undefined)).toBeNull()
    expect(metrikaCounterId('')).toBeNull()
    expect(metrikaCounterId('   ')).toBeNull()
  })

  it('принимает номер счётчика и обрезает пробелы', () => {
    expect(metrikaCounterId(' 12345678 ')).toBe('12345678')
  })

  it('отбрасывает всё, что не похоже на номер — посторонний код в HTML не попадёт', () => {
    expect(metrikaCounterId('12"onload="alert(1)')).toBeNull()
    expect(metrikaCounterId('<script>')).toBeNull()
    expect(metrikaCounterId('123')).toBeNull()
  })
})

describe('metrikaSnippet', () => {
  it('содержит номер счётчика и инициализацию ym', () => {
    const snippet = metrikaSnippet('12345678')

    expect(snippet).toContain("ym(12345678,'init'")
    expect(snippet).toContain('mc.yandex.ru/metrika/tag.js')
  })
})

describe('webmasterVerificationMeta', () => {
  it('пустое значение — тега нет', () => {
    expect(webmasterVerificationMeta(undefined)).toBeNull()
    expect(webmasterVerificationMeta('')).toBeNull()
    expect(webmasterVerificationMeta('  ')).toBeNull()
  })

  it('значение из env становится мета-тегом подтверждения', () => {
    expect(webmasterVerificationMeta(' a1b2c3d4e5f6 ')).toEqual({
      name: 'yandex-verification',
      content: 'a1b2c3d4e5f6',
    })
  })

  it('кавычки и теги не проходят в атрибут', () => {
    expect(webmasterVerificationMeta('abc"><script>')).toBeNull()
  })
})
