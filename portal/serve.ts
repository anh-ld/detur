// dev server: Bun bundles index.html (HMR). /api -> Go portal listener, Host/Origin untouched
// (the portal guard needs Origin == Host, else every write is 403).
import index from './index.html';

const API = 'http://127.0.0.1:8081';
const port = Number(process.env.PORT ?? 8000);

Bun.serve({
  port,
  development: true,
  routes: { '/': index, '/api/*': (req) => fetch(API + new URL(req.url).pathname + new URL(req.url).search, req) },
});
console.log(`portal http://localhost:${port}`);
