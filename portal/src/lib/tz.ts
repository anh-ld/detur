import { useEffect, useState } from 'preact/hooks';

// Display-only. Server buckets stay UTC days. Value: '0' (UTC) or 'local' (browser).
const KEY = 'detur.tz';

const read = () => {
  try {
    return localStorage.getItem(KEY) ?? '0';
  } catch {
    return '0';
  }
};
let cur = read();
const subs = new Set<() => void>();

export function setTz(v: string) {
  cur = v;
  try {
    localStorage.setItem(KEY, v);
  } catch {}
  subs.forEach((s) => s());
}
export function useTz() {
  const [, bump] = useState(0);
  useEffect(() => {
    const f = () => bump((n) => n + 1);
    subs.add(f);
    return () => void subs.delete(f);
  }, []);
  return cur;
}

const offset = (d: Date) => (cur === 'local' ? -d.getTimezoneOffset() : 0);
const fmtWith = (iso: string, o: Intl.DateTimeFormatOptions) => {
  const d = new Date(iso);
  return new Date(d.getTime() + offset(d) * 60000).toLocaleString('en', { ...o, timeZone: 'UTC' });
};

export const fmtTzTime = (iso: string) => fmtWith(iso, { hour: '2-digit', minute: '2-digit', hour12: false });
export const fmtTzDateTime = (iso: string) => fmtWith(iso, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
export const fmtTzDate = (iso: string) => fmtWith(iso, { month: 'short', day: 'numeric', year: 'numeric' });
