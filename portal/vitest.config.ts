import { defineConfig } from 'vitest/config';
import preact from '@preact/preset-vite';

// One suite: client-flow integration vs real Go binary + fresh SQLite, in happy-dom.
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