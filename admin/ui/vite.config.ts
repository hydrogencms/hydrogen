import { defineConfig, type Plugin } from 'vite';
import { closeSync, openSync } from 'node:fs';
import { svelte } from '@sveltejs/vite-plugin-svelte';

const goServer = process.env.ADMIN_API_URL ?? 'http://127.0.0.1:8080';

// Standalone dev (npm run dev) serves the app at /admin/ and answers the runtime config the Go handler would
// serve. Set ADMIN_PREFIX to match a non-default admin.prefix; behind the Go dev proxy Go answers config.json.
const devPrefix = (process.env.ADMIN_PREFIX ?? '/admin').replace(/\/+$/, '');
function devHost(): Plugin {
  return {
    name: 'h-dev-host',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        if (req.url?.split('?')[0] !== `${devPrefix}/config.json`) return next();
        res.setHeader('content-type', 'application/json');
        res.end(
          JSON.stringify({
            apiPrefix: '/api/manage/v1',
            prefix: devPrefix,
            pages: true,
            deliveryPrefix: '/api/deliver/v1',
          }),
        );
      });
    },
  };
}

// emptyOutDir removes the tracked admin/dist/.keep (it keeps go:embed valid on a fresh clone); put it back.
function keepDist(): Plugin {
  return {
    name: 'h-keep-dist',
    apply: 'build',
    closeBundle() {
      closeSync(openSync(new URL('../dist/.keep', import.meta.url), 'a'));
    },
  };
}

export default defineConfig(({ command }) => ({
  base: command === 'serve' && devPrefix ? devPrefix + '/' : './',
  plugins: [svelte(), devHost(), keepDist()],
  resolve: process.env.VITEST ? { conditions: ['browser'] } : {},
  build: {
    outDir: '../dist',
    emptyOutDir: true,
    assetsDir: 'assets',
    manifest: 'manifest.json',
    target: 'es2022',
    sourcemap: false,
  },
  server: {
    port: 5173,
    proxy: { '/api': goServer },
  },
  preview: { proxy: { '/api': goServer } },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
    setupFiles: ['./src/test-setup.ts'],
  },
}));
