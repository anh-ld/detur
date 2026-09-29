import { defineConfig } from 'vite';
import preact from '@preact/preset-vite';

// base "./": Go binary serves the built portal from any path.
export default defineConfig({
  base: './',
  plugins: [preact()],
  build: { outDir: 'dist' },
  // Dev convenience: forward API calls to the portal listener.
  server: { proxy: { '/api': 'http://127.0.0.1:8081' } },
});