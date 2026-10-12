import { defineConfig, mergeConfig } from 'vitest/config';
import viteConfig from './vite.config';

// flows/admin/pages: integration vs real Go binary + fresh SQLite (pages runs short-link scripts). stale: mocked-fetch unit. All happy-dom.
export default mergeConfig(viteConfig, defineConfig({
  test: {
    include: ['tests/**/*.test.tsx'],
    environment: 'happy-dom',
    setupFiles: ['tests/setup.ts'],
    globals: true,
    testTimeout: 30_000,
    hookTimeout: 120_000,
    fileParallelism: false,
  },
}));
