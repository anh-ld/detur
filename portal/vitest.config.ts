import { defineConfig, mergeConfig } from 'vitest/config';
import viteConfig from './vite.config';

// flows/admin/pages.test.tsx: integration vs real Go binary + fresh SQLite (pages runs the short-link page scripts). stale.test.tsx: mocked-fetch unit. All in happy-dom.
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
