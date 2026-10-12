import { fetch as nodeFetch } from 'undici';

// happy-dom fetch enforces CORS; server is same-origin. Use Node's real fetch.
// + minimal cookie jar (undici 8.11.2 has no CookieJar): Set-Cookie per host -> Cookie header,
// so the detur_admin session round-trips in integration suites.
const jar = new Map<string, Map<string, string>>();

const hostOf = (input: unknown): string => {
  const url = typeof input === 'string' ? new URL(input) : input instanceof URL ? input : new URL((input as Request).url);
  return url.host;
};

// inject a cookie (tampered/expired session) or clear the jar.
// injecting locks the jar: late Set-Cookie can't clobber it.
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
      cookies?.delete(name); // deletion cookie (exit): drop
      if (cookies?.size === 0) jar.delete(host);
      continue;
    }
    if (!jar.has(host)) jar.set(host, new Map());
    jar.get(host)!.set(name, value);
  }
  return res;
}) as unknown as typeof fetch;

// shims happy-dom lacks: clipboard (copy flows), scrollTo (shell)
Object.defineProperty(navigator, 'clipboard', { value: { writeText: async () => {} }, configurable: true });
window.scrollTo = (() => {}) as typeof window.scrollTo;