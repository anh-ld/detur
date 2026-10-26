import { defineConfig } from 'vite';

const jsx = { runtime: 'automatic', importSource: 'preact' } as const;

// base "./": Go binary serves portal from any path
// oxc does JSX (Preact automatic runtime), no Babel. No HMR state keep: edits full-reload.
export default defineConfig({
  base: './',
  oxc: { jsx },
  optimizeDeps: { rolldownOptions: { transform: { jsx } } }, // dep scan reads its own JSX setting
  build: { outDir: 'dist' },
  // dev: proxy API calls to portal listener. Keep Host: the portal guard needs Origin == Host (else every write is 403).
  server: { proxy: { '/api': { target: 'http://127.0.0.1:8081', changeOrigin: false } } },
});
