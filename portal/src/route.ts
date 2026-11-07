// Portal URLs: one module reads and writes the app hash, and owns the tab list and the filter carry-over rule.
import { isWindow } from './api';
import type { DateRangeFilter, Platform } from './api';

// sidebar nav: label, single-key shortcut, Lucide icon path (ISC)
export const TABS = [
  {
    tab: 'links',
    label: 'Links',
    key: 'L',
    icon: 'M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71',
  },
  { tab: 'analytics', label: 'Analytics', key: 'A', icon: 'M3 3v18h18M18 17V9M13 17V5M8 17v-3' },
  {
    tab: 'fraud',
    label: 'Fraud',
    key: 'F',
    icon: 'M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z',
  },
  {
    tab: 'settings',
    label: 'Settings',
    key: 'S',
    icon: 'M20 7h-9M14 17H5M17 20a3 3 0 1 0 0-6 3 3 0 0 0 0 6zM7 10a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
  },
  {
    tab: 'webhooks',
    label: 'Webhooks',
    key: 'W',
    icon: 'M18 16.98h-5.99c-1.1 0-1.95.94-2.48 1.9A4 4 0 0 1 2 17c.01-.7.2-1.4.57-2M6 17l3.13-5.78c.53-.97.1-2.18-.5-3.1a4 4 0 1 1 6.89-4.06M12 6l3.13 5.73C15.66 12.7 16.9 13 18 13a4 4 0 0 1 0 8',
  },
] as const;
export type AppTab = (typeof TABS)[number]['tab'];

// tab segment of an app hash: every tab but Links (the bare app hash)
const APP_HASH = new RegExp(`^#/apps/([^/?#]+)(?:/(${TABS.filter((t) => t.tab !== 'links').map((t) => t.tab).join('|')}))?(?:\\?(.*))?`);

export type Route =
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
  const m = h.match(APP_HASH);
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

        if (isWindow(win)) {
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

export const tabHref = (id: string, tab: AppTab) => `#/apps/${encodeURIComponent(id)}${tab === 'links' ? '' : '/' + tab}`;

export function buildAppHash(
  id: string,
  tab: AppTab,
  params: {
    platform?: Platform;
    range?: DateRangeFilter;
    linkId?: string;
  } = {},
): string {
  const base = tabHref(id, tab);
  const q = new URLSearchParams();
  if (params.linkId) {
    q.set('link', params.linkId);
  }
  if (params.platform) {
    q.set('platform', params.platform);
  }
  if (params.range) {
    if (params.range.window) {
      q.set('window', params.range.window);
    } else if (params.range.from && params.range.to) {
      q.set('from', params.range.from);
      q.set('to', params.range.to);
    } else if (params.range.days !== undefined) {
      q.set('days', String(params.range.days));
    }
  }
  const s = q.toString();
  return s ? `${base}?${s}` : base;
}

export type AppRoute = Extract<Route, { name: 'app' }>;
type Filters = { platform?: Platform; range?: DateRangeFilter; linkId?: string };

// Platform and range ride on Links and Analytics; the link filter only on Analytics. Takes inputs as given: no defaults.
const filtered = (tab: AppTab) => tab === 'links' || tab === 'analytics';
export const filteredHash = (id: string, tab: AppTab, f: Filters) =>
  filtered(tab) ? buildAppHash(id, tab, { platform: f.platform, range: f.range, linkId: tab === 'analytics' ? f.linkId : undefined }) : tabHref(id, tab);

// sidebar link / shortcut: filters survive only between Links and Analytics
export const switchTab = (r: AppRoute, tab: AppTab) => (filtered(r.tab) ? filteredHash(r.id, tab, r) : tabHref(r.id, tab));
