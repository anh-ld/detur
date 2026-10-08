import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { ChildProcess } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { api, setApiBase } from '../src/api';
import { startServer, stopServer, uniq, waitForServer } from './integration';

// Short-link page scripts (safety-net timer, copy page, tap page), fetched from the real Go binary and run against a
// stubbed browser with fake timers. Go tests only see the HTML; these run the code.
const PORT = 8094;
const BASE = `http://127.0.0.1:${PORT}`;
const SDK = 'http://127.0.0.1:8080';
const bin = join(tmpdir(), 'detur-pages');

const SAFARI = 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1';
const CHROME_ANDROID = 'Mozilla/5.0 (Linux; Android 14; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Mobile Safari/537.36';
const WEBVIEW = 'Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36';
const MESSENGER_IOS = 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [FBAN/MessengerForiOS;FBAV/442.0.0.42.110;FBSN/iOS]';
const MESSENGER_ANDROID = 'Mozilla/5.0 (Linux; Android 14; Pixel 7 Build/UP1A.231005.007; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/125.0.0.0 Mobile Safari/537.36 [FB_IAB/MESSENGER;FBAV/450.0.0.42.109;]';

let server: ChildProcess;
let dbDir: string;
let key: string;

beforeAll(async () => {
  const s = startServer({ port: PORT, bin });
  server = s.server;
  dbDir = s.dbDir;
  await waitForServer(`${BASE}/api/apps`, 60_000, server, s.getStderr);
  setApiBase(BASE);
  const app = await api.createApp(uniq('pages-app'));
  key = uniq('pages');
  await api.createLink(app.id, {
    key,
    url: 'https://example.com/p',
    ios: 'https://apps.apple.com/app/id123',
    android: 'https://play.google.com/store/apps/details?id=com.example',
    fallbackUrl: '',
  });
});

afterAll(() => {
  setApiBase('');
  stopServer(server, dbDir, bin);
});

afterEach(() => {
  vi.useRealTimers();
});

async function scripts(query: string, ua: string): Promise<string[]> {
  const res = await fetch(`${SDK}/${key}${query}`, { headers: { 'User-Agent': ua }, redirect: 'manual' });
  expect(res.status).toBe(200);
  return [...(await res.text()).matchAll(/<script>([\s\S]*?)<\/script>/g)].map((m) => m[1]);
}

// A fake browser: records navigations, replaceState, fetches; lets a test fire page events and choose whether the copy works.
function browser(search = '', copyWorks = true) {
  const handlers: Record<string, (() => void)[]> = {};
  const on = (t: string, f: () => void) => (handlers[t] ??= []).push(f);
  const button: { onclick: null | (() => void); click?: () => void; addEventListener: (t: string, f: () => void) => void } = {
    onclick: null,
    addEventListener: (_t, f) => (button.click = f),
  };
  const b = {
    replaced: [] as string[],
    states: [] as string[],
    fetched: [] as string[],
    doc: {
      visibilityState: 'visible',
      addEventListener: on,
      getElementById: () => button,
      createElement: () => ({ value: '', style: {}, setAttribute() {}, select() {}, setSelectionRange() {} }),
      body: { appendChild() {}, removeChild() {} },
      execCommand: () => copyWorks,
    },
    button,
    fire: (t: string) => (handlers[t] ?? []).forEach((f) => f()),
    run(src: string) {
      const location = { search, pathname: `/${key}`, origin: 'https://links.example', replace: (u: string) => b.replaced.push(u) };
      const window = { addEventListener: on, devicePixelRatio: 3 };
      const history = { replaceState: (_s: unknown, _t: string, u: string) => b.states.push(u) };
      const navigator = { clipboard: { writeText: () => Promise.reject(new Error('denied')) } };
      const fetch = (u: string) => (b.fetched.push(u), Promise.resolve());
      new Function('document', 'window', 'location', 'history', 'navigator', 'fetch', 'screen', src)(
        b.doc, window, location, history, navigator, fetch, { width: 390, height: 844 },
      );
    },
  };
  return b;
}

const params = (u: string) => new URLSearchParams(u.slice(u.indexOf('?')));

describe('hop-1 safety net', () => {
  it('generic webview: reloads to hop 2 at once, then to the tap page when still visible after 1.5s', async () => {
    const [src] = await scripts('', WEBVIEW);
    vi.useFakeTimers();
    const b = browser();
    b.run(src);
    expect(b.replaced).toHaveLength(1);
    expect(params(b.replaced[0]).get('_dt')).toBe('1');
    expect(params(b.replaced[0]).has('_tap')).toBe(false);
    vi.advanceTimersByTime(1499);
    expect(b.replaced).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(b.replaced).toHaveLength(2);
    expect(params(b.replaced[1]).get('_tap')).toBe('1');
  });

  it('cancels once the page is hidden or unloaded (the store opened)', async () => {
    const [src] = await scripts('', WEBVIEW);
    for (const event of ['pagehide', 'visibilitychange']) {
      vi.useFakeTimers();
      const b = browser();
      b.run(src);
      b.doc.visibilityState = 'hidden';
      b.fire(event);
      vi.advanceTimersByTime(5000);
      expect(b.replaced, event).toHaveLength(1);
      vi.useRealTimers();
    }
  });

  it('skips a timer that fires late (frozen in the background, user came back)', async () => {
    const [src] = await scripts('', WEBVIEW);
    vi.useFakeTimers();
    const b = browser();
    b.run(src);
    vi.setSystemTime(Date.now() + 5000);
    vi.advanceTimersByTime(1500);
    expect(b.replaced).toHaveLength(1);
  });

  it('real browser waits 4s; recognized in-app arms no timer', async () => {
    const [realSrc] = await scripts('', CHROME_ANDROID);
    const [namedSrc] = await scripts('', MESSENGER_ANDROID);
    vi.useFakeTimers();
    const real = browser();
    real.run(realSrc);
    vi.advanceTimersByTime(3999);
    expect(real.replaced).toHaveLength(1);
    vi.advanceTimersByTime(1);
    expect(real.replaced).toHaveLength(2);

    const named = browser();
    named.run(namedSrc);
    vi.advanceTimersByTime(60_000);
    expect(named.replaced).toHaveLength(1);
  });
});

describe('iOS copy page', () => {
  it('one tap copies, then reloads with pasted_link; a failed copy reloads without it', async () => {
    const [src] = await scripts('', SAFARI);
    for (const copyWorks of [true, false]) {
      vi.useFakeTimers();
      const b = browser('', copyWorks);
      b.run(src);
      expect(b.replaced).toHaveLength(0); // waits for the tap
      b.button.onclick!();
      expect(b.replaced).toHaveLength(1);
      expect(params(b.replaced[0]).get('pasted_link')).toBe(copyWorks ? `https://links.example/${key}` : null);
      vi.useRealTimers();
    }
  });
});

describe('tap page', () => {
  it('drops _tap from its own URL so "Open in browser" reopens the plain link', async () => {
    const [strip] = await scripts('?_dt=1&_tap=1', MESSENGER_IOS);
    const b = browser('?_dt=1&screen=390x844%403&_tap=1');
    b.run(strip);
    expect(b.states).toHaveLength(1);
    expect(params(b.states[0]).has('_tap')).toBe(false);
    expect(params(b.states[0]).get('_dt')).toBe('1');
  });

  it('iOS tap reports pasted_link only when the copy worked', async () => {
    const [, ios] = await scripts('?_dt=1', MESSENGER_IOS);
    for (const copyWorks of [true, false]) {
      const b = browser('?_dt=1', copyWorks);
      b.run(ios);
      b.button.click!();
      expect(b.fetched.map((u) => params(u).get('pasted_link'))).toEqual(copyWorks ? [`https://links.example/${key}`] : []);
    }
  });
});
