import { fetch as nodeFetch } from 'undici';

// happy-dom fetch enforces CORS; server same-origin by design. Use Node's real fetch.
// Minimal cookie jar on top (undici 8.11.2 ships no CookieJar class): stores every
// Set-Cookie per host and sends it back as a Cookie header, so the detur_admin
// elevation session round-trips across fetches in the integration suites.
const jar = new Map<string, Map<string, string>>();

const hostOf = (input: unknown): string => {
  const url = typeof input === 'string' ? new URL(input) : input instanceof URL ? input : new URL((input as Request).url);
  return url.host;
};

// Test helpers: inject a cookie (e.g. a tampered/expired session) or clear the jar.
// Injecting locks the jar: no late Set-Cookie may clobber the injected value.
let jarLocked = false;
export function setJarCookie(host: string, name: string, value: string) {
  jarLocked = true;
  if (!jar.has(host)) jar.set(host, new Map());
  jar.get(host)!.set(name, value);
}
export function clearJar() {
  jarLocked = false;
  jar.clear();
}

globalThis.fetch = (async (input: any, init?: any) => {
  const host = hostOf(input);
  const cookies = jar.get(host);
  if (cookies && cookies.size > 0) {
    init = { ...init, headers: { ...(init?.headers ?? {}), Cookie: [...cookies].map(([n, v]) => `${n}=${v}`).join('; ') } };
  }
  const res = await nodeFetch(input, init);
  if (jarLocked) return res;
  for (const sc of res.headers.getSetCookie()) {
    const m = sc.match(/^([^=;]+)=([^;]*)/);
    if (!m) continue;
    const [name, value] = [m[1], m[2]];
    if (value === '' && /Max-Age=0/i.test(sc)) {
      cookies?.delete(name); // deletion cookie (exit): drop from the jar
      if (cookies?.size === 0) jar.delete(host);
      continue;
    }
    if (!jar.has(host)) jar.set(host, new Map());
    jar.get(host)!.set(name, value);
  }
  return res;
}) as unknown as typeof fetch;

// Platform shims happy-dom lacks: clipboard for copy flows, scrollTo for shell.
Object.defineProperty(navigator, 'clipboard', { value: { writeText: async () => {} }, configurable: true });
window.scrollTo = (() => {}) as typeof window.scrollTo;