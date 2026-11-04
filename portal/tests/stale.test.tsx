import { afterEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/preact';
import { AppPage } from '../src/detail';
import { AnalyticsView } from '../src/analytics';
import { CopyButton, getAdmin, refreshAdminSession, setElevated } from '../src/ui';

// unit, mocked fetch: cases the real binary can't produce on demand (delayed reply, every outcome/signal at once)

const json = (body: unknown) => Promise.resolve(new Response(JSON.stringify(body), { status: 200 }));
const stats = (clicks: number) => ({
  days: [{ day: '2026-10-04', clicks, webFallbacks: 0, opens: 0, organic: 0, nonOrganic: 0 }],
  links: [],
  events: [],
});
const app = {
  id: 'app1', name: 'Stale App', apiKeyHash: 'h', iosAppId: '', androidPackage: '',
  androidCertFingerprint: '', iosScheme: '', matchThreshold: 850, matchWindowMinutes: 15,
};

// Clicks tile value ('' while skeleton up)
const clicksTileText = () =>
  screen
    .getAllByText('Clicks', { exact: true })
    .map((e) => e.closest('[k=card][padding=sm]'))
    .find(Boolean)!
    .querySelector(':scope > div')!.textContent!;
const clicksTile = () => parseInt(clicksTileText(), 10);

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it('stale analytics response: a slow 7-day reply after switching to 30 days is dropped', async () => {
  let releaseSlow!: () => void;
  vi.stubGlobal('fetch', (url: string) => {
    if (url.includes('/analytics?days=7'))
      return new Promise<Response>((r) => (releaseSlow = () => r(new Response(JSON.stringify(stats(7))))));
    if (url.includes('/analytics?days=30')) return json(stats(30));
    if (url.endsWith('/links')) return json([]);
    return json(app);
  });

  render(<AppPage id="app1" tab="analytics" />);
  fireEvent.change(await screen.findByLabelText('Date range'), { target: { value: '30' } });
  await waitFor(() => expect(clicksTile()).toBe(30));

  releaseSlow();
  await new Promise((r) => setTimeout(r, 50));
  expect(clicksTile()).toBe(30);
});

const settings = {
  velocityMode: 'tagged', timingMode: 'tagged', userAgentMode: 'tagged', ipMode: 'tagged',
  velocityIpMax: 20, velocityLinkMax: 500, velocityWindowMinutes: 60,
  timingShortSeconds: 10, timingLongHours: 24, fingerprintMax: 5, fingerprintWindowDays: 7,
};
const flagged = (linkKey: string) => ({
  signals: { timing: 1 },
  installs: [{
    id: linkKey, createdAt: '2026-10-04T10:00:00Z', attribution: 'non_organic', method: 'ip', linkKey,
    platform: 'ios', fraud: ['timing'], fraudAction: '', fraudLinkKey: '',
  }],
});

it('stale flagged installs response: a slow 7-day reply after switching to 30 days is dropped', async () => {
  let releaseSlow!: () => void;
  vi.stubGlobal('fetch', (url: string) => {
    if (url.includes('/fraud?days=7'))
      return new Promise<Response>((r) => (releaseSlow = () => r(new Response(JSON.stringify(flagged('week-link'))))));
    if (url.includes('/fraud?days=30')) return json(flagged('month-link'));
    if (url.endsWith('/fraud/settings')) return json(settings);
    if (url.includes('/analytics?')) return json(stats(0));
    if (url.endsWith('/links')) return json([]);
    return json(app);
  });

  render(<AppPage id="app1" tab="fraud" />);
  fireEvent.change(await screen.findByLabelText('Flagged range'), { target: { value: '30' } });
  await screen.findByText('month-link');

  releaseSlow();
  await new Promise((r) => setTimeout(r, 50));
  screen.getByText('month-link');
  expect(screen.queryByText('week-link')).toBeNull();
});

const install = (o: Record<string, unknown>) => ({
  createdAt: '2026-10-04T10:00:00Z', attribution: 'organic', method: '', linkKey: '', platform: 'ios',
  fraud: [], fraudAction: '', fraudLinkKey: '', ...o,
});

it('flagged installs table: per-signal counts, signal names and outcome wording for every case', async () => {
  vi.stubGlobal('fetch', (url: string) => {
    if (url.includes('/fraud?days=7'))
      return json({
        signals: { velocity: 1, timing: 2, user_agent: 3, ip: 4, install_ip: 5, fingerprint: 6 },
        installs: [
          install({ id: 'r', linkKey: 'moved-to', attribution: 'non_organic', fraud: ['velocity'], fraudAction: 'reattributed', fraudLinkKey: 'flood-link' }),
          install({ id: 'e', fraud: ['timing', 'install_ip'], fraudAction: 'excluded', fraudLinkKey: 'fast-link' }),
          install({ id: 'g', fraud: ['ip'], fraudAction: 'excluded' }), // flagged click's link since deleted
          install({ id: 'a', linkKey: 'tagged-link', attribution: 'non_organic', fraud: ['user_agent', 'fingerprint'] }),
          install({ id: 'o', fraud: ['new_signal'] }),
        ],
      });
    if (url.endsWith('/fraud/settings')) return json(settings);
    if (url.includes('/analytics?')) return json(stats(0));
    if (url.endsWith('/links')) return json([]);
    return json(app);
  });

  render(<AppPage id="app1" tab="fraud" />);
  const section = await screen.findByRole('region', { name: 'Flagged installs' });
  await within(section).findByText('moved-to');

  const dl = within(section).getByLabelText('Flagged installs per signal');
  expect(Array.from(dl.querySelectorAll('dt')).map((dt) => [dt.textContent, dt.nextElementSibling!.textContent])).toEqual([
    ['Click flooding', '1'], ['Suspicious timing', '2'], ['Bot traffic', '3'],
    ['Datacenter IP', '4'], ['Datacenter install IP', '5'], ['Repeated device', '6'],
  ]);

  const rows = within(section).getAllByRole('row').slice(1).map((r) => ({
    link: r.querySelector('[data-label="Link"]')!.textContent,
    signals: Array.from(r.querySelectorAll('[data-label="Signals"] [k=badge]')).map((b) => b.textContent),
    outcome: Array.from(r.querySelector('[data-label="Outcome"] > div')!.childNodes).map((n) => n.textContent).filter(Boolean),
  }));
  expect(rows).toEqual([
    { link: 'moved-to', signals: ['Click flooding'], outcome: ['Credit moved to another click', 'Flagged click on flood-link'] },
    { link: '—', signals: ['Suspicious timing', 'Datacenter install IP'], outcome: ['Kept as organic', 'Flagged click on fast-link'] },
    { link: '—', signals: ['Datacenter IP'], outcome: ['Kept as organic'] },
    { link: 'tagged-link', signals: ['Bot traffic', 'Repeated device'], outcome: ['Attributed, tagged only'] },
    { link: '—', signals: ['new_signal'], outcome: ['Organic, tagged only'] },
  ]);
});

it('switching apps: the old app, its links and its numbers never show under the new id', async () => {
  const appB = { ...app, id: 'appB', name: 'Other App' };
  const pending: Record<string, (r: Response) => void> = {};
  const hold = (k: string) => new Promise<Response>((r) => (pending[k] = r));
  const release = (k: string, body: unknown) => pending[k](new Response(JSON.stringify(body)));
  vi.stubGlobal('fetch', (url: string) => {
    if (url.includes('/apps/appB/analytics')) return hold('stats');
    if (url.endsWith('/apps/appB/links')) return hold('links');
    if (url.endsWith('/apps/appB')) return hold('app');
    if (url.includes('/analytics?')) return json({ ...stats(7), links: [{ linkId: 'l1', key: 'old-link', clicks: 7, matches: 0 }] });
    if (url.endsWith('/links')) return json([{ id: 'l1', appId: 'app1', key: 'old-link', url: '/home', ios: '', android: '', fallbackUrl: '' }]);
    return json(app);
  });

  const { rerender } = render(<AppPage id="app1" tab="analytics" />);
  await waitFor(() => expect(clicksTile()).toBe(7));

  rerender(<AppPage id="appB" tab="analytics" />);
  await waitFor(() => expect(screen.queryByRole('heading', { name: 'Stale App' })).toBeNull());

  release('app', appB);
  await screen.findByRole('heading', { name: 'Other App' });
  expect(clicksTileText()).toBe('');
  release('stats', stats(3));
  await waitFor(() => expect(clicksTile()).toBe(3));

  rerender(<AppPage id="appB" tab="links" />);
  await new Promise((r) => setTimeout(r, 50));
  expect(screen.queryByText('old-link')).toBeNull();
  release('links', []);
  await screen.findByText('No links yet');
});

it('switching apps mid-load: a slow reply for the app left behind never lands under the new id', async () => {
  const appB = { ...app, id: 'appB', name: 'Other App' };
  let releaseA!: () => void;
  vi.stubGlobal('fetch', (url: string) => {
    if (url.endsWith('/apps/app1')) return new Promise<Response>((r) => (releaseA = () => r(new Response(JSON.stringify(app)))));
    if (url.includes('/analytics?')) return json(stats(1));
    if (url.endsWith('/links')) return json([]);
    return json(appB);
  });

  const { rerender } = render(<AppPage id="app1" tab="analytics" />);
  rerender(<AppPage id="appB" tab="analytics" />);
  await screen.findByRole('heading', { name: 'Other App' });

  releaseA();
  await new Promise((r) => setTimeout(r, 50));
  expect(screen.queryByRole('heading', { name: 'Stale App' })).toBeNull();
  expect(screen.getByRole('heading', { name: 'Other App' })).toBeTruthy();
});

it('analytics error: a rejected range shows the alert; the next good range clears it, and so does switching apps', async () => {
  vi.stubGlobal('fetch', (url: string) => {
    if (url.includes('/analytics?days=7')) return Promise.resolve(new Response('Date range cannot exceed 730 days', { status: 400 }));
    if (url.includes('/analytics?')) return json(stats(30));
    if (url.endsWith('/links')) return json([]);
    return json(app);
  });
  const { rerender } = render(<AppPage id="app1" tab="analytics" />);
  await screen.findByText(/730 days/);
  fireEvent.change(await screen.findByLabelText('Date range'), { target: { value: '30' } });
  await waitFor(() => expect(screen.queryByText(/730 days/)).toBeNull());

  fireEvent.change(screen.getByLabelText('Date range'), { target: { value: '7' } });
  await screen.findByText(/730 days/);
  rerender(<AppPage id="app2" tab="settings" />);
  await screen.findByRole('heading', { name: 'API key' }); // past the loading screen, page rendered
  expect(screen.queryByText(/730 days/)).toBeNull();
});

const day = (i: number) => ({ day: `2026-10-${String(i + 1).padStart(2, '0')}`, clicks: i, webFallbacks: 0, opens: 0, organic: 0, nonOrganic: 0 });
const days = (n: number) => ({ days: Array.from({ length: n }, (_, i) => day(i)), links: [], events: [], sources: [] });

it('chart hover: range shrinks under a resting pointer -> no crash, tooltip gone', async () => {
  const { rerender, container } = render(<AnalyticsView data={days(30)} />);
  const targets = container.querySelectorAll('.chart-hover');
  fireEvent.pointerEnter(targets[targets.length - 1]);
  await waitFor(() => expect(container.querySelector('.chart-tip')).not.toBeNull());
  rerender(<AnalyticsView data={days(7)} />);
  expect(container.querySelector('.chart-tip')).toBeNull();
});

it('copy without navigator.clipboard (plain-HTTP portal): execCommand copies; a failure says so and a later success clears it', async () => {
  vi.stubGlobal('navigator', { ...navigator, clipboard: undefined });
  let ok = false;
  const exec = vi.fn(() => ok);
  (document as unknown as { execCommand: typeof exec }).execCommand = exec;
  render(<CopyButton value="dk_secret" label="Copy API key" />);
  fireEvent.click(screen.getByRole('button', { name: 'Copy API key' }));
  await screen.findByText('Copy failed: select and copy');
  ok = true;
  fireEvent.click(screen.getByRole('button'));
  await screen.findByText('Copied');
  expect(exec).toHaveBeenCalledWith('copy');
  await waitFor(() => expect(screen.getByRole('button').textContent).toBe('Copy API key'), { timeout: 3000 });
});

it('admin session: a slow session read that started before unlock never clears the fresh elevation', async () => {
  let release!: () => void;
  vi.stubGlobal('fetch', () => new Promise<Response>((r) => (release = () => r(new Response(JSON.stringify({ admin: false }))))));
  const pending = refreshAdminSession(); // mount-time check, still in flight
  setElevated(true, new Date(Date.now() + 3600_000).toISOString()); // unlock lands first
  release();
  await pending;
  expect(getAdmin()).toBe(true);
  setElevated(false);
});
