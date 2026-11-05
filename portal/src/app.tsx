import { useEffect, useRef, useState } from 'preact/hooks';
import { ToastContainer } from 'kinu';
import { api } from './api';
import {
  AdminPasswordDialog,
  ensureAdmin,
  getAdmin,
  getAdminSet,
  getAdminUntil,
  Icon,
  refreshAdminSession,
  setAdminAvailable,
  setElevated,
  subscribeAdmin,
} from './ui';
import { AppsPage } from './apps';
import { ApiTokensPage } from './api-tokens';
import { AppPage, AppTab, tabHref, TABS } from './detail';
import type { App as AppT } from './api';

type Route = { name: 'apps' } | { name: 'api' } | { name: 'app'; id: string; tab: AppTab };

export function parseHash(h: string = location.hash): Route {
  if (/^#\/api(?:[/?#]|$)/.test(h)) return { name: 'api' };
  const m = h.match(/^#\/apps\/([^/?#]+)(?:\/(analytics|fraud|settings|webhooks))?/);
  if (m) {
    try {
      return { name: 'app', id: decodeURIComponent(m[1]), tab: (m[2] as AppTab | undefined) ?? 'links' };
    } catch {
      // bad %-encoding: unknown route
    }
  }
  return { name: 'apps' };
}

// top-bar "Admin · until HH:MM"; local clock, server enforces expiry
const fmtUntil = (iso: string) => {
  const d = new Date(iso);
  return `${d.getHours()}:${String(d.getMinutes()).padStart(2, '0')}`;
};

const adminSnapshot = () => ({ adminSet: getAdminSet(), admin: getAdmin(), until: getAdminUntil() });

// SPA shell: hash routing (survives refresh), no auth. Gateway gates entry, admin gates management.
export function App() {
  const [route, setRoute] = useState<Route>(parseHash);
  // server LOGOUT_URL; '' (unset/fetch failed) hides Log out
  const [logoutUrl, setLogoutUrl] = useState('');
  const [ui, setUi] = useState(adminSnapshot);
  const [appName, setAppName] = useState('');
  useEffect(() => subscribeAdmin(() => setUi(adminSnapshot())), []);
  useEffect(() => {
    api.getConfig().then(
      (c) => {
        setLogoutUrl(c.logoutUrl);
        setAdminAvailable(c.adminSet);
      },
      () => {},
    );
    refreshAdminSession();
  }, []);
  // chip = local estimate: clear at session expiry; mid-use 403 clears earlier (refreshAdminSession)
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

  // single-key tab shortcuts (sidebar <kbd>); ignored while typing or in a dialog
  useEffect(() => {
    if (route.name !== 'app') return;
    const on = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || e.repeat) return;
      const el = e.target as HTMLElement;
      if (el.closest('input, textarea, select, [contenteditable], dialog[open]')) return;
      const t = TABS.find((x) => x.key.toLowerCase() === e.key.toLowerCase());
      if (t) location.hash = tabHref(route.id, t.tab);
    };
    addEventListener('keydown', on);
    return () => removeEventListener('keydown', on);
  }, [route.name === 'app' ? route.id : '']);

  const exitAdmin = async () => {
    try {
      await api.exitAdminMode();
      setElevated(false); // DELETE response already says admin:false
    } catch {
      await refreshAdminSession();
    }
  };

  const lockIcon = (
    <svg
      width="16"
      height="16"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      stroke-width="1.75"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <rect x="4" y="11" width="16" height="10" rx="2" />
      <path d="M8 11V7a4 4 0 0 1 8 0v4" />
    </svg>
  );

  return (
    <div class="shell">
      <aside class="sidebar">
        <a href="#/" class="brand">
          detur
        </a>
        {route.name === 'app' && <AppSwitch id={route.id} tab={route.tab} name={appName} />}
        <nav aria-label="App sections" class="side-nav">
          {route.name === 'app' ? (
            TABS.map((t) => (
              <a
                key={t.tab}
                href={tabHref(route.id, t.tab)}
                aria-current={t.tab === route.tab ? 'page' : undefined}
                onClick={(e) => {
                  e.preventDefault();
                  location.hash = tabHref(route.id, t.tab);
                }}
              >
                <Icon d={t.icon} />
                {t.label}
                <kbd aria-hidden="true">{t.key}</kbd>
              </a>
            ))
          ) : (
            <>
              <a href="#/" aria-current={route.name === 'apps' ? 'page' : undefined}>
                <Icon d="M3 3h7v7H3zM14 3h7v7h-7zM14 14h7v7h-7zM3 14h7v7H3z" />
                Apps
              </a>
              <a href="#/api" aria-current={route.name === 'api' ? 'page' : undefined}>
                <Icon d="m8 6-6 6 6 6M16 6l6 6-6 6" />
                API
              </a>
            </>
          )}
        </nav>
        <div class="side-foot">
          {ui.adminSet && !ui.admin && (
            <button type="button" class="nav-link" aria-label="Enter admin" onClick={() => ensureAdmin()}>
              {lockIcon}
              Admin
            </button>
          )}
          {ui.admin && (
            <span role="status" class="admin-chip">
              <span class="admin-chip-dot" aria-hidden="true" />
              Admin · until {fmtUntil(ui.until)}
              <button type="button" aria-label="Exit" title="Exit admin" onClick={exitAdmin}>
                <svg
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  aria-hidden="true"
                >
                  <path d="M12 2v10M18.36 6.64a9 9 0 1 1-12.73 0" />
                </svg>
              </button>
            </span>
          )}
          {logoutUrl && (
            <a class="nav-link" href={logoutUrl}>
              <Icon d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" />
              Log out
            </a>
          )}
        </div>
      </aside>
      <main>
        {route.name === 'apps' && <AppsPage />}
        {route.name === 'api' && <ApiTokensPage />}
        {route.name === 'app' && <AppPage id={route.id} tab={route.tab} onName={setAppName} />}
      </main>
      <AdminPasswordDialog />
      <ToastContainer />
    </div>
  );
}

// sidebar app switcher: menu of apps (same tab), All apps last. Native <details>; closes on pick/outside click/Esc
function AppSwitch({ id, tab, name }: { id: string; tab: AppTab; name: string }) {
  const ref = useRef<HTMLDetailsElement>(null);
  const [apps, setApps] = useState<AppT[] | null>(null);
  const close = () => ref.current?.removeAttribute('open');
  useEffect(() => {
    const out = (e: Event) => !ref.current?.contains(e.target as Node) && close();
    const esc = (e: KeyboardEvent) => e.key === 'Escape' && close();
    addEventListener('pointerdown', out);
    addEventListener('keydown', esc);
    return () => {
      removeEventListener('pointerdown', out);
      removeEventListener('keydown', esc);
    };
  }, []);
  useEffect(close, [id, tab]);
  return (
    <details
      ref={ref}
      class="app-switch"
      onToggle={(e) => (e.currentTarget as HTMLDetailsElement).open && api.listApps().then(setApps, () => setApps([]))}
    >
      <summary aria-label={`Switch app, current: ${name}`}>
        <span class="app-switch-name">{name || '…'}</span>
        <Icon d="m7 15 5 5 5-5M7 9l5-5 5 5" />
      </summary>
      <div class="app-menu" role="menu">
        {apps === null ? (
          <span class="app-menu-note">Loading…</span>
        ) : (
          apps.map((a) => (
            <a key={a.id} role="menuitem" href={tabHref(a.id, tab)} aria-current={a.id === id ? 'true' : undefined}>
              <span class="app-switch-name">{a.name}</span>
              {a.id === id && <Icon d="M20 6 9 17l-5-5" />}
            </a>
          ))
        )}
        <hr />
        <a role="menuitem" href="#/">
          <Icon d="M3 3h7v7H3zM14 3h7v7h-7zM14 14h7v7h-7zM3 14h7v7H3z" />
          All apps
        </a>
      </div>
    </details>
  );
}
