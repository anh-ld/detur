import { defineConfig } from 'vite';

const jsx = { runtime: 'automatic', importSource: 'preact' } as const;

// base "./": Go binary serves the built portal from any path.
// oxc does the JSX (Preact automatic runtime); no Babel preset. No HMR state keep: edits full-reload.
export default defineConfig({
  base: './',
  oxc: { jsx },
  optimizeDeps: { rolldownOptions: { transform: { jsx } } }, // dep scan reads its own JSX setting
  build: { outDir: 'dist' },
  // Dev convenience: forward API calls to the portal listener.
  server: { proxy: { '/api': 'http://127.0.0.1:8081' } },
});
