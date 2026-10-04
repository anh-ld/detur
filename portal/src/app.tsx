import { useEffect, useState } from 'preact/hooks';
import { Separator } from 'kinu';
import { row } from './ui';
import { AppsPage } from './apps';
import { DetailPage } from './detail';
import { SettingsPage } from './settings';

type Route = { name: 'apps' } | { name: 'detail' | 'settings'; id: string };

export function parseHash(h: string = location.hash): Route {
  const m = h.match(/^#\/apps\/([^/?#]+)(\/settings)?/);
  if (m) {
    try {
      return { name: m[2] ? 'settings' : 'detail', id: decodeURIComponent(m[1]) };
    } catch {
      // malformed %-encoding in id: treat as unknown route
    }
  }
  return { name: 'apps' };
}

// Thin SPA shell: hash routing (refresh keeps the page), no auth.
export function App() {
  const [route, setRoute] = useState<Route>(parseHash);
  useEffect(() => {
    const on = () => {
      setRoute(parseHash());
      window.scrollTo(0, 0);
    };
    addEventListener('hashchange', on);
    return () => removeEventListener('hashchange', on);
  }, []);

  return (
    <>
      <header style={{ maxWidth: 1040, margin: '0 auto', padding: '16px 24px', ...row, justifyContent: 'space-between' }}>
        <div style={{ ...row, gap: 24 }}>
          <a href="#/" style={{ fontWeight: 700, fontSize: 18, color: 'inherit', textDecoration: 'none' }}>
            detur
          </a>
        </div>
      </header>
      <Separator />
      <main style={{ maxWidth: 1040, margin: '0 auto', padding: '0 24px 64px' }}>
        {route.name === 'apps' && <AppsPage />}
        {route.name === 'detail' && <DetailPage id={route.id} />}
        {route.name === 'settings' && <SettingsPage id={route.id} />}
      </main>
    </>
  );
}
