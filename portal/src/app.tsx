import { useEffect, useState } from 'preact/hooks';
import { Chip, Kbd, Select, Status, ToastContainer } from 'kinu';
import { api } from './api';
import {
  AdminPasswordDialog,
  ensureAdmin,
  Icon,
  refreshAdminSession,
  setAdminAvailable,
  setElevated,
  useAdmin,
} from './ui';
import { AppsPage } from './apps';
import { ApiTokensPage } from './api-tokens';
import { AppPage, AppTab, buildAppHash, tabHref, TABS } from './detail';
import type { App as AppT, DateRangeFilter, Platform } from './api';

type Route =
  | { name: 'apps' }
  | { name: 'api' }
  | {
      name: 'app';
      id: string;
      tab: AppTab;
      linkId?: string;
      platform?: Platform;
      range?: DateRangeFilter;
    };

export function parseHash(h: string = location.hash): Route {
  if (/^#\/api(?:[/?#]|$)/.test(h)) return { name: 'api' };
  const m = h.match(/^#\/apps\/([^/?#]+)(?:\/(analytics|fraud|settings|webhooks))?(?:\?(.*))?/);
  if (m) {
    try {
      const id = decodeURIComponent(m[1]);
      const tab = (m[2] as AppTab | undefined) ?? 'links';
      let linkId: string | undefined;
      let platform: Platform | undefined;
      let range: DateRangeFilter | undefined;
      if (m[3]) {
        const q = new URLSearchParams(m[3]);
        linkId = q.get('link') ?? undefined;

        const p = (q.get('platform') ?? '').toLowerCase();
        if (p === 'ios' || p === 'android' || p === 'desktop') {
          platform = p as Platform;
        }

        const win = q.get('window');
        const from = q.get('from') ?? undefined;
        const to = q.get('to') ?? undefined;
        const daysParam = q.get('days');

        if (win === '15m' || win === '1h' || win === '3h' || win === '12h') {
          range = { window: win };
        } else if (from && to) {
          range = { from, to };
        } else if (daysParam) {
          const d = parseInt(daysParam, 10);
          if (!isNaN(d) && d > 0) {
            range = { days: d };
          }
        }
      }
      return { name: 'app', id, tab, linkId, platform, range };
    } catch {
      // bad %-encoding: unknown route
    }
  }
  return { name: 'apps' };
}

// tab link: Links <-> Analytics keep the filter query
type AppRoute = Extract<Route, { name: 'app' }>;
const navHref = (r: AppRoute, tab: AppTab) =>
  (r.tab === 'links' || r.tab === 'analytics') && (tab === 'links' || tab === 'analytics')
    ? buildAppHash(r.id, tab, { platform: r.platform, range: r.range, linkId: tab === 'analytics' ? r.linkId : undefined })
    : tabHref(r.id, tab);

// top-bar "Admin · until HH:MM"; local clock, server enforces expiry
const fmtUntil = (iso: string) => {
  const d = new Date(iso);
  return `${d.getHours()}:${String(d.getMinutes()).padStart(2, '0')}`;
};

// SPA shell: hash routing (survives refresh), no auth. Gateway gates entry, admin gates management.
export function App() {
  const [route, setRoute] = useState<Route>(parseHash);
  // server LOGOUT_URL; '' (unset/fetch failed) hides Log out
  const [logoutUrl, setLogoutUrl] = useState('');
  const ui = useAdmin();
  const [appName, setAppName] = useState('');
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
      if (t) location.hash = navHref(route, t.tab);
    };
    addEventListener('keydown', on);
    return () => removeEventListener('keydown', on);
  }, [route]);

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
                href={navHref(route, t.tab)}
                aria-current={t.tab === route.tab ? 'page' : undefined}
                onClick={(e) => {
                  e.preventDefault();
                  location.hash = navHref(route, t.tab);
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
            <Chip role="status" class="admin-chip">
              <Status variant="success" />
              Admin · until {fmtUntil(ui.until)}
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

// sidebar app switcher: native select of apps (same tab), All apps last
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
