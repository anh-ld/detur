import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { ChildProcess } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/preact';
import { api, setApiBase } from '../src/api';
import { App } from '../src/app';
import { AppsPage } from '../src/apps';
import { AppPage } from '../src/detail';
import { go, openDialog, selectedTab, startServer, stopServer, tabs, uniq, waitForServer } from './integration';

// Client-flow integration: real pages vs real Go binary + fresh SQLite. SDK :8080 hardcoded (must be free); portal on :8091.
const PORT = 8091;
const BASE = `http://127.0.0.1:${PORT}`;
const bin = join(tmpdir(), 'detur-integ');

let server: ChildProcess;
let dbDir: string;
let getStderr: () => string = () => '';

// The same binary serves short links and the SDK API on :8080.
const SDK = 'http://127.0.0.1:8080';
const IPHONE = 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1';
// A second iPhone on the same iOS version behind the same IP (its own UA, so the server doesn't merge it into the first click).
const IPHONE_B = IPHONE.replace('15E148', '15E149');
const MESSENGER = 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [FBAN/MessengerForiOS;FBAV/442.0.0.42.110;FBSN/iOS]';
const DESKTOP = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36';

async function click(key: string, ua: string, done = false): Promise<Response> {
  return fetch(`${SDK}/${key}${done ? '?_dt=1' : ''}`, { headers: { 'User-Agent': ua }, redirect: 'manual' });
}

// First SDK launch on an iPhone that clicked: same IP + iOS version, so it matches the latest click. The UA also makes it a distinct device.
async function matchLink(app: { id: string; apiKey: string }, userAgent = IPHONE): Promise<Response> {
  return fetch(`${SDK}/api/link/match-link`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${app.apiKey}`, 'X-App-ID': app.id, 'X-SDK': 'react-native/2.3.1' },
    body: JSON.stringify({ platform: 'ios', model: 'iPhone', systemVersion: '17.5', timezone: 'UTC', userAgent }),
  });
}

// Toasts on screen (kinu ToastContainer, mounted by App): id is monotonic, so "no new toast" = no id above the last one seen.
const toasts = () =>
  Array.from(document.querySelectorAll('[k=toast]')).map((t) => ({
    id: Number(t.getAttribute('data-toast')),
    title: t.querySelector('[k=toast-title]')?.textContent ?? '',
    text: t.querySelector('[k=toast-content]')!.textContent!,
  }));
const lastToastId = () => Math.max(-1, ...toasts().map((t) => t.id));
const findToast = (text: string | RegExp) =>
  waitFor(() => {
    const t = toasts().find((x) => (typeof text === 'string' ? x.text === text : text.test(x.text)));
    if (!t) throw new Error(`no toast ${text}; have ${JSON.stringify(toasts())}`);
    return t;
  });
const settle = () => new Promise((r) => setTimeout(r, 150));

const modeGroup = (label: string) => screen.getByRole('group', { name: `${label} mode` });
const pressed = (label: string) =>
  within(modeGroup(label))
    .getAllByRole('button')
    .filter((b) => b.getAttribute('aria-pressed') === 'true')
    .map((b) => b.textContent);
const threshold = (label: string) => screen.getByLabelText(label) as HTMLInputElement;
// Type like a person: the page has painted (preact flushes mount effects after a frame), the keystroke renders, then focus leaves.
const tick = () => new Promise((r) => setTimeout(r, 0));
async function editAndBlur(label: string, value: string) {
  const el = threshold(label);
  el.focus();
  await settle();
  fireEvent.input(el, { target: { value } });
  await tick();
  el.blur();
}
const flaggedCounts = (section: HTMLElement): Record<string, number> => {
  const dl = section.querySelector('dl[aria-label="Flagged installs per signal"]');
  if (!dl) throw new Error('no per-signal counts');
  return Object.fromEntries(
    Array.from(dl.querySelectorAll('dt')).map((dt) => [dt.textContent, Number(dt.nextElementSibling!.textContent)]),
  );
};

// Analytics tile number by label (labels repeat in chart legends; tiles are the padding=sm cards). First text node: some tiles append a share.
const tileValue = (label: string): number => {
  const tile = screen
    .getAllByText(label, { exact: true })
    .map((e) => e.closest('[k=card][padding=sm]'))
    .find(Boolean)!;
  return parseInt(tile.querySelector(':scope > div')!.firstChild!.textContent!, 10);
};

beforeAll(async () => {
  const s = startServer({ port: PORT, bin });
  server = s.server;
  dbDir = s.dbDir;
  getStderr = s.getStderr;
  await waitForServer(`${BASE}/api/apps`, 60_000, server, getStderr);
  setApiBase(BASE);
});

afterAll(() => {
  setApiBase('');
  stopServer(server, dbDir, bin);
});

afterEach(cleanup);

describe('portal client flows', () => {
  it('apps flow: create app through the dialog, row shows bare id', async () => {
    const name = uniq('flow-app');
    render(<AppsPage />);
    await screen.findByText('No apps yet');

    fireEvent.click(screen.getByRole('button', { name: 'Create app' }));
    fireEvent.input(within(openDialog()).getByLabelText('Name'), { target: { value: name } });
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Create', exact: true }));

    // show-once panel: plaintext key + app id
    await screen.findByText('App created');
    const dlg = openDialog();
    const keyInput = within(dlg).getByDisplayValue(/^dk_/) as HTMLInputElement;
    expect(keyInput.value).toMatch(/^dk_[A-Za-z0-9]{22}$/);
    const appId = (within(dlg).getByDisplayValue(/^[A-Za-z0-9]{21}$/) as HTMLInputElement).value;
    within(dlg).getByRole('button', { name: 'Copy API key' });

    // copy feedback: ghost button flashes "Copied"
    fireEvent.click(within(dlg).getByRole('button', { name: 'Copy API key' }));
    await waitFor(() => within(dlg).getByRole('button', { name: 'Copied' }));

    fireEvent.click(within(dlg).getByRole('button', { name: 'Done' }));

    // row: bare id, no key or copy, no Edit (config lives on the settings page)
    const card = (await screen.findByText(name)).closest('tr')!;
    within(card).getByText(appId, { exact: true });
    expect(within(card).queryByRole('button', { name: /^Copy/ })).toBeNull();
    expect(within(card).queryByRole('button', { name: 'Edit' })).toBeNull();
    expect((await api.listApps()).some((a) => a.id === appId)).toBe(true);
    await api.deleteApp(appId);
  });

  it('detail flow: Links tab creates a link, then deletes it', async () => {
    const app = await api.createApp(uniq('detail-app'));
    render(<AppPage id={app.id} tab="links" />);
    await screen.findByRole('heading', { name: app.name });

    fireEvent.click(screen.getByRole('button', { name: 'New link' }));
    fireEvent.input(within(openDialog()).getByLabelText('Key'), { target: { value: 'summer-sale' } });
    fireEvent.input(within(openDialog()).getByLabelText('Destination URL'), {
      target: { value: 'https://example.com/promo' },
    });
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Create', exact: true }));

    const row = (await screen.findByText('summer-sale')).closest('tr')!;
    expect(row.innerText).toContain('https://example.com/promo');
    expect((await api.listLinks(app.id)).length).toBe(1);

    fireEvent.click(within(row).getByRole('button', { name: 'Delete' }));
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(screen.queryByText('summer-sale')).toBeNull());
    expect(await api.listLinks(app.id)).toHaveLength(0);
    await api.deleteApp(app.id);
  });

  it('analytics flow: real clicks and SDK events reach tiles and top events; the platform filter narrows tiles and Links click/match counts', async () => {
    const app = await api.createApp(uniq('stats-app'));
    const key = uniq('promo');
    await api.createLink(app.id, {
      key,
      url: 'https://example.com/promo',
      ios: 'https://apps.apple.com/app/id123',
      android: '',
      fallbackUrl: 'https://example.com',
    });

    // desktop -> fallback page (web fallback); iPhone after the interstitial hop -> App Store
    expect((await click(key, DESKTOP)).headers.get('location')).toBe('https://example.com');
    expect((await click(key, IPHONE, true)).headers.get('location')).toBe('https://apps.apple.com/app/id123');
    const ev = await fetch(`${SDK}/api/analytics/event`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${app.apiKey}`,
        'X-App-ID': app.id,
        'X-SDK': 'react-native/2.3.1',
      },
      body: JSON.stringify({ event_name: 'purchase' }),
    });
    expect(ev.status).toBe(200);
    expect((await matchLink(app)).status).toBe(200); // the iPhone installs: one match, iOS

    location.hash = `#/apps/${app.id}/analytics`;
    render(<App />);
    await waitFor(() => expect(tileValue('Clicks')).toBe(2));
    expect(tileValue('Web fallbacks')).toBe(1);
    expect(tileValue('Installs via link')).toBe(1);
    expect(tileValue('Already installed opens')).toBe(0);
    expect(screen.getByText('purchase').closest('tr')!.querySelector('[data-label="Count"]')!.textContent).toBe('1');
    screen.getByText('No in-app clicks in this range.');

    fireEvent.change(screen.getByLabelText('Platform'), { target: { value: 'ios' } });
    await waitFor(() => expect(tileValue('Clicks')).toBe(1));
    expect(tileValue('Web fallbacks')).toBe(0);

    // the filter is page-level: the Links tab counts follow it, and clearing it restores both clicks
    go(`#/apps/${app.id}`);
    const linkCounts = () => {
      const tr = screen.getByText(key).closest('tr')!;
      return [tr.querySelector('[data-label="Clicks"]')!.textContent, tr.querySelector('[data-label="Matches"]')!.textContent];
    };
    await waitFor(() => expect(linkCounts()).toEqual(['1', '1']));
    expect((screen.getByLabelText('Platform') as HTMLSelectElement).value).toBe('ios');
    fireEvent.change(screen.getByLabelText('Platform'), { target: { value: 'android' } });
    await waitFor(() => expect(linkCounts()).toEqual(['0', '0']));
    fireEvent.change(screen.getByLabelText('Platform'), { target: { value: '' } });
    await waitFor(() => expect(linkCounts()).toEqual(['2', '1']));
    await api.deleteApp(app.id);
  });

  it('in-app sources flow: a Messenger tap gets the tap page and shows up as one Messenger row', async () => {
    const app = await api.createApp(uniq('inapp-app'));
    const key = uniq('social');
    await api.createLink(app.id, {
      key,
      url: 'https://example.com/social',
      ios: 'https://apps.apple.com/app/id123',
      android: '',
      fallbackUrl: '',
    });
    const res = await click(key, MESSENGER, true);
    expect(res.status).toBe(200);
    expect(await res.text()).toContain('Get the app');

    location.hash = `#/apps/${app.id}/analytics`;
    render(<App />);
    const name = await screen.findByText('Messenger');
    expect(name.closest('tr')!.querySelector('[data-label="Clicks"]')!.textContent).toBe('1');
    await api.deleteApp(app.id);
  });

  it('tab flow: Links lands first, each tab hash renders its own content, aria-selected follows the hash, filters only on Links/Analytics', async () => {
    const app = await api.createApp(uniq('tabs-app'));
    location.hash = `#/apps/${app.id}`;
    render(<App />);
    await screen.findByRole('heading', { name: app.name });
    expect(tabs().map((t) => t.textContent)).toEqual(['Links', 'Analytics', 'Fraud', 'Settings', 'Webhooks']);
    expect(selectedTab()).toEqual(['Links']);
    await screen.findByText('No links yet');
    screen.getByRole('button', { name: 'New link' });
    screen.getByLabelText('Platform');

    // clicking a tab writes the hash (refresh/back keep the tab)
    fireEvent.click(screen.getByRole('tab', { name: 'Analytics' }));
    expect(location.hash).toBe(`#/apps/${app.id}/analytics`);
    window.dispatchEvent(new Event('hashchange'));
    await screen.findByRole('heading', { name: 'Activity' });
    expect(selectedTab()).toEqual(['Analytics']);
    expect(screen.queryByRole('button', { name: 'New link' })).toBeNull();
    screen.getByLabelText('Date range');

    fireEvent.click(screen.getByRole('tab', { name: 'Fraud' }));
    expect(location.hash).toBe(`#/apps/${app.id}/fraud`);
    window.dispatchEvent(new Event('hashchange'));
    await screen.findByRole('region', { name: 'Fraud signals' });
    screen.getByRole('region', { name: 'Flagged installs' });
    expect(selectedTab()).toEqual(['Fraud']);
    expect(screen.queryByLabelText('Platform')).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Activity' })).toBeNull();

    fireEvent.click(screen.getByRole('tab', { name: 'Settings' }));
    expect(location.hash).toBe(`#/apps/${app.id}/settings`);
    window.dispatchEvent(new Event('hashchange'));
    await screen.findByRole('heading', { name: 'API key' });
    expect(selectedTab()).toEqual(['Settings']);
    expect(screen.queryByLabelText('Platform')).toBeNull();
    // fraud lives only on its tab: nothing of it on Settings, even once every Settings request has landed
    await screen.findByText('No installs yet.');
    await settle();
    expect(screen.queryByRole('region', { name: 'Fraud signals' })).toBeNull();
    expect(screen.queryByRole('region', { name: 'Flagged installs' })).toBeNull();
    expect(screen.queryByText('Click flooding')).toBeNull();

    fireEvent.click(screen.getByRole('tab', { name: 'Webhooks' }));
    expect(location.hash).toBe(`#/apps/${app.id}/webhooks`);
    window.dispatchEvent(new Event('hashchange'));
    await screen.findByRole('heading', { name: 'Webhook endpoints' });
    expect(selectedTab()).toEqual(['Webhooks']);

    fireEvent.click(screen.getByRole('tab', { name: 'Links' }));
    expect(location.hash).toBe(`#/apps/${app.id}`);
    window.dispatchEvent(new Event('hashchange'));
    await screen.findByText('No links yet');
    expect(selectedTab()).toEqual(['Links']);
    await api.deleteApp(app.id);
  });

  it('matching flow: save persists to the server, empty fields are rejected', async () => {
    const app = await api.createApp(uniq('m'));
    render(<AppPage id={app.id} tab="settings" />);
    await screen.findByDisplayValue('850');
    fireEvent.input(screen.getByLabelText('Match threshold'), { target: { value: '900' } });
    fireEvent.input(screen.getByLabelText('Match window (minutes)'), { target: { value: '30' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save matching' }));
    await screen.findByText('Saved.');
    expect(await api.getApp(app.id)).toMatchObject({ matchThreshold: 900, matchWindowMinutes: 30 });

    // empty fields: honest error, no false "Saved."
    fireEvent.input(screen.getByLabelText('Match threshold'), { target: { value: '' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save matching' }));
    await screen.findByText('threshold and window are required');
    expect(screen.queryByText('Saved.')).toBeNull();

    // out of range: client-side rejection, no round trip
    fireEvent.input(screen.getByLabelText('Match threshold'), { target: { value: '1300' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save matching' }));
    await screen.findByText(/threshold must be 700–1200/);
    expect(screen.queryByText('Saved.')).toBeNull();
    expect(await api.getApp(app.id)).toMatchObject({ matchThreshold: 900, matchWindowMinutes: 30 });
    await api.deleteApp(app.id);
  });

  it('settings flow: fresh app shows failing SDK check and no match receipts', async () => {
    const app = await api.createApp(uniq('h'));
    render(<AppPage id={app.id} tab="settings" />);
    await screen.findByText(/No SDK call yet/);
    expect(screen.getAllByText('Fix').length).toBeGreaterThan(0);
    await screen.findByText('No installs yet.');
    await api.deleteApp(app.id);
  });

  it('rotate key flow: new key is shown once', async () => {
    const app = await api.createApp(uniq('rotate-app'));
    render(<AppPage id={app.id} tab="settings" />);
    await screen.findByRole('heading', { name: 'API key' });

    fireEvent.click(screen.getByRole('button', { name: 'Rotate', exact: true }));
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Rotate key' }));
    await screen.findByText('Key rotated');
    expect((within(openDialog()).getByDisplayValue(/^dk_/) as HTMLInputElement).value).toMatch(/^dk_[A-Za-z0-9]{22}$/);
    expect((await api.getApp(app.id)).apiKeyHash).not.toBe(app.apiKeyHash);
    await api.deleteApp(app.id);
  });

  it('delete app flow: confirm removes the row and the server row', async () => {
    const name = uniq('del-app');
    const app = await api.createApp(name);
    render(<AppsPage />);
    const card = (await screen.findByText(name)).closest('tr')!;

    fireEvent.click(within(card).getByRole('button', { name: 'Delete' }));
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(screen.queryByText(name)).toBeNull());
    await expect(api.getApp(app.id)).rejects.toThrow(/404/);
  });

  it('app config flow: Settings tab saves iOS App ID, list row shows the iOS badge', async () => {
    const name = uniq('edit-app');
    const app = await api.createApp(name);
    render(<AppPage id={app.id} tab="settings" />);
    fireEvent.input(await screen.findByLabelText('iOS App ID'), {
      target: { value: 'ABCDE12345.com.example.app' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save config' }));
    await screen.findByText('Saved.');
    expect((await api.getApp(app.id)).iosAppId).toBe('ABCDE12345.com.example.app');
    cleanup();

    // list has no Edit button; the row carries the badge
    render(<AppsPage />);
    const row = (await screen.findByText(name)).closest('tr')!;
    within(row).getByText('iOS', { exact: true });
    expect(within(row).queryByRole('button', { name: 'Edit' })).toBeNull();
    await api.deleteApp(app.id);
  });

  it('revoke key flow: key removed, status text shown', async () => {
    const app = await api.createApp(uniq('revoke-app'));
    render(<AppPage id={app.id} tab="settings" />);
    await screen.findByRole('heading', { name: 'API key' });

    const keyCard = screen.getByRole('button', { name: 'Rotate', exact: true }).closest('tr')!;
    fireEvent.click(within(keyCard).getByRole('button', { name: 'Delete' }));
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Delete' }));

    await screen.findByText(/revoked — SDK calls rejected/);
    expect((await api.getApp(app.id)).apiKeyHash).toBe('');
    await api.deleteApp(app.id);
  });

  it('edit link flow: url change persists to the table and the server', async () => {
    const app = await api.createApp(uniq('edit-link-app'));
    await api.createLink(app.id, {
      key: 'summer-sale',
      url: 'https://example.com/promo',
      ios: '',
      android: '',
      fallbackUrl: '',
    });
    render(<AppPage id={app.id} tab="links" />);
    const row = (await screen.findByText('summer-sale')).closest('tr')!;

    fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    fireEvent.input(within(openDialog()).getByLabelText('Destination URL'), {
      target: { value: 'https://example.com/v2' },
    });
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Save', exact: true }));

    await waitFor(() => screen.getByText('https://example.com/v2'));
    expect((await api.listLinks(app.id))[0].url).toBe('https://example.com/v2');
    await api.deleteApp(app.id);
  });

  it('link validation flow: empty fields rejected', async () => {
    const app = await api.createApp(uniq('link-val-app'));
    render(<AppPage id={app.id} tab="links" />);
    await screen.findByRole('heading', { name: app.name });

    fireEvent.click(screen.getByRole('button', { name: 'New link' }));
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Create', exact: true }));
    await screen.findByText('key and url are required');

    expect(await api.listLinks(app.id)).toHaveLength(0);

    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Cancel' }));
    await api.deleteApp(app.id);
  });

  it('fraud signals flow: defaults from the server, mode press saves at once with a toast, re-pressing the current mode does nothing', async () => {
    const app = await api.createApp(uniq('fraud-mode'));
    location.hash = `#/apps/${app.id}/fraud`;
    render(<App />);
    await screen.findByRole('region', { name: 'Fraud signals' });
    await waitFor(() => expect(threshold('Max clicks per IP').value).toBe('20'));

    // fresh app: every moded signal Tagged, Repeated device is tag only, thresholds are the server defaults
    const rows = Array.from(document.querySelectorAll('li.signal-row')).map((li) => li.getAttribute('aria-label'));
    expect(rows).toEqual(['Click flooding', 'Suspicious timing', 'Bot traffic', 'Datacenter IP', 'Repeated device']);
    for (const label of ['Click flooding', 'Suspicious timing', 'Bot traffic', 'Datacenter IP']) expect(pressed(label)).toEqual(['Tagged']);
    within(screen.getByRole('listitem', { name: 'Repeated device' })).getByText('Tag only');
    expect(within(screen.getByRole('listitem', { name: 'Repeated device' })).queryByRole('group')).toBeNull();
    const shown = Object.fromEntries(
      [
        ['Max clicks per IP', 'velocityIpMax'],
        ['Max clicks per link', 'velocityLinkMax'],
        ['Window (minutes)', 'velocityWindowMinutes'],
        ['Too soon (seconds)', 'timingShortSeconds'],
        ['Too late (hours)', 'timingLongHours'],
        ['Max installs', 'fingerprintMax'],
        ['Window (days)', 'fingerprintWindowDays'],
      ].map(([label, k]) => [k, Number(threshold(label).value)]),
    );
    expect(shown).toEqual({
      velocityIpMax: 20, velocityLinkMax: 500, velocityWindowMinutes: 60,
      timingShortSeconds: 10, timingLongHours: 24, fingerprintMax: 5, fingerprintWindowDays: 7,
    });

    // one press = one save, no confirm dialog
    fireEvent.click(within(modeGroup('Bot traffic')).getByRole('button', { name: 'Active' }));
    expect(document.querySelector('dialog[open]')).toBeNull();
    const t = await findToast('Bot traffic: active');
    expect(t.title).toBe('');
    expect(pressed('Bot traffic')).toEqual(['Active']);
    expect(await api.getFraudSettings(app.id)).toMatchObject({
      userAgentMode: 'active', velocityMode: 'tagged', timingMode: 'tagged', ipMode: 'tagged', velocityIpMax: 20,
    });

    // pressing the mode that is already on: no save, no toast
    const seen = lastToastId();
    fireEvent.click(within(modeGroup('Bot traffic')).getByRole('button', { name: 'Active' }));
    fireEvent.click(within(modeGroup('Click flooding')).getByRole('button', { name: 'Tagged' }));
    await settle();
    expect(toasts().filter((x) => x.id > seen)).toEqual([]);
    expect(pressed('Bot traffic')).toEqual(['Active']);
    expect(pressed('Click flooding')).toEqual(['Tagged']);

    // and back: Tagged saves too
    fireEvent.click(within(modeGroup('Bot traffic')).getByRole('button', { name: 'Tagged' }));
    await findToast('Bot traffic: tagged');
    expect((await api.getFraudSettings(app.id)).userAgentMode).toBe('tagged');

    // changed elsewhere (another tab): Refresh shows the server's values
    await api.saveFraudSettings(app.id, { ...(await api.getFraudSettings(app.id)), velocityMode: 'active', velocityIpMax: 44 });
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    await waitFor(() => expect(pressed('Click flooding')).toEqual(['Active']));
    await waitFor(() => expect(threshold('Max clicks per IP').value).toBe('44'));

    // a fresh mount reads what the server stored
    fireEvent.click(within(modeGroup('Datacenter IP')).getByRole('button', { name: 'Active' }));
    await findToast('Datacenter IP: active');
    cleanup();
    location.hash = `#/apps/${app.id}/fraud`;
    render(<App />);
    await waitFor(() => expect(pressed('Datacenter IP')).toEqual(['Active']));
    expect(pressed('Bot traffic')).toEqual(['Tagged']);
    await api.deleteApp(app.id);
  });

  it('fraud signals flow: a failed mode save reverts the toggle and toasts "Not saved"', async () => {
    const app = await api.createApp(uniq('fraud-fail'));
    location.hash = `#/apps/${app.id}/fraud`;
    render(<App />);
    await waitFor(() => expect(pressed('Click flooding')).toEqual(['Tagged']));

    // app deleted elsewhere (another tab): the save fails server-side
    await api.deleteApp(app.id);
    fireEvent.click(within(modeGroup('Click flooding')).getByRole('button', { name: 'Active' }));
    const t = await findToast(/404/);
    expect(t.title).toBe('Not saved');
    await waitFor(() => expect(pressed('Click flooding')).toEqual(['Tagged']));
  });

  it('fraud thresholds flow: blur and Enter save changed values, unchanged or empty sends nothing, out of range reverts with "Not saved"', async () => {
    const app = await api.createApp(uniq('fraud-num'));
    location.hash = `#/apps/${app.id}/fraud`;
    render(<App />);
    await waitFor(() => expect(threshold('Max clicks per IP').value).toBe('20'));

    // blur with a new value saves just that field
    await editAndBlur('Max clicks per IP', '30');
    await findToast('Click flooding: max clicks per ip set to 30');
    expect(await api.getFraudSettings(app.id)).toMatchObject({ velocityIpMax: 30, velocityLinkMax: 500, velocityMode: 'tagged' });

    // Enter commits too
    const soon = threshold('Too soon (seconds)');
    soon.focus();
    await settle();
    fireEvent.input(soon, { target: { value: '5' } });
    await tick();
    fireEvent.keyDown(soon, { key: 'Enter' });
    await findToast('Suspicious timing: too soon (seconds) set to 5');
    expect(await api.getFraudSettings(app.id)).toMatchObject({ timingShortSeconds: 5, velocityIpMax: 30 });

    // thousands render with separators in the toast
    await editAndBlur('Max clicks per link', '12000');
    await findToast('Click flooding: max clicks per link set to 12,000');
    expect((await api.getFraudSettings(app.id)).velocityLinkMax).toBe(12000);

    // unchanged value, or a cleared field: no save, no toast; a cleared field shows the saved value again
    let seen = lastToastId();
    await editAndBlur('Max clicks per IP', '30');
    await editAndBlur('Max installs', '');
    await settle();
    expect(toasts().filter((x) => x.id > seen)).toEqual([]);
    expect(threshold('Max installs').value).toBe('5');

    // out of range (server bound 2..10000): rejected, field shows the saved value, server keeps it
    seen = lastToastId();
    await editAndBlur('Max clicks per IP', '1');
    const t = await findToast(/velocity IP max out of range/);
    expect(t.title).toBe('Not saved');
    await waitFor(() => expect(threshold('Max clicks per IP').value).toBe('30'));
    expect(toasts().filter((x) => x.id > seen)).toHaveLength(1);
    expect(await api.getFraudSettings(app.id)).toMatchObject({
      velocityIpMax: 30, velocityLinkMax: 12000, timingShortSeconds: 5, fingerprintMax: 5,
    });

    // a later save after a rejection carries the saved values, not the rejected one
    await editAndBlur('Window (days)', '14');
    await findToast('Repeated device: window (days) set to 14');
    expect(await api.getFraudSettings(app.id)).toMatchObject({ fingerprintWindowDays: 14, velocityIpMax: 30 });
    await api.deleteApp(app.id);
  });

  it("fraud app switch flow: moving from app A's Fraud tab to app B's shows and saves B's settings, never A's", async () => {
    const a = await api.createApp(uniq('fraud-a'));
    const b = await api.createApp(uniq('fraud-b'));
    await api.saveFraudSettings(a.id, { ...(await api.getFraudSettings(a.id)), userAgentMode: 'active', velocityIpMax: 33 });

    location.hash = `#/apps/${a.id}/fraud`;
    render(<App />);
    await screen.findByRole('heading', { name: a.name });
    await waitFor(() => expect(threshold('Max clicks per IP').value).toBe('33'));
    expect(pressed('Bot traffic')).toEqual(['Active']);

    go(`#/apps/${b.id}/fraud`);
    await screen.findByRole('heading', { name: b.name });
    await waitFor(() => expect(threshold('Max clicks per IP').value).toBe('20'));
    await settle();
    expect(threshold('Max clicks per IP').value).toBe('20');
    expect(pressed('Bot traffic')).toEqual(['Tagged']);
    expect(selectedTab()).toEqual(['Fraud']);

    // a save on B lands on B only
    await editAndBlur('Max clicks per link', '600');
    await findToast('Click flooding: max clicks per link set to 600');
    expect(await api.getFraudSettings(b.id)).toMatchObject({ velocityLinkMax: 600, velocityIpMax: 20, userAgentMode: 'tagged' });
    expect(await api.getFraudSettings(a.id)).toMatchObject({ velocityLinkMax: 500, velocityIpMax: 33, userAgentMode: 'active' });

    // and back to A: A's values again
    go(`#/apps/${a.id}/fraud`);
    await screen.findByRole('heading', { name: a.name });
    await waitFor(() => expect(threshold('Max clicks per IP').value).toBe('33'));
    expect(threshold('Max clicks per link').value).toBe('500');
    await api.deleteApp(a.id);
    await api.deleteApp(b.id);
  });

  it('flagged installs flow: a real fast install is tagged and listed; after turning timing active in the UI the next one is kept organic', async () => {
    const app = await api.createApp(uniq('fraud-view'));
    const key = uniq('fast');
    await api.createLink(app.id, { key, url: 'https://example.com/p', ios: 'https://apps.apple.com/app/id123', android: '', fallbackUrl: '' });
    location.hash = `#/apps/${app.id}/fraud`;
    render(<App />);
    const section = await screen.findByRole('region', { name: 'Flagged installs' });
    await within(section).findByText('Nothing flagged in this range.');
    expect(within(section).queryByLabelText('Flagged installs per signal')).toBeNull();

    // click then first launch within seconds: timing fires (tagged), install still attributed (IP 500 + iOS version 350)
    expect((await click(key, IPHONE, true)).status).toBe(302);
    expect((await matchLink(app)).status).toBe(200);

    // Refresh reloads the Fraud tab
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    const row = (await within(section).findByText(key)).closest('tr')!;
    expect(within(row).getByText('Suspicious timing').closest('[data-label="Signals"]')).not.toBeNull();
    within(row).getByText('Attributed, tagged only');
    expect(within(row).queryByText(/Flagged click on/)).toBeNull();
    let counts = flaggedCounts(section);
    expect(counts['Suspicious timing']).toBe(1);
    expect(counts['Bot traffic']).toBe(0);
    expect((await api.getFraud(app.id, 7)).installs).toMatchObject([
      { linkKey: key, attribution: 'non_organic', fraudAction: '', fraud: expect.arrayContaining(['timing']) },
    ]);

    // operator turns timing active from the Fraud tab
    fireEvent.click(within(modeGroup('Suspicious timing')).getByRole('button', { name: 'Active' }));
    await findToast('Suspicious timing: active');
    expect((await api.getFraudSettings(app.id)).timingMode).toBe('active');

    // the next fast install (another iPhone) loses its credit: the SDK gets no link, the install is organic with the flagged click named
    expect((await click(key, IPHONE_B, true)).status).toBe(302);
    expect((await matchLink(app, IPHONE_B)).status).toBe(404);
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    await waitFor(() => expect(within(section).getAllByRole('row')).toHaveLength(3)); // header + 2
    const [, newest, older] = within(section).getAllByRole('row');
    within(newest).getByText('Kept as organic');
    within(newest).getByText(`Flagged click on ${key}`);
    within(newest).getByText('Suspicious timing');
    within(older).getByText('Attributed, tagged only');
    counts = flaggedCounts(section);
    expect(counts['Suspicious timing']).toBe(2);
    const stored = (await api.getFraud(app.id, 7)).installs;
    expect(stored[0]).toMatchObject({ attribution: 'organic', fraudAction: 'excluded', fraudLinkKey: key });

    // a wider range still holds the same two installs
    fireEvent.change(within(section).getByLabelText('Flagged range'), { target: { value: '90' } });
    await settle();
    await waitFor(() => expect(within(section).getAllByRole('row')).toHaveLength(3));
    expect(flaggedCounts(section)['Suspicious timing']).toBe(2);
    within(within(section).getAllByRole('row')[1]).getByText('Kept as organic');
    await api.deleteApp(app.id);
  });

  it('app not found flow: unknown id shows the recovery card', async () => {
    render(<AppPage id="does-not-exist" tab="links" />);
    await screen.findByText('App not found');
    screen.getByRole('link', { name: 'Back to apps' });
    expect(screen.queryByRole('tablist')).toBeNull();
  });

  it('shell flow: hash routes reach apps and every app tab; malformed hash falls back to apps', async () => {
    const app = await api.createApp(uniq('shell-app'));
    location.hash = '';
    render(<App />);
    await screen.findByRole('heading', { name: 'Apps' });

    go(`#/apps/${app.id}`);
    await screen.findByRole('heading', { name: app.name });
    expect(selectedTab()).toEqual(['Links']);

    go(`#/apps/${app.id}/settings`);
    await screen.findByRole('heading', { name: 'API key' });
    expect(selectedTab()).toEqual(['Settings']);

    // unknown tab segment: the app page, Links tab
    go(`#/apps/${app.id}/bogus`);
    await waitFor(() => expect(selectedTab()).toEqual(['Links']));
    await api.deleteApp(app.id);

    // malformed percent-encoding: crash-safe fallback to the apps page
    go('#/apps/%zz');
    await screen.findByRole('heading', { name: 'Apps' });
    expect(screen.queryByText('Something went wrong')).toBeNull();
  });

  it('AE1: ADMIN_PASSWORD unset — no admin entry, elevation inert, admin APIs open without a session', async () => {
    // config: adminSet false, so the shell offers no admin button or chip
    expect(await api.getConfig()).toMatchObject({ logoutUrl: '', adminSet: false });
    location.hash = '#/';
    render(<App />);
    await screen.findByRole('heading', { name: 'Apps' });
    expect(screen.queryByRole('button', { name: 'Enter admin' })).toBeNull();
    expect(screen.queryByText(/Admin · until/)).toBeNull();

    // nothing to elevate from: POST rejects, GET stays inactive
    await expect(api.enterAdminMode('anything')).rejects.toThrow(/403/);
    expect(await api.getAdminSession()).toMatchObject({ admin: false });

    // admin routes pass through without any session (today's behavior)
    const app = await api.createApp(uniq('ae1-app'));
    expect((await api.getApp(app.id)).id).toBe(app.id);
    await api.deleteApp(app.id);
  });
});
