import { defineConfig } from 'vite'

import { tanstackStart } from '@tanstack/react-start/plugin/vite'

import viteReact from '@vitejs/plugin-react'
import { nitro } from 'nitro/vite'

const config = defineConfig({
  resolve: { tsconfigPaths: true },
  server: {
    // В dev `/api` и `/oauth` живут в сервисе на Go; в проде это делает прокси домена.
    proxy: {
      '/api': process.env.API_URL ?? 'http://localhost:8080',
      '/oauth': process.env.API_URL ?? 'http://localhost:8080',
    },
  },
  plugins: [nitro(), tanstackStart(), viteReact()],
})

export default config
