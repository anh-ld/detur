import { useEffect, useState } from 'preact/hooks';
import { ToastContainer } from 'kinu';
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
// decides who enters, admin decides who manages.
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
            {ui.adminSet && !ui.admin && (
              <button type="button" class="nav-link" aria-label="Enter admin" onClick={() => ensureAdmin()}>
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75"
                  stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                  <rect x="4" y="11" width="16" height="10" rx="2" />
                  <path d="M8 11V7a4 4 0 0 1 8 0v4" />
                </svg>
                Admin
              </button>
            )}
            {ui.admin && (
              <span role="status" class="admin-chip">
                <span class="admin-chip-dot" aria-hidden="true" />
                Admin · until {fmtUntil(ui.until)}
                <button type="button" aria-label="Exit" title="Exit admin" onClick={exitAdmin}>
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"
                    stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
                    <path d="M12 2v10M18.36 6.64a9 9 0 1 1-12.73 0" />
                  </svg>
                </button>
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
