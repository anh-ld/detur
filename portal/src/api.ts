// relative URLs: Go binary serves portal + API on one origin

export interface App {
  id: string;
  name: string;
  apiKeyHash: string;
  iosAppId: string;
  androidPackage: string;
  androidCertFingerprint: string;
  // iOS tap page tries <scheme>://<route> before the App Store
  iosScheme: string;
  matchThreshold: number;
  matchWindowMinutes: number;
  // SDK destinations get detur_link=<key> so the app can tag its device
  tagLinks: boolean;
  // reusable store/web redirect sets; a link copies one when picked
  redirectProfiles?: RedirectProfile[];
  // reusable social preview sets (title/description/image)
  socialPresets?: SocialPreset[];
}

export interface Link {
  id: string;
  appId: string;
  key: string;
  url: string;
  title: string;
  ios: string;
  android: string;
  fallbackUrl: string;
  ogTitle: string;
  ogDescription: string;
  ogImage: string;
  expiresAt: string; // RFC3339 UTC; "" = never
  expiredUrl: string;
  tags?: string[]; // portal labels; server always sends []
  ruleSummary?: string; // list view: rule name + split weights, "" = no rules
  hasSplit?: boolean; // any rule uses weighted variants
}

// one UTC day. clicks = browser clicks; webFallbacks ⊂ clicks; opens = SDK opens of installed app
export interface DayStat {
  day: string;
  clicks: number;
  webFallbacks: number;
  opens: number;
  organic: number;
  nonOrganic: number;
}

export interface Analytics {
  days: DayStat[];
  links: { linkId: string; key: string; title?: string; clicks: number; matches: number }[];
  events: { event: string; count: number }[];
  // browser clicks per in-app source (messenger, zalo, …, unknown-inapp), all platforms
  sources: { source: string; count: number }[];
  // per link, from app-tagged devices, all platforms. Older servers omit both.
  conversions?: { linkId: string; key: string; event: string; count: number }[];
  retention?: LinkRetention[];
}

// retention mark: of devices (cohorts w/ mark day in range), returned = opened app exactly N days after tag
export interface Mark {
  returned: number;
  devices: number;
}

export interface LinkRetention {
  linkId: string;
  key: string;
  devices: number;
  d1: Mark;
  d7: Mark;
  d30: Mark;
}

// Installs per match method ('' = pre-receipt) + 50-pt score buckets.
export interface MatchQuality {
  methods: Record<string, number>;
  buckets: { from: number; matched: number; organic: number }[];
}

// GET /api/apps/{id}/health row.
export interface HealthCheck {
  check: string;
  status: 'ok' | 'warn' | 'fail';
  detail: string;
}

export type FraudMode = 'tagged' | 'active';

// per-app fraud config (server fraud.Settings). Fingerprint: no mode, tag only.
export interface FraudSettings {
  velocityMode: FraudMode;
  timingMode: FraudMode;
  userAgentMode: FraudMode;
  ipMode: FraudMode;
  velocityIpMax: number;
  velocityLinkMax: number;
  velocityWindowMinutes: number;
  timingShortSeconds: number;
  timingLongHours: number;
  fingerprintMax: number;
  fingerprintWindowDays: number;
}

// Flagged install. linkKey/fraudLinkKey '' = organic or link deleted; fraudAction '' = tagged only.
export interface FlaggedInstall {
  id: string;
  createdAt: string;
  attribution: 'organic' | 'non_organic' | 'unknown';
  method: string;
  linkKey: string;
  platform: string;
  fraud: string[];
  fraudAction: '' | 'reattributed' | 'excluded';
  fraudLinkKey: string;
}

// Per-signal counts + latest 100 flagged installs, newest first.
export interface Fraud {
  signals: Record<string, number>;
  installs: FlaggedInstall[];
}

export type Platform = '' | 'ios' | 'android' | 'desktop';

// base URL: '' = same origin; integration tests point at live server
export let apiBase = '';
export function setApiBase(url: string) {
  apiBase = url;
}

// carries status: UI tells 401 (wrong password) from 403 (no/expired admin session) without parsing.
// message = server's text (user-facing); method + path kept for debugging. String(err) = message only.
export class HttpError extends Error {
  status: number;
  request: string;
  constructor(message: string, status: number, request: string) {
    super(message);
    this.status = status;
    this.request = request;
  }
  toString() {
    return this.message;
  }
}

export async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(apiBase + path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    const text = await res.text();
    const msg = text.trim() || res.statusText || `Request failed (${res.status})`;
    throw new HttpError(msg.charAt(0).toUpperCase() + msg.slice(1), res.status, `${method} ${path}`);
  }
  return res.status === 204 ? (undefined as T) : ((await res.json()) as T);
}

export interface CreatedApp {
  id: string;
  name: string;
  apiKey: string;
  apiKeyHash: string;
}

// GET /api/admin/session; expiresAt only when admin
export interface AdminSession {
  admin: boolean;
  expiresAt?: string;
}

// sub-day windows the server accepts (A9)
export const WINDOWS = ['15m', '1h', '3h', '12h'] as const;
export type WindowId = (typeof WINDOWS)[number];
export const isWindow = (v: unknown): v is WindowId => WINDOWS.includes(v as WindowId);

export type DateRangeFilter = {
  days?: number;
  window?: WindowId;
  linkId?: string;
  from?: string;
  to?: string;
};

export const dateRangeQuery = (r: DateRangeFilter | number): string => {
  if (typeof r === 'number') return `days=${r}`;
  const parts: string[] = [];
  if (r.window) parts.push(`window=${r.window}`);
  else if (r.from && r.to) parts.push(`from=${r.from}&to=${r.to}`);
  else parts.push(`days=${r.days ?? 7}`);
  if (r.linkId) parts.push(`link_id=${encodeURIComponent(r.linkId)}`);
  return parts.join('&');
};

// GET /api/apps/{id}/page; html '' = default page in use
export interface AppPage {
  html: string;
  defaultHtml: string;
  placeholders: { name: string; desc: string }[];
}

export interface PagePreview {
  html: string;
  page: 'copy' | 'tap';
  platform: 'ios' | 'android';
}

export const api = {
  getMatchQuality: (id: string) => req<MatchQuality>('GET', `/api/apps/${id}/match-quality?days=30`),
  getHealth: (id: string) => req<HealthCheck[]>('GET', `/api/apps/${id}/health`),
  getConfig: () => req<{ logoutUrl: string; adminSet: boolean; linkBase: string }>('GET', '/api/config'),
  getAdminSession: () => req<AdminSession>('GET', '/api/admin/session'),
  enterAdminMode: (password: string) => req<{ expiresAt: string }>('POST', '/api/admin/session', { password }),
  exitAdminMode: () => req<AdminSession>('DELETE', '/api/admin/session'),
  listApps: () => req<App[]>('GET', '/api/apps'),
  getApp: (id: string) => req<App>('GET', `/api/apps/${id}`),
  createApp: (name: string) => req<CreatedApp>('POST', '/api/apps', { name }),
  rotateKey: (id: string) => req<CreatedApp>('POST', `/api/apps/${id}/rotate-key`),
  revokeKey: (id: string) => req<void>('DELETE', `/api/apps/${id}/key`),
  updateApp: (
    id: string,
    d: {
      iosAppId?: string;
      androidPackage?: string;
      androidCertFingerprint?: string;
      iosScheme?: string;
      redirectProfiles?: RedirectProfile[];
      socialPresets?: SocialPreset[];
    },
  ) => req<App>('PATCH', `/api/apps/${id}`, d),
  deleteApp: (id: string) => req<void>('DELETE', `/api/apps/${id}`),
  getPage: (id: string) => req<AppPage>('GET', `/api/apps/${id}/page`),
  savePage: (id: string, html: string) => req<void>('PUT', `/api/apps/${id}/page`, { html }),
  // preview comes back as text/html, not JSON
  previewPage: async (id: string, p: PagePreview) => {
    const res = await fetch(apiBase + `/api/apps/${id}/page/preview`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(p),
    });
    const text = await res.text();
    if (!res.ok) throw new HttpError(text.trim() || res.statusText, res.status, `POST /api/apps/${id}/page/preview`);
    return text;
  },
  listLinks: (appId: string) => req<Link[]>('GET', `/api/apps/${appId}/links`),
  createLink: (appId: string, l: Omit<Link, 'id' | 'appId'>) => req<Link>('POST', `/api/apps/${appId}/links`, l),
  updateLink: (id: string, l: Partial<Omit<Link, 'id' | 'appId' | 'key'>>) => req<Link>('PATCH', `/api/links/${id}`, l),
  deleteLink: (id: string) => req<void>('DELETE', `/api/links/${id}`),
  getLinkRules: (appId: string, linkId: string) => req<LinkRule[]>('GET', `/api/apps/${appId}/links/${linkId}/rules`),
  setLinkRules: (appId: string, linkId: string, rules: LinkRule[]) =>
    req<LinkRule[]>('PUT', `/api/apps/${appId}/links/${linkId}/rules`, rules),
  getLinkVariantStats: (appId: string, linkId: string, range?: DateRangeFilter | number) =>
    req<VariantStat[]>('GET', `/api/apps/${appId}/links/${linkId}/variants${range ? `?${dateRangeQuery(range)}` : ''}`),
  saveMatching: (id: string, m: { threshold: number; windowMinutes: number }) => req<App>('PATCH', `/api/apps/${id}/matching`, m),
  saveTagging: (id: string, tagLinks: boolean) => req<App>('PATCH', `/api/apps/${id}/tagging`, { tagLinks }),
  getFraud: (appId: string, range: DateRangeFilter | number) => req<Fraud>('GET', `/api/apps/${appId}/fraud?${dateRangeQuery(range)}`),
  getFraudSettings: (appId: string) => req<FraudSettings>('GET', `/api/apps/${appId}/fraud/settings`),
  saveFraudSettings: (appId: string, s: FraudSettings) => req<FraudSettings>('PATCH', `/api/apps/${appId}/fraud/settings`, s),
  getAnalytics: (appId: string, range: DateRangeFilter | number, platform?: Platform, linkId?: string) => {
    const q = dateRangeQuery(range);
    const params = new URLSearchParams(q);
    if (platform) params.set('platform', platform);
    if (linkId) params.set('link_id', linkId);
    return req<Analytics>('GET', `/api/apps/${appId}/analytics?${params.toString()}`);
  },
  listWebhooks: (appId: string) => req<Webhook[]>('GET', `/api/apps/${appId}/webhooks`),
  createWebhook: (appId: string, w: { url: string; types: string[] }) => req<Webhook>('POST', `/api/apps/${appId}/webhooks`, w),
  updateWebhook: (id: string, patch: { url?: string; types?: string[]; enabled?: boolean }) =>
    req<Webhook>('PATCH', `/api/webhooks/${id}`, patch),
  deleteWebhook: (id: string) => req<void>('DELETE', `/api/webhooks/${id}`),
  rotateWebhookSecret: (id: string) => req<{ secret: string }>('POST', `/api/webhooks/${id}/rotate-secret`),
  replayWebhook: (id: string, from: string) => req<{ ok: boolean }>('POST', `/api/webhooks/${id}/replay`, { from }),
  listTokens: () => req<ApiToken[]>('GET', '/api/tokens'),
  createToken: (t: { name: string; appIds: string[] }) => req<CreatedToken>('POST', '/api/tokens', t),
  updateToken: (id: string, t: { name?: string; appIds?: string[] }) => req<ApiToken>('PATCH', `/api/tokens/${id}`, t),
  deleteToken: (id: string) => req<void>('DELETE', `/api/tokens/${id}`),
};

// data-API / MCP bearer token. prefix only after creation; lastUsedAt null = never used
export interface ApiToken {
  id: string;
  name: string;
  prefix: string;
  appIds: string[];
  createdAt: string;
  lastUsedAt: string | null;
}

// POST reply: plaintext token, shown once
export interface CreatedToken extends ApiToken {
  token: string;
}

export type WebhookEventType = 'installs' | 'events' | 'clicks';

export interface Webhook {
  id: string;
  appId: string;
  url: string;
  secret: string;
  types: WebhookEventType[];
  cursorInstalls: number;
  cursorClicks: number;
  cursorEvents: number;
  failsInstalls: number;
  failsClicks: number;
  failsEvents: number;
  backoffInstalls?: string;
  backoffClicks?: string;
  backoffEvents?: string;
  replayUntilInstalls: number;
  replayUntilClicks: number;
  replayUntilEvents: number;
  enabled: boolean;
  createdAt: string;
}

export interface RuleCond {
  platform?: Platform;
  lang?: string;
  param_key?: string;
  param_val?: string;
  from?: string;
  until?: string;
}

export interface RuleTargets {
  destination?: string;
  ios?: string;
  android?: string;
  fallback_url?: string;
}

export interface SocialPreset {
  name: string;
  title?: string;
  description?: string;
  image?: string;
}

export interface RedirectProfile {
  name: string;
  ios?: string;
  android?: string;
  web?: string;
}

export interface SplitVariant {
  name: string;
  weight: number;
  targets: RuleTargets;
}

export interface RuleAction {
  targets?: RuleTargets;
  split?: SplitVariant[];
}

export interface LinkRule {
  id?: string;
  link_id?: string;
  position?: number;
  name: string;
  cond: RuleCond;
  action: RuleAction;
  created_at?: string;
}

export interface VariantStat {
  variant: string;
  clicks: number;
  installs: number;
  conversion_rate: number;
}
