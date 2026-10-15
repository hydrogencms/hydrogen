import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// Build: one ES module, dist/elements.js, that defines the custom elements.
// Dev (npm run dev + ADMIN_ELEMENTS_DEV_URL=http://localhost:5174): hydrogen proxies <admin.prefix>/x/dev/* here,
// so base must match admin.prefix: set ADMIN_PREFIX when it is not /admin.
const prefix = (process.env.ADMIN_PREFIX ?? '/admin').replace(/\/+$/, '')

export default defineConfig(({ command }) => ({
  base: command === 'serve' ? `${prefix}/x/dev/` : './',
  plugins: [svelte()],
  server: { port: 5174, strictPort: true },
  build: {
    lib: { entry: 'src/elements.ts', formats: ['es'], fileName: () => 'elements.js' },
    outDir: 'dist',
    emptyOutDir: true,
  },
}))
