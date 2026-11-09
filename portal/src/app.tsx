import { useEffect, useState } from 'preact/hooks';
import { Chip, Kbd, Select, Status, ToastContainer } from 'kinu';
import { api } from './lib/api';
import { AdminPasswordDialog } from './components/admin';
import { ensureAdmin, refreshAdminSession, setAdminAvailable, setElevated, useAdmin } from './lib/admin';
import { Icon } from './components/icons';
import { AppsPage } from './pages/apps';
import { ApiTokensPage } from './pages/api-tokens';
import { AppPage } from './pages/app';
import { AppTab, parseHash, Route, switchTab, tabHref, TABS } from './lib/route';
import type { App as AppT } from './lib/types';

const fmtUntil = (iso: string) => {
  const d = new Date(iso);
  return `${d.getHours()}:${String(d.getMinutes()).padStart(2, '0')}`;
};

export function App() {
  const [route, setRoute] = useState<Route>(parseHash);
  const [logoutUrl, setLogoutUrl] = useState('');
  const [version, setVersion] = useState('');
  const ui = useAdmin();
  const [appName, setAppName] = useState('');
  useEffect(() => {
    api.getConfig().then(
      (c) => {
        setLogoutUrl(c.logoutUrl);
        setVersion(c.version);
        setAdminAvailable(c.adminSet);
      },
      () => {},
    );
    refreshAdminSession();
  }, []);
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

  useEffect(() => {
    if (route.name !== 'app') return;
    const on = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey || e.repeat) return;
      const el = e.target as HTMLElement;
      if (el.closest('input, textarea, select, [contenteditable], dialog[open]')) return;
      const t = TABS.find((x) => x.key.toLowerCase() === e.key.toLowerCase());
      if (t) location.hash = switchTab(route, t.tab);
    };
    addEventListener('keydown', on);
    return () => removeEventListener('keydown', on);
  }, [route]);

  const exitAdmin = async () => {
    try {
      await api.exitAdminMode();
      setElevated(false);
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
                href={switchTab(route, t.tab)}
                aria-current={t.tab === route.tab ? 'page' : undefined}
                onClick={(e) => {
                  e.preventDefault();
                  location.hash = switchTab(route, t.tab);
                }}
              >
                <Icon d={t.icon} />
                {t.label}
                <Kbd aria-hidden="true">{t.key}</Kbd>
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
            <Chip role="status" class="admin-chip" title={`Admin until ${fmtUntil(ui.until)}`}>
              <Status variant="success" />
              Admin
              <Chip.Button aria-label="Exit" title="Exit admin" onClick={exitAdmin}>
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
              </Chip.Button>
            </Chip>
          )}
          {logoutUrl && (
            <a class="nav-link" href={logoutUrl}>
              <Icon d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" />
              Log out
            </a>
          )}
          <div class="version">
            {version && <span>{/^\d/.test(version) ? `v${version}` : version}</span>}
            <a class="nav-link" href="https://github.com/anh-ld/detur" target="_blank" rel="noreferrer" aria-label="GitHub">
              <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
                <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8z" />
              </svg>
            </a>
          </div>
        </div>
      </aside>
      <main>
        {route.name === 'apps' && <AppsPage />}
        {route.name === 'api' && <ApiTokensPage />}
        {route.name === 'app' && (
          <AppPage
            id={route.id}
            tab={route.tab}
            linkId={route.linkId}
            platform={route.platform}
            range={route.range}
            onName={setAppName}
          />
        )}
      </main>
      <AdminPasswordDialog />
      <ToastContainer />
    </div>
  );
}

function AppSwitch({ id, tab, name }: { id: string; tab: AppTab; name: string }) {
  const [apps, setApps] = useState<AppT[]>([]);
  useEffect(() => {
    api.listApps().then(setApps, () => setApps([]));
  }, [id]);
  return (
    <Select
      class="app-switch"
      aria-label="Switch app"
      value={id}
      onChange={(e) => (location.hash = e.currentTarget.value ? tabHref(e.currentTarget.value, tab) : '#/')}
    >
      {!apps.some((a) => a.id === id) && <option value={id}>{name || '…'}</option>}
      {apps.map((a) => (
        <option key={a.id} value={a.id}>
          {a.name}
        </option>
      ))}
      <option value="">All apps</option>
    </Select>
  );
}
