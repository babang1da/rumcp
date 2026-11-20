import { defineConfig } from 'vitest/config'

// Отдельный конфиг: тесты не поднимают TanStack Start и Vite-плагин сборки.
export default defineConfig({
  test: {
    include: ['src/**/*.test.ts'],
  },
})
