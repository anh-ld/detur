import { defineConfig } from 'vitest/config';
import preact from '@preact/preset-vite';

// flows.test.tsx: client-flow integration vs real Go binary + fresh SQLite. stale.test.tsx: mocked-fetch unit. Both in happy-dom.
export default defineConfig({
  plugins: [preact()],
  test: {
    include: ['tests/**/*.test.tsx'],
    environment: 'happy-dom',
    setupFiles: ['tests/setup.ts'],
    globals: true,
    testTimeout: 30_000,
    hookTimeout: 120_000,
    fileParallelism: false,
  },
});