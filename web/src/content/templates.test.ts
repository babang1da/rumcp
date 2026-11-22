import { describe, expect, it } from 'vitest'
import { reportTemplates } from './templates'

describe('reportTemplates', () => {
  it('содержит четыре шаблона из спеки эпика', () => {
    const titles = reportTemplates.map((t) => t.title)
    for (const title of [
      'Каналы трафика и конверсии',
      'Цели по дням',
      'Страницы и отказы',
      'Еженедельный дайджест',
    ]) {
      expect(titles).toContain(title)
    }
    expect(reportTemplates.length).toBeGreaterThanOrEqual(4)
  })

  it('id уникальны', () => {
    const ids = reportTemplates.map((t) => t.id)
    expect(new Set(ids).size).toBe(ids.length)
  })

  it.each(reportTemplates.map((t) => [t.id, t] as const))('%s: все поля заполнены', (_id, t) => {
    for (const key of ['id', 'title', 'task', 'prompt', 'outputFormat', 'note'] as const) {
      expect(t[key].trim(), key).not.toBe('')
    }
    expect(t.outputFormat).toContain('| --- ')
    expect(t.note).toContain('Claude Desktop')
    expect(t.note).toContain('Cursor')
  })
})
