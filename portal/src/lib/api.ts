// relative URLs: Go binary serves portal + API on one origin
import type {
  AdminSession,
  Analytics,
  ApiToken,
  App,
  AppPage,
  CreatedApp,
  CreatedToken,
  DayStat,
  FlaggedInstall,
  Fraud,
  FraudSettings,
  HealthCheck,
  Link,
  LinkRetention,
  LinkRule,
  Mark,
  MatchQuality,
  PagePreview,
  Platform,
  RedirectProfile,
  RuleAction,
  RuleCond,
  RuleTargets,
  SocialPreset,
  SplitVariant,
  VariantStat,
  Webhook,
} from './types';
import { dateRangeQuery, type DateRangeFilter } from './range';

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
