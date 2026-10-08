// Portal API client. Relative URLs: Go binary serves portal + API on one origin.

export interface App {
  id: string;
  name: string;
  apiKeyHash: string;
  iosAppId: string;
  androidPackage: string;
  androidCertFingerprint: string;
  matchThreshold: number;
  matchWindowMinutes: number;
}

export interface Link {
  id: string;
  appId: string;
  key: string;
  url: string;
  ios: string;
  android: string;
  fallbackUrl: string;
}

// One UTC day. clicks = browser clicks; webFallbacks ⊂ clicks; opens = SDK opens of an installed app.
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
  links: { linkId: string; key: string; clicks: number; matches: number }[];
  events: { event: string; count: number }[];
  // Browser clicks per in-app source (messenger, zalo, …, unknown-inapp); all platforms.
  sources: { source: string; count: number }[];
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

// Per-app fraud config (server fraud.Settings). Fingerprint has no mode: tag only.
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

// Base URL override: '' = same origin; integration tests point client at live server.
export let apiBase = '';
export function setApiBase(url: string) {
  apiBase = url;
}

// HTTP error carrying the status: the UI distinguishes 401 (wrong password) from
// 403 (missing/expired admin session) without parsing the message.
export class HttpError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
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
    throw new HttpError(`${method} ${path}: ${res.status} ${text.trim() || res.statusText}`, res.status);
  }
  return res.status === 204 ? (undefined as T) : ((await res.json()) as T);
}

export interface CreatedApp {
  id: string;
  name: string;
  apiKey: string;
  apiKeyHash: string;
}

// Admin elevation session (GET /api/admin/session); expiresAt present only when admin.
export interface AdminSession {
  admin: boolean;
  expiresAt?: string;
}

export const api = {
  getMatchQuality: (id: string) => req<MatchQuality>('GET', `/api/apps/${id}/match-quality?days=30`),
  getHealth: (id: string) => req<HealthCheck[]>('GET', `/api/apps/${id}/health`),
  getConfig: () => req<{ logoutUrl: string; adminSet: boolean }>('GET', '/api/config'),
  getAdminSession: () => req<AdminSession>('GET', '/api/admin/session'),
  enterAdminMode: (password: string) => req<{ expiresAt: string }>('POST', '/api/admin/session', { password }),
  exitAdminMode: () => req<AdminSession>('DELETE', '/api/admin/session'),
  listApps: () => req<App[]>('GET', '/api/apps'),
  getApp: (id: string) => req<App>('GET', `/api/apps/${id}`),
  createApp: (name: string) => req<CreatedApp>('POST', '/api/apps', { name }),
  rotateKey: (id: string) => req<CreatedApp>('POST', `/api/apps/${id}/rotate-key`),
  revokeKey: (id: string) => req<void>('DELETE', `/api/apps/${id}/key`),
  updateApp: (id: string, d: { iosAppId: string; androidPackage: string; androidCertFingerprint: string }) =>
    req<App>('PATCH', `/api/apps/${id}`, d),
  deleteApp: (id: string) => req<void>('DELETE', `/api/apps/${id}`),
  listLinks: (appId: string) => req<Link[]>('GET', `/api/apps/${appId}/links`),
  createLink: (appId: string, l: Omit<Link, 'id' | 'appId'>) =>
    req<Link>('POST', `/api/apps/${appId}/links`, l),
  updateLink: (id: string, l: Partial<Omit<Link, 'id' | 'appId' | 'key'>>) =>
    req<Link>('PATCH', `/api/links/${id}`, l),
  deleteLink: (id: string) => req<void>('DELETE', `/api/links/${id}`),
  saveMatching: (id: string, m: { threshold: number; windowMinutes: number }) =>
    req<App>('PATCH', `/api/apps/${id}/matching`, m),
  getFraud: (appId: string, days: number) => req<Fraud>('GET', `/api/apps/${appId}/fraud?days=${days}`),
  getFraudSettings: (appId: string) => req<FraudSettings>('GET', `/api/apps/${appId}/fraud/settings`),
  saveFraudSettings: (appId: string, s: FraudSettings) =>
    req<FraudSettings>('PATCH', `/api/apps/${appId}/fraud/settings`, s),
  getAnalytics: (appId: string, days: number, platform: Platform) =>
    req<Analytics>('GET', `/api/apps/${appId}/analytics?days=${days}${platform ? `&platform=${platform}` : ''}`),
  listWebhooks: (appId: string) => req<Webhook[]>('GET', `/api/apps/${appId}/webhooks`),
  createWebhook: (appId: string, w: { url: string; types: string[] }) =>
    req<Webhook>('POST', `/api/apps/${appId}/webhooks`, w),
  updateWebhook: (id: string, patch: { url?: string; types?: string[]; enabled?: boolean }) =>
    req<Webhook>('PATCH', `/api/webhooks/${id}`, patch),
  deleteWebhook: (id: string) => req<void>('DELETE', `/api/webhooks/${id}`),
  rotateWebhookSecret: (id: string) => req<{ secret: string }>('POST', `/api/webhooks/${id}/rotate-secret`),
  replayWebhook: (id: string, from: string) => req<{ ok: boolean }>('POST', `/api/webhooks/${id}/replay`, { from }),
};

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