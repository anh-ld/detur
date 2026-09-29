// Portal API client. Relative URLs: Go binary serves portal + API on one origin.

export interface App {
  id: string;
  name: string;
  apiKeyHash: string;
  iosAppId: string;
  androidPackage: string;
  androidCertFingerprint: string;
}

export interface Link {
  id: string;
  appId: string;
  key: string;
  url: string;
  ios: string;
  android: string;
  fallbackUrl: string;
  threshold: number;
  windowMinutes: number;
}

export interface Readout {
  clicks: number;
  organic: number;
  nonOrganic: number;
}

export interface Settings {
  threshold: number;
  windowMinutes: number;
}

// Base URL override: '' = same origin; integration tests point client at live server.
export let apiBase = '';
export function setApiBase(url: string) {
  apiBase = url;
}

export async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(apiBase + path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${method} ${path}: ${res.status} ${text.trim() || res.statusText}`);
  }
  return res.status === 204 ? (undefined as T) : ((await res.json()) as T);
}

export interface CreatedApp {
  id: string;
  name: string;
  apiKey: string;
  apiKeyHash: string;
}

export const api = {
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
  getSettings: () => req<Settings>('GET', '/api/settings'),
  saveSettings: (s: Settings) => req<Settings>('PATCH', '/api/settings', s),
  getReadout: (appId: string) => req<Readout>('GET', `/api/apps/${appId}/readout`),
};