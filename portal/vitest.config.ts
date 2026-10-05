import { defineConfig, mergeConfig } from 'vitest/config';
import viteConfig from './vite.config';

// flows.test.tsx: client-flow integration vs real Go binary + fresh SQLite. stale.test.tsx: mocked-fetch unit. Both in happy-dom.
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
