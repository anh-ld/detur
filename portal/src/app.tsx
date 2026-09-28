import { useEffect, useState } from 'preact/hooks';
import { Button } from 'kinu';
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

  return (
    <div class="shell">
      <header class="topbar">
        <a class="brand" href="#/">detur</a>
        <nav>
          <Button size="sm" variant="ghost" href="#/">
            Apps
          </Button>
          <Button size="sm" variant="ghost" href="#/settings">
            Settings
          </Button>
        </nav>
      </header>
      <main>
        {route.name === 'apps' && <AppsPage />}
        {route.name === 'detail' && <DetailPage id={route.id} />}
        {route.name === 'settings' && <SettingsPage />}
      </main>
    </div>
  );
}