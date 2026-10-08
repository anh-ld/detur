import { useEffect, useState } from 'preact/hooks';
import { Button, ToastContainer } from 'kinu';
import { api } from './api';
import { AdminPasswordDialog, ensureAdmin, getAdmin, getAdminSet, getAdminUntil, refreshAdminSession, row, setAdminAvailable, setElevated, subscribeAdmin } from './ui';
import { AppsPage } from './apps';
import { AppPage, AppTab } from './detail';

type Route = { name: 'apps' } | { name: 'app'; id: string; tab: AppTab };

export function parseHash(h: string = location.hash): Route {
  const m = h.match(/^#\/apps\/([^/?#]+)(?:\/(analytics|fraud|settings))?/);
  if (m) {
    try {
      return { name: 'app', id: decodeURIComponent(m[1]), tab: (m[2] as AppTab | undefined) ?? 'links' };
    } catch {
      // malformed %-encoding in id: treat as unknown route
    }
  }
  return { name: 'apps' };
}

// "Admin · until HH:MM" in the top bar (local clock; the server enforces expiry).
const fmtUntil = (iso: string) => {
  const d = new Date(iso);
  return `${d.getHours()}:${String(d.getMinutes()).padStart(2, '0')}`;
};

const adminSnapshot = () => ({ adminSet: getAdminSet(), admin: getAdmin(), until: getAdminUntil() });

// Thin SPA shell: hash routing (refresh keeps the page), no auth — the gateway
// decides who enters, admin mode decides who manages.
export function App() {
  const [route, setRoute] = useState<Route>(parseHash);
  // LOGOUT_URL from the server; '' (unset or fetch failed) hides Log out.
  const [logoutUrl, setLogoutUrl] = useState('');
  const [ui, setUi] = useState(adminSnapshot);
  useEffect(() => subscribeAdmin(() => setUi(adminSnapshot())), []);
  useEffect(() => {
    api.getConfig().then((c) => {
      setLogoutUrl(c.logoutUrl);
      setAdminAvailable(c.adminSet);
    }, () => {});
    refreshAdminSession();
  }, []);
  // The chip is a local estimate: clear it at the session's own expiry; a mid-use
  // 403 clears it earlier via refreshAdminSession.
  useEffect(() => {
    if (!ui.admin || !ui.until) return;
    const ms = new Date(ui.until).getTime() - Date.now();
    if (ms <= 0) {
      setElevated(false);
      return;
    }
    const t = setTimeout(() => setElevated(false), ms);
    return () => clearTimeout(t);
  }, [ui.admin, ui.until]);
  useEffect(() => {
    const on = () => {
      setRoute(parseHash());
      window.scrollTo(0, 0);
    };
    addEventListener('hashchange', on);
    return () => removeEventListener('hashchange', on);
  }, []);

  const exitAdmin = async () => {
    try {
      await api.exitAdminMode();
      setElevated(false); // the DELETE response already carries admin:false
    } catch {
      await refreshAdminSession();
    }
  };

  return (
    <>
      <header>
        <div style={{ ...row, maxWidth: 1040, margin: '0 auto', padding: '12px 24px', justifyContent: 'space-between' }}>
          <a href="#/" style={{ fontWeight: 500, fontSize: 14, color: 'inherit', textDecoration: 'none' }}>
            detur
          </a>
          <div style={{ ...row, gap: 20 }}>
            <a class="nav-link" href="https://github.com/anh-ld/detur" target="_blank" rel="noreferrer">
              <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
                <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8z" />
              </svg>
              GitHub
            </a>
            {ui.adminSet && !ui.admin && (
              <Button size="sm" variant="outline" onClick={() => ensureAdmin()}>
                Enter admin mode
              </Button>
            )}
            {ui.admin && (
              <span role="status" style={{ ...row, gap: 8, fontSize: 13 }}>
                <span style={{ ...row, gap: 6, color: 'hsl(var(--k-muted-foreground))', fontSize: 13 }}>
                  Admin · until {fmtUntil(ui.until)}
                </span>
                <Button size="sm" variant="outline" onClick={exitAdmin}>
                  Exit
                </Button>
              </span>
            )}
            {logoutUrl && (
              <a class="nav-link" href={logoutUrl}>
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75"
                  stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" />
                </svg>
                Log out
              </a>
            )}
          </div>
        </div>
      </header>
      <main style={{ maxWidth: 1040, margin: '0 auto', padding: '0 24px 64px' }}>
        {route.name === 'apps' && <AppsPage />}
        {route.name === 'app' && <AppPage id={route.id} tab={route.tab} />}
      </main>
      <AdminPasswordDialog />
      <ToastContainer />
    </>
  );
}
