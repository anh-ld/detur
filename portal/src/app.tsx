import { useEffect, useState } from 'preact/hooks';
import { Button, Separator } from 'kinu';
import { row } from './ui';
import { AppsPage } from './apps';
import { DetailPage } from './detail';
import { SettingsPage } from './settings';

type Route = { name: 'apps' } | { name: 'detail'; id: string } | { name: 'settings' };

function parseHash(): Route {
  const h = location.hash;
  const m = h.match(/^#\/apps\/([^/?#]+)/);
  if (m) return { name: 'detail', id: decodeURIComponent(m[1]) };
  if (h.startsWith('#/settings')) return { name: 'settings' };
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

  const nav = (href: string, label: string, active: boolean) => (
    <Button size="sm" variant={active ? 'secondary' : 'ghost'} href={href}>
      {label}
    </Button>
  );

  return (
    <>
      <header style={{ maxWidth: 1040, margin: '0 auto', padding: '12px 24px', ...row, justifyContent: 'space-between' }}>
        <div style={{ ...row, gap: 24 }}>
          <a href="#/" style={{ fontWeight: 700, fontSize: 18, color: 'inherit', textDecoration: 'none' }}>
            detur
          </a>
          <nav style={row}>
            {nav('#/', 'Apps', route.name !== 'settings')}
            {nav('#/settings', 'Settings', route.name === 'settings')}
          </nav>
        </div>
      </header>
      <Separator />
      <main style={{ maxWidth: 1040, margin: '0 auto', padding: '0 24px 64px' }}>
        {route.name === 'apps' && <AppsPage />}
        {route.name === 'detail' && <DetailPage id={route.id} />}
        {route.name === 'settings' && <SettingsPage />}
      </main>
    </>
  );
}
