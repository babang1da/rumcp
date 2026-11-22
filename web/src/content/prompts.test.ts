import { describe, expect, it } from 'vitest'

import { findPromptScenario, promptScenarios } from './prompts'

describe('prompts', () => {
  it('содержит не менее трёх сценариев, включая все три из гипотезы эпика', () => {
    expect(promptScenarios.length).toBeGreaterThanOrEqual(3)

    const titles = promptScenarios.map((s) => s.title)
    expect(titles).toContain('Анализ падения органики')
    expect(titles).toContain('Поиск неэффективных страниц')
    expect(titles).toContain('Когорты по источникам')
  })

  it('каждый сценарий имеет уникальный непустой id', () => {
    const ids = promptScenarios.map((s) => s.id)
    expect(new Set(ids).size).toBe(promptScenarios.length)

    for (const id of ids) {
      expect(id.trim()).toBe(id)
      expect(id.length).toBeGreaterThan(0)
    }
  })

  it('каждый сценарий имеет непустые title, задачу и промпт', () => {
    for (const scenario of promptScenarios) {
      expect(scenario.title.trim().length).toBeGreaterThan(0)
      expect(scenario.task.trim().length).toBeGreaterThan(20)
      expect(scenario.prompt.trim().length).toBeGreaterThan(50)
    }
  })

  it('каждый сценарий имеет ≥1 пример диалога с репликами маркетолога и ассистента', () => {
    for (const scenario of promptScenarios) {
      expect(scenario.dialog.length).toBeGreaterThanOrEqual(1)

      const roles = scenario.dialog.map((line) => line.role)
      expect(roles).toContain('user')
      expect(roles).toContain('assistant')

      for (const line of scenario.dialog) {
        expect(['user', 'assistant']).toContain(line.role)
        expect(line.text.trim().length).toBeGreaterThan(0)
      }
    }
  })

  it('ответы ассистента содержат реальные данные и метрики', () => {
    for (const scenario of promptScenarios) {
      const assistantReplies = scenario.dialog.filter((line) => line.role === 'assistant')
      for (const reply of assistantReplies) {
        expect(reply.text).toMatch(/\d+/)
      }
    }
  })

  it('findPromptScenario находит сценарий по id и возвращает undefined для несуществующего', () => {
    expect(findPromptScenario('organic-drop')?.title).toBe('Анализ падения органики')
    expect(findPromptScenario('ineffective-pages')?.title).toBe('Поиск неэффективных страниц')
    expect(findPromptScenario('source-cohorts')?.title).toBe('Когорты по источникам')
    expect(findPromptScenario('non-existent')).toBeUndefined()
  })
})
