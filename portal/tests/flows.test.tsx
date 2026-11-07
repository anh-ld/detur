import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest';
import { ChildProcess } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/preact';
import { api, setApiBase } from '../src/api';
import { App } from '../src/app';
import { AppsPage } from '../src/apps';
import { AppPage } from '../src/detail';
import { go, openDialog, saveButton, selectedTab, startServer, stopServer, tabNames, uniq, waitForServer } from './_integration';

// the sidebar app switcher is a native select: its selected option names the current app
const currentApp = () => (screen.getByLabelText('Switch app') as HTMLSelectElement).selectedOptions[0]?.textContent;

// client flows: real pages vs real Go binary + fresh SQLite. SDK :8080 hardcoded (must be free); portal :8091.
const PORT = 8091;
const BASE = `http://127.0.0.1:${PORT}`;
const bin = join(tmpdir(), 'detur-integ');

let server: ChildProcess;
let dbDir: string;
let getStderr: () => string = () => '';

// short links + SDK API share :8080
const SDK = 'http://127.0.0.1:8080';
const IPHONE = 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1';
// 2nd iPhone, same iOS + IP; own UA so server doesn't merge it into the first click
const IPHONE_B = IPHONE.replace('15E148', '15E149');
const MESSENGER =
  'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Mobile/15E148 [FBAN/MessengerForiOS;FBAV/442.0.0.42.110;FBSN/iOS]';
const DESKTOP = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36';

async function click(key: string, ua: string, done = false): Promise<Response> {
  return fetch(`${SDK}/${key}${done ? '?_dt=1&tz=UTC&screen=393x852%403' : ''}`, { headers: { 'User-Agent': ua }, redirect: 'manual' });
}

// first SDK launch on the clicking iPhone: same IP + iOS + screen -> matches latest click. UA = distinct device.
async function matchLink(app: { id: string; apiKey: string }, userAgent = IPHONE): Promise<Response> {
  return fetch(`${SDK}/api/link/match-link`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${app.apiKey}`,
      'X-App-ID': app.id,
      'X-SDK': 'react-native/2.3.1',
    },
    body: JSON.stringify({
      platform: 'ios',
      model: 'iPhone',
      systemVersion: '17.5',
      screenWidth: 393,
      screenHeight: 852,
      scale: 3,
      timezone: 'UTC',
      userAgent,
    }),
  });
}

// on-screen toasts (kinu ToastContainer in App): ids monotonic -> no new toast = no id above last seen
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
// type like a person: wait for paint (preact flushes mount effects after a frame), keystroke renders, then blur
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

// stat number by label (labels repeat in legends). First text node: some stats append a share.
const tileValue = (label: string): number => {
  const stat = screen
    .getAllByText(label, { exact: true })
    .map((e) => e.closest('.stat'))
    .find(Boolean)!;
  return parseInt(stat.querySelector('.stat-value')!.firstChild!.textContent!, 10);
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

    // row: bare id, no key/copy/Edit (config on settings page)
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
    fireEvent.click(await screen.findByRole('button', { name: 'New link' }));
    fireEvent.input(within(openDialog()).getByLabelText('Short link'), { target: { value: 'summer-sale' } });
    fireEvent.input(within(openDialog()).getByLabelText('Destination'), {
      target: { value: '/promo' },
    });
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Create', exact: true }));

    const row = (await screen.findAllByText('summer-sale'))[0].closest('tr')!;
    expect(row.innerText).toContain('/promo');
    expect((await api.listLinks(app.id)).length).toBe(1);
    expect(screen.getByRole('button', { name: /^Tags/ })).toBeTruthy(); // filter stays visible with no tags yet

    fireEvent.click(within(row).getByRole('button', { name: 'Delete' }));
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(screen.queryAllByText('summer-sale')).toHaveLength(0));
    expect(await api.listLinks(app.id)).toHaveLength(0);
    await api.deleteApp(app.id);
  });

  it('tags flow: chips add on Enter and remove with ×; the tag filter matches any selected tag', async () => {
    const app = await api.createApp(uniq('tags-app'));
    const tagged = await api.createLink(app.id, {
      key: uniq('promo'),
      url: '/promo',
      ios: '',
      android: '',
      fallbackUrl: '',
      tags: ['black-friday'],
    });
    await api.createLink(app.id, { key: uniq('spring'), url: '/spring', ios: '', android: '', fallbackUrl: '', tags: ['spring'] });
    await api.createLink(app.id, { key: uniq('plain'), url: '/plain', ios: '', android: '', fallbackUrl: '' });

    render(<AppPage id={app.id} tab="links" />);
    const row = (await screen.findAllByText(tagged.key))[0].closest('tr')!;
    within(row.querySelector('.link-cell')!).getByText('black-friday'); // badge on the row (the row also hosts its edit dialog)

    // chips: Enter adds, × removes
    fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    const dlg = openDialog();
    // combobox offers tags used on other links; tags already on this link are not listed
    const offered = [...dlg.querySelectorAll('[k="item"]')].map((o) => o.textContent);
    expect(offered).toContain('spring');
    expect(offered).not.toContain('black-friday');
    // pick an existing tag, type a new one, remove one
    const tagsInput = within(dlg).getByLabelText(/Tags/);
    fireEvent.focus(tagsInput); // opens the suggestion list
    fireEvent.click(within(dlg).getByRole('button', { name: 'spring' }));
    within(dlg).getByText('spring');
    fireEvent.input(tagsInput, { target: { value: 'alice' } });
    fireEvent.keyDown(tagsInput, { key: 'Enter' });
    within(dlg).getByText('alice');
    fireEvent.click(within(dlg).getByRole('button', { name: 'Remove black-friday' }));
    expect(within(dlg).queryByRole('button', { name: 'Remove black-friday' })).toBeNull(); // chip gone
    await waitFor(() => expect((within(dlg).getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(false)); // rules loaded
    fireEvent.click(within(dlg).getByRole('button', { name: 'Save' }));
    await waitFor(async () => expect((await api.listLinks(app.id)).find((l) => l.id === tagged.id)!.tags).toEqual(['spring', 'alice']));

    // filter: any selected tag matches (OR), Clear restores all
    fireEvent.click(screen.getByRole('button', { name: /^Tags/ })); // opens the popover
    const rows = () => screen.getAllByRole('row').slice(1); // header off
    fireEvent.click(await screen.findByLabelText('alice'));
    await waitFor(() => expect(rows()).toHaveLength(1));
    fireEvent.click(screen.getByLabelText('spring'));
    await waitFor(() => expect(rows()).toHaveLength(2));
    fireEvent.click(screen.getByRole('button', { name: 'Clear' }));
    await waitFor(() => expect(rows()).toHaveLength(3));
    await api.deleteApp(app.id);
  });

  it('analytics flow: real clicks and SDK events reach tiles and top events; the platform filter narrows tiles and Links click/match counts', async () => {
    const app = await api.createApp(uniq('stats-app'));
    const key = uniq('promo');
    await api.createLink(app.id, {
      key,
      url: '/promo',
      ios: 'https://apps.apple.com/app/id123',
      android: '',
      fallbackUrl: 'https://example.com',
    });

    // desktop -> web fallback; iPhone after interstitial hop -> App Store
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
    expect(screen.getByText('purchase').closest('li')!.querySelector('.num')!.textContent).toBe('1');
    screen.getByText('No in-app clicks');

    fireEvent.change(screen.getByLabelText('Platform'), { target: { value: 'ios' } });
    await waitFor(() => expect(tileValue('Clicks')).toBe(1));
    expect(tileValue('Web fallbacks')).toBe(0);

    // filter is page-level: Links tab counts follow; clearing restores both clicks
    fireEvent.click(screen.getByRole('link', { name: /^Links/ })); // sidebar tab keeps the filter query
    const linkCounts = () => {
      const tr = screen.getAllByText(key)[0].closest('tr')!;
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
      url: '/social',
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
    expect(name.closest('li')!.querySelector('.num')!.textContent).toBe('1');
    await api.deleteApp(app.id);
  });

  it('link analytics flow: events from a tagged device reach Conversions by link; Retention by link counts the device, unreached marks show –', async () => {
    const app = await api.createApp(uniq('tag-app'));
    const key = uniq('tag');
    await api.createLink(app.id, { key, url: '/t', ios: '', android: '', fallbackUrl: '' });
    const sdk = (path: string, body: object) =>
      fetch(`${SDK}/api/analytics/${path}`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          Authorization: `Bearer ${app.apiKey}`,
          'X-App-ID': app.id,
          'X-SDK': 'react-native/2.3.1',
        },
        body: JSON.stringify(body),
      });
    expect((await sdk('event', { event_name: 'detur_link', data: { link: key }, device_id: 'dev-1' })).status).toBe(200);
    expect((await sdk('event', { event_name: 'purchase', data: {}, device_id: 'dev-1' })).status).toBe(200);
    expect((await sdk('retention', { event_name: 'app_open', device_id: 'dev-1' })).status).toBe(200);

    location.hash = `#/apps/${app.id}/analytics`;
    render(<App />);
    const conv = await screen.findByRole('region', { name: 'Conversions by link' });
    await waitFor(() => expect(within(conv).getAllByRole('row')).toHaveLength(2)); // header + purchase; tag marker isn't a conversion
    expect(within(conv).getByText('purchase').closest('tr')!.querySelector('[data-label="Count"]')!.textContent).toBe('1');
    const ret = screen.getByRole('region', { name: 'Retention by link' });
    const tr = within(ret).getByText(key).closest('tr')!;
    expect(tr.querySelector('[data-label="Devices"]')!.textContent).toBe('1');
    expect(tr.querySelector('[data-label="Day 1"]')!.textContent).toBe('–');
    await api.deleteApp(app.id);
  });

  it('tag links flow: the switch saves on press with a toast; match-link then hands the app detur_link=<key>', async () => {
    const app = await api.createApp(uniq('tagset-app'));
    const key = uniq('tagset');
    await api.createLink(app.id, { key, url: '/d', ios: 'https://apps.apple.com/app/id123', android: '', fallbackUrl: '' });
    location.hash = `#/apps/${app.id}/settings`;
    render(<App />);
    fireEvent.click(await screen.findByRole('tab', { name: 'Matching' }));
    const sw = (await screen.findByLabelText('Tag deferred links')) as HTMLInputElement;
    expect(sw.checked).toBe(false);
    fireEvent.click(sw);
    await findToast('Tag deferred links: on');
    expect((await api.getApp(app.id)).tagLinks).toBe(true);

    await click(key, IPHONE, true);
    const res = await matchLink(app);
    expect(res.status).toBe(200);
    expect((await res.json()).link).toBe(`/${app.id.slice(0, 3)}/d?detur_link=${key}`);
    await api.deleteApp(app.id);
  });

  it('tab flow: Links lands first, each tab hash renders its own content, aria-selected follows the hash, filters only on Links/Analytics', async () => {
    const app = await api.createApp(uniq('tabs-app'));
    location.hash = `#/apps/${app.id}`;
    render(<App />);
    await waitFor(() => expect(currentApp()).toBe(app.name));
    expect(tabNames()).toEqual(['Links', 'Analytics', 'Fraud', 'Settings', 'Webhooks']);
    expect(selectedTab()).toEqual(['Links']);
    await screen.findByText('No links yet');
    screen.getByRole('button', { name: 'New link' });
    screen.getByLabelText('Platform');

    // tab click writes hash (refresh/back keep tab)
    fireEvent.click(screen.getByRole('link', { name: 'Analytics' }));
    expect(location.hash).toBe(`#/apps/${app.id}/analytics`);
    window.dispatchEvent(new Event('hashchange'));
    await screen.findByRole('heading', { name: 'Activity' });
    expect(selectedTab()).toEqual(['Analytics']);
    expect(screen.queryByRole('button', { name: 'New link' })).toBeNull();
    screen.getByLabelText('Date range');

    fireEvent.click(screen.getByRole('link', { name: 'Fraud' }));
    expect(location.hash).toBe(`#/apps/${app.id}/fraud`);
    window.dispatchEvent(new Event('hashchange'));
    await screen.findByRole('region', { name: 'Fraud signals' });
    screen.getByRole('region', { name: 'Flagged installs' });
    expect(selectedTab()).toEqual(['Fraud']);
    expect(screen.queryByLabelText('Platform')).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Activity' })).toBeNull();

    fireEvent.click(screen.getByRole('link', { name: 'Settings' }));
    expect(location.hash).toBe(`#/apps/${app.id}/settings`);
    window.dispatchEvent(new Event('hashchange'));
    await screen.findByRole('heading', { name: 'API key' });
    expect(selectedTab()).toEqual(['Settings']);
    expect(screen.queryByLabelText('Platform')).toBeNull();
    // fraud only on its tab: none on Settings, even after all Settings requests land
    fireEvent.click(screen.getByRole('tab', { name: 'Matching' }));
    await screen.findByText('No installs yet.');
    await settle();
    expect(screen.queryByRole('region', { name: 'Fraud signals' })).toBeNull();
    expect(screen.queryByRole('region', { name: 'Flagged installs' })).toBeNull();
    expect(screen.queryByText('Click flooding')).toBeNull();

    fireEvent.click(screen.getByRole('link', { name: 'Webhooks' }));
    expect(location.hash).toBe(`#/apps/${app.id}/webhooks`);
    window.dispatchEvent(new Event('hashchange'));
    await screen.findByText(/Webhooks need an admin password/);
    expect(selectedTab()).toEqual(['Webhooks']);

    fireEvent.click(screen.getByRole('link', { name: 'Links' }));
    expect(location.hash).toBe(`#/apps/${app.id}`);
    window.dispatchEvent(new Event('hashchange'));
    await screen.findByText('No links yet');
    expect(selectedTab()).toEqual(['Links']);
    await api.deleteApp(app.id);
  });

  it('matching flow: save persists to the server, empty fields are rejected', async () => {
    const app = await api.createApp(uniq('m'));
    render(<AppPage id={app.id} tab="settings" />);
    fireEvent.click(await screen.findByRole('tab', { name: 'Matching' }));
    await screen.findByDisplayValue('850');
    fireEvent.input(screen.getByLabelText('Match threshold'), { target: { value: '900' } });
    fireEvent.input(screen.getByLabelText('Match window (minutes)'), { target: { value: '30' } });
    fireEvent.click(saveButton(screen.getByLabelText('Match threshold')));
    await screen.findByText('Saved.');
    expect(await api.getApp(app.id)).toMatchObject({ matchThreshold: 900, matchWindowMinutes: 30 });

    // empty fields: honest error, no false "Saved."
    fireEvent.input(screen.getByLabelText('Match threshold'), { target: { value: '' } });
    fireEvent.click(saveButton(screen.getByLabelText('Match threshold')));
    await screen.findByText('threshold and window are required');
    expect(screen.queryByText('Saved.')).toBeNull();

    // out of range: rejected client-side, no round trip
    fireEvent.input(screen.getByLabelText('Match threshold'), { target: { value: '1300' } });
    fireEvent.click(saveButton(screen.getByLabelText('Match threshold')));
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
    fireEvent.click(screen.getByRole('tab', { name: 'Matching' }));
    await screen.findByText('No installs yet.');
    await api.deleteApp(app.id);
  });

  it('page flow: editor shows the default HTML on load, edits save as custom, a bad page shows the server error, Reset saves default', async () => {
    const app = await api.createApp(uniq('page-app'));
    location.hash = `#/apps/${app.id}/settings`;
    render(<App />); // toasts live in App's ToastContainer
    fireEvent.click(await screen.findByRole('tab', { name: 'Page' }));
    const html = (await screen.findByLabelText('HTML')) as HTMLTextAreaElement;
    expect(html.value).toContain('{{cta}}'); // one field, starts as the default
    expect(screen.getByRole('button', { name: 'Reset to default' })).toHaveProperty('disabled', true);
    fireEvent.input(html, { target: { value: '<p>hi</p>' } });
    fireEvent.click(saveButton(screen.getByRole('button', { name: 'Reset to default' })));
    await screen.findByText(/needs the button/);
    fireEvent.input(html, { target: { value: '<p>hi</p>{{cta}}' } });
    fireEvent.click(saveButton(screen.getByRole('button', { name: 'Reset to default' })));
    await findToast('Custom page saved');
    expect((await api.getPage(app.id)).html).toBe('<p>hi</p>{{cta}}');
    fireEvent.click(screen.getByRole('button', { name: 'Reset to default' }));
    expect(html.value).toContain('{{cta}}');
    fireEvent.click(saveButton(screen.getByRole('button', { name: 'Reset to default' })));
    await findToast('Default page saved');
    expect((await api.getPage(app.id)).html).toBe(''); // unchanged default stored as "", not a copy
    await api.deleteApp(app.id);
  });

  it('rotate key flow: new key is shown once', async () => {
    const app = await api.createApp(uniq('rotate-app'));
    render(<AppPage id={app.id} tab="settings" />);
    await screen.findByRole('heading', { name: 'API key' });

    fireEvent.click(screen.getByRole('button', { name: 'Rotate', exact: true }));
    const rotate = vi.spyOn(api, 'rotateKey');
    const submit = within(openDialog()).getByRole('button', { name: 'Rotate key' });
    fireEvent.click(submit);
    fireEvent.click(submit); // double click: a second rotate would kill the key about to be shown
    await screen.findByText('Key rotated');
    expect(rotate).toHaveBeenCalledTimes(1);
    rotate.mockRestore();
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
    await expect(api.getApp(app.id)).rejects.toMatchObject({ status: 404 });
  });

  it('app config flow: Settings tab saves iOS App ID, list row shows the iOS badge', async () => {
    const name = uniq('edit-app');
    const app = await api.createApp(name);
    render(<AppPage id={app.id} tab="settings" />);
    fireEvent.input(await screen.findByLabelText('iOS App ID'), {
      target: { value: 'ABCDE12345.com.example.app' },
    });
    fireEvent.click(saveButton(screen.getByLabelText('iOS App ID')));
    await screen.findByText('Saved.');
    expect((await api.getApp(app.id)).iosAppId).toBe('ABCDE12345.com.example.app');
    cleanup();

    // list: no Edit, row has badge
    render(<AppsPage />);
    const row = (await screen.findByText(name)).closest('tr')!;
    within(row).getByText('iOS', { exact: true });
    expect(within(row).queryByRole('button', { name: 'Edit' })).toBeNull();
    await api.deleteApp(app.id);
  });

  it('redirect profiles flow: a profile saved in Settings fills a new link', async () => {
    const app = await api.createApp(uniq('profiles-app'));
    render(<AppPage id={app.id} tab="settings" />);
    fireEvent.click(await screen.findByRole('button', { name: 'Add redirect preset' }));
    fireEvent.input(screen.getByLabelText('Redirect preset name'), { target: { value: 'Production' } });
    fireEvent.input(screen.getByLabelText('Preset iOS store'), { target: { value: 'https://apps.apple.com/app/id123' } });
    fireEvent.input(screen.getByLabelText('Preset Android store'), {
      target: { value: 'https://play.google.com/store/apps/details?id=com.example' },
    });
    fireEvent.input(screen.getByLabelText('Preset web fallback'), { target: { value: 'https://example.com' } });
    fireEvent.click(saveButton(screen.getByLabelText('Redirect preset name')));
    await screen.findByText('Saved.');
    expect((await api.getApp(app.id)).redirectProfiles).toEqual([
      {
        name: 'Production',
        ios: 'https://apps.apple.com/app/id123',
        android: 'https://play.google.com/store/apps/details?id=com.example',
        web: 'https://example.com',
      },
    ]);
    cleanup();

    render(<AppPage id={app.id} tab="links" />);
    fireEvent.click(await screen.findByRole('button', { name: 'New link' }));
    const dlg = openDialog();
    fireEvent.change(within(dlg).getByLabelText('Store and web redirect preset'), { target: { value: 'Production' } });
    expect((within(dlg).getByLabelText('iOS store page') as HTMLInputElement).value).toBe('https://apps.apple.com/app/id123');
    expect((within(dlg).getByLabelText('Android store page') as HTMLInputElement).value).toBe(
      'https://play.google.com/store/apps/details?id=com.example',
    );
    expect((within(dlg).getByLabelText('Web fallback') as HTMLInputElement).value).toBe('https://example.com');
    fireEvent.click(within(dlg).getByRole('button', { name: 'Cancel' }));
    await api.deleteApp(app.id);
  });

  it('social presets flow: a preview preset saved in Settings fills a new link', async () => {
    const app = await api.createApp(uniq('social-app'));
    render(<AppPage id={app.id} tab="settings" />);
    fireEvent.click(await screen.findByRole('button', { name: 'Add social preset' }));
    fireEvent.input(screen.getByLabelText('Social preset name'), { target: { value: 'Launch' } });
    fireEvent.input(screen.getByLabelText('Preview title'), { target: { value: 'Sam invited you' } });
    fireEvent.input(screen.getByLabelText('Preview description'), { target: { value: 'Join the sale' } });
    fireEvent.input(screen.getByLabelText('Preview image URL'), { target: { value: 'https://example.com/card.png' } });
    fireEvent.click(saveButton(screen.getByLabelText('Social preset name')));
    await screen.findByText('Saved.');
    expect((await api.getApp(app.id)).socialPresets).toEqual([
      { name: 'Launch', title: 'Sam invited you', description: 'Join the sale', image: 'https://example.com/card.png' },
    ]);
    cleanup();

    render(<AppPage id={app.id} tab="links" />);
    fireEvent.click(await screen.findByRole('button', { name: 'New link' }));
    const dlg = openDialog();
    fireEvent.change(within(dlg).getByLabelText('Social preview preset'), { target: { value: 'Launch' } });
    expect((within(dlg).getByLabelText('Title', { exact: true }) as HTMLInputElement).value).toBe('Sam invited you');
    expect((within(dlg).getByLabelText('Description', { exact: true }) as HTMLTextAreaElement).value).toBe('Join the sale');
    expect((within(dlg).getByLabelText('Image URL') as HTMLInputElement).value).toBe('https://example.com/card.png');
    fireEvent.click(within(dlg).getByRole('button', { name: 'Cancel' }));
    await api.deleteApp(app.id);
  });

  it('revoke key flow: key removed, status text shown', async () => {
    const app = await api.createApp(uniq('revoke-app'));
    render(<AppPage id={app.id} tab="settings" />);
    await screen.findByRole('heading', { name: 'API key' });

    const keyCard = screen.getByRole('button', { name: 'Rotate', exact: true }).closest('tr')!;
    fireEvent.click(within(keyCard).getByRole('button', { name: 'Delete' }));
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Delete' }));

    await screen.findByText(/Revoked. SDK calls are rejected./);
    expect((await api.getApp(app.id)).apiKeyHash).toBe('');
    await api.deleteApp(app.id);
  });

  it('edit link flow: url change persists to the table and the server', async () => {
    const app = await api.createApp(uniq('edit-link-app'));
    await api.createLink(app.id, {
      key: 'summer-sale',
      url: '/promo',
      ios: '',
      android: '',
      fallbackUrl: '',
    });
    render(<AppPage id={app.id} tab="links" />);
    const row = (await screen.findAllByText('summer-sale'))[0].closest('tr')!;

    fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    fireEvent.input(within(openDialog()).getByLabelText('Destination'), {
      target: { value: '/v2' },
    });
    await waitFor(() => expect((within(openDialog()).getByRole('button', { name: 'Save', exact: true }) as HTMLButtonElement).disabled).toBe(false)); // rules loaded
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Save', exact: true }));

    await waitFor(() => screen.getByText('/v2'));
    expect((await api.listLinks(app.id))[0].url).toBe('/v2');
    await api.deleteApp(app.id);
  });


  it('link draft round-trip: opening a fully set link leaves Save off; a title change saves and every other field stays', async () => {
    const app = await api.createApp(uniq('draft-link-app'));
    const full = {
      key: 'full-link',
      url: '/promo',
      title: 'Old title',
      tags: ['spring', 'alice'],
      ios: 'https://apps.apple.com/app/id123',
      android: 'https://play.google.com/store/apps/details?id=com.example',
      fallbackUrl: 'https://example.com',
      ogTitle: 'Share title',
      ogDescription: 'Share description',
      ogImage: 'https://example.com/og.png',
      expiresAt: '2030-01-01T12:34:00Z',
      expiredUrl: 'https://example.com/ended',
    };
    await api.createLink(app.id, full);
    render(<AppPage id={app.id} tab="links" />);
    const row = (await screen.findAllByText('full-link'))[0].closest('tr')!;

    fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    const dlg = openDialog();
    await waitFor(() => expect((within(dlg).getByRole('button', { name: 'Add rule' }) as HTMLButtonElement).disabled).toBe(false)); // rules loaded
    expect((within(dlg).getByRole('button', { name: 'Save', exact: true }) as HTMLButtonElement).disabled).toBe(true); // nothing changed

    // edit-mode validation: the shared helper blocks an empty destination and a non-https image
    const dest = within(dlg).getByLabelText('Destination');
    fireEvent.input(dest, { target: { value: '' } });
    fireEvent.click(within(dlg).getByRole('button', { name: 'Save', exact: true }));
    await within(dlg).findByText('Destination is required');
    fireEvent.input(dest, { target: { value: '/promo' } });
    const img = within(dlg).getByLabelText('Image URL');
    fireEvent.input(img, { target: { value: 'http://example.com/og.png' } });
    fireEvent.click(within(dlg).getByRole('button', { name: 'Save', exact: true }));
    await within(dlg).findByText('Social preview image must be an absolute https URL');
    fireEvent.input(img, { target: { value: full.ogImage } });

    fireEvent.input(within(dlg).getByLabelText(/Title \/ Note/), { target: { value: 'New title' } });
    await waitFor(() => expect((within(dlg).getByRole('button', { name: 'Save', exact: true }) as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(within(dlg).getByRole('button', { name: 'Save', exact: true }));

    await waitFor(async () => expect((await api.listLinks(app.id))[0].title).toBe('New title'));
    expect((await api.listLinks(app.id))[0]).toMatchObject({ ...full, title: 'New title' });
    await api.deleteApp(app.id);
  });

  it('rules and splits flow: open rules dialog, add rule with split, save, and verify', async () => {
    const app = await api.createApp(uniq('rules-app'));
    const link = await api.createLink(app.id, {
      key: 'split-link',
      url: '/base',
      ios: '',
      android: '',
      fallbackUrl: '',
    });
    render(<AppPage id={app.id} tab="links" />);
    const row = (await screen.findAllByText('split-link'))[0].closest('tr')!;

    fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    const dlg = openDialog();
    await within(dlg).findByText('No rules yet');

    fireEvent.click(within(dlg).getByRole('button', { name: 'Add rule' }));
    fireEvent.input(within(dlg).getByLabelText('Rule name'), {
      target: { value: 'Split Test 1' },
    });

    fireEvent.click(within(dlg).getByRole('button', { name: 'A/B split' }));
    fireEvent.click(within(dlg).getByRole('button', { name: 'Done' }));

    await within(dlg).findByText('Split Test 1');
    // leaving the editor moves focus to the next step, never onto Discard
    await waitFor(() => expect(document.activeElement).toBe(within(dlg).getByRole('button', { name: 'Save' })));

    fireEvent.click(within(dlg).getByRole('button', { name: 'Save' }));
    await within(dlg).findByText('Link saved');

    const storedRules = await api.getLinkRules(app.id, link.id);
    expect(storedRules).toHaveLength(1);
    expect(storedRules[0].name).toBe('Split Test 1');
    expect(storedRules[0].action.split).toHaveLength(2);

    // the row carries the rule beside the target, no need to open the drawer
    await waitFor(() => expect(within(document.querySelector('tbody tr .link-cell')!).getByText(/Split Test 1/)).toBeTruthy());

    // deleting the last rule still saves
    fireEvent.click(within(dlg).getByRole('button', { name: 'Delete' }));
    await within(dlg).findByText('No rules yet');
    fireEvent.click(within(dlg).getByRole('button', { name: 'Save' }));
    await within(dlg).findByText('Link saved');
    expect(await api.getLinkRules(app.id, link.id)).toHaveLength(0);

    fireEvent.click(within(dlg).getByRole('button', { name: 'Close' }));
    await api.deleteApp(app.id);
  });

  it('editing only link fields keeps existing rules', async () => {
    const app = await api.createApp(uniq('rules-keep'));
    const link = await api.createLink(app.id, { key: 'keep-link', url: '/base', ios: '', android: '', fallbackUrl: '' });
    await api.setLinkRules(app.id, link.id, [
      { name: 'Keep Me', cond: { platform: 'ios' }, action: { targets: { fallback_url: 'https://example.com/x' } } },
    ] as never);
    render(<AppPage id={app.id} tab="links" />);
    const row = (await screen.findAllByText('keep-link'))[0].closest('tr')!;
    fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    const dlg = openDialog();
    await within(dlg).findByText('Keep Me');
    fireEvent.input(within(dlg).getByLabelText('Title', { exact: true }), { target: { value: 'Only title' } });
    fireEvent.click(within(dlg).getByRole('button', { name: 'Save' }));
    await within(dlg).findByText('Link saved');
    expect((await api.getLinkRules(app.id, link.id)).map((r) => r.name)).toEqual(['Keep Me']);
    fireEvent.click(within(dlg).getByRole('button', { name: 'Close' }));
    await api.deleteApp(app.id);
  });

  it('rules dialog: Cancel in the editor, close, reopen still loads rules saved elsewhere', async () => {
    const app = await api.createApp(uniq('rules-cancel'));
    const link = await api.createLink(app.id, { key: 'cancel-link', url: '/base', ios: '', android: '', fallbackUrl: '' });
    render(<AppPage id={app.id} tab="links" />);
    const row = (await screen.findAllByText('cancel-link'))[0].closest('tr')!;

    fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    let dlg = openDialog();
    await within(dlg).findByText('No rules yet');
    fireEvent.click(within(dlg).getByRole('button', { name: 'Add rule' }));
    fireEvent.click(within(dlg).getByRole('button', { name: 'Cancel' }));
    fireEvent.click(within(dlg).getByRole('button', { name: 'Close' }));

    // another teammate saves a rule meanwhile
    await api.setLinkRules(app.id, link.id, [
      { name: 'Saved Elsewhere', cond: { platform: 'ios' }, action: { targets: { fallback_url: 'https://example.com/x' } } },
    ] as never);

    fireEvent.click(within(row).getByRole('button', { name: 'Edit' }));
    dlg = openDialog();
    await within(dlg).findByText('Saved Elsewhere');
    fireEvent.click(within(dlg).getByRole('button', { name: 'Close' }));
    await api.deleteApp(app.id);
  });

  it('link validation flow: empty fields rejected', async () => {
    const app = await api.createApp(uniq('link-val-app'));
    render(<AppPage id={app.id} tab="links" />);
    fireEvent.click(await screen.findByRole('button', { name: 'New link' }));
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Create', exact: true }));
    await screen.findByText('Key and destination are required');

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

    // fresh app: moded signals Tagged, Repeated device tag only, server-default thresholds
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
      velocityIpMax: 20,
      velocityLinkMax: 500,
      velocityWindowMinutes: 60,
      timingShortSeconds: 10,
      timingLongHours: 24,
      fingerprintMax: 5,
      fingerprintWindowDays: 7,
    });

    // one press = one save, no confirm dialog
    fireEvent.click(within(modeGroup('Bot traffic')).getByRole('button', { name: 'Active' }));
    expect(document.querySelector('dialog[open]')).toBeNull();
    const t = await findToast('Bot traffic: active');
    expect(t.title).toBe('');
    expect(pressed('Bot traffic')).toEqual(['Active']);
    expect(await api.getFraudSettings(app.id)).toMatchObject({
      userAgentMode: 'active',
      velocityMode: 'tagged',
      timingMode: 'tagged',
      ipMode: 'tagged',
      velocityIpMax: 20,
    });

    // re-press current mode: no save, no toast
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

    // changed elsewhere (another tab): Refresh shows server values
    await api.saveFraudSettings(app.id, { ...(await api.getFraudSettings(app.id)), velocityMode: 'active', velocityIpMax: 44 });
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    await waitFor(() => expect(pressed('Click flooding')).toEqual(['Active']));
    await waitFor(() => expect(threshold('Max clicks per IP').value).toBe('44'));

    // fresh mount reads server state
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

    // app deleted elsewhere (another tab): save fails server-side
    await api.deleteApp(app.id);
    fireEvent.click(within(modeGroup('Click flooding')).getByRole('button', { name: 'Active' }));
    const t = await findToast(/not found/i);
    expect(t.title).toBe('Not saved');
    await waitFor(() => expect(pressed('Click flooding')).toEqual(['Tagged']));
  });

  it('fraud thresholds flow: blur and Enter save changed values, unchanged or empty sends nothing, out of range reverts with "Not saved"', async () => {
    const app = await api.createApp(uniq('fraud-num'));
    location.hash = `#/apps/${app.id}/fraud`;
    render(<App />);
    await waitFor(() => expect(threshold('Max clicks per IP').value).toBe('20'));

    // blur w/ new value saves only that field
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

    // toast shows thousands separators
    await editAndBlur('Max clicks per link', '12000');
    await findToast('Click flooding: max clicks per link set to 12,000');
    expect((await api.getFraudSettings(app.id)).velocityLinkMax).toBe(12000);

    // unchanged or cleared: no save, no toast; cleared field shows saved value again
    let seen = lastToastId();
    await editAndBlur('Max clicks per IP', '30');
    await editAndBlur('Max installs', '');
    await settle();
    expect(toasts().filter((x) => x.id > seen)).toEqual([]);
    expect(threshold('Max installs').value).toBe('5');

    // out of range (server 2..10000): rejected, field reverts, server keeps saved value
    seen = lastToastId();
    await editAndBlur('Max clicks per IP', '1');
    const t = await findToast(/velocity IP max out of range/i);
    expect(t.title).toBe('Not saved');
    await waitFor(() => expect(threshold('Max clicks per IP').value).toBe('30'));
    expect(toasts().filter((x) => x.id > seen)).toHaveLength(1);
    expect(await api.getFraudSettings(app.id)).toMatchObject({
      velocityIpMax: 30,
      velocityLinkMax: 12000,
      timingShortSeconds: 5,
      fingerprintMax: 5,
    });

    // later save after rejection carries saved values, not the rejected one
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
    await waitFor(() => expect(currentApp()).toBe(a.name));
    await waitFor(() => expect(threshold('Max clicks per IP').value).toBe('33'));
    expect(pressed('Bot traffic')).toEqual(['Active']);

    go(`#/apps/${b.id}/fraud`);
    await waitFor(() => expect(currentApp()).toBe(b.name));
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
    await waitFor(() => expect(currentApp()).toBe(a.name));
    await waitFor(() => expect(threshold('Max clicks per IP').value).toBe('33'));
    expect(threshold('Max clicks per link').value).toBe('500');
    await api.deleteApp(a.id);
    await api.deleteApp(b.id);
  });

  it('flagged installs flow: a real fast install is tagged and listed; after turning timing active in the UI the next one is kept organic', async () => {
    const app = await api.createApp(uniq('fraud-view'));
    const key = uniq('fast');
    await api.createLink(app.id, { key, url: '/p', ios: 'https://apps.apple.com/app/id123', android: '', fallbackUrl: '' });
    location.hash = `#/apps/${app.id}/fraud`;
    render(<App />);
    const section = await screen.findByRole('region', { name: 'Flagged installs' });
    await within(section).findByText('Nothing flagged in this range.');
    expect(within(section).queryByLabelText('Flagged installs per signal')).toBeNull();

    // click -> launch within seconds: timing fires (tagged), install still attributed (IP 400 + iOS version 350 + timezone 200)
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

    // operator sets timing active on Fraud tab
    fireEvent.click(within(modeGroup('Suspicious timing')).getByRole('button', { name: 'Active' }));
    await findToast('Suspicious timing: active');
    expect((await api.getFraudSettings(app.id)).timingMode).toBe('active');

    // next fast install (another iPhone) loses credit: SDK gets no link, install organic w/ flagged click named
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

    // wider range: same two installs
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
    await waitFor(() => expect(currentApp()).toBe(app.name));
    expect(selectedTab()).toEqual(['Links']);

    go(`#/apps/${app.id}/settings`);
    await screen.findByRole('heading', { name: 'API key' });
    expect(selectedTab()).toEqual(['Settings']);

    // unknown tab segment -> app page, Links tab
    go(`#/apps/${app.id}/bogus`);
    await waitFor(() => expect(selectedTab()).toEqual(['Links']));
    await api.deleteApp(app.id);

    // bad %-encoding: no crash, falls back to apps page
    go('#/apps/%zz');
    await screen.findByRole('heading', { name: 'Apps' });
    expect(screen.queryByText('Something went wrong')).toBeNull();
  });

  it('AE1: ADMIN_PASSWORD unset — no admin entry, elevation inert, admin APIs open without a session', async () => {
    // adminSet false: no admin button or chip
    expect(await api.getConfig()).toMatchObject({ logoutUrl: '', adminSet: false });
    location.hash = '#/';
    render(<App />);
    await screen.findByRole('heading', { name: 'Apps' });
    expect(screen.queryByRole('button', { name: 'Enter admin' })).toBeNull();
    expect(screen.queryByText(/Admin · until/)).toBeNull();

    // nothing to elevate: POST rejects, GET stays inactive
    await expect(api.enterAdminMode('anything')).rejects.toMatchObject({ status: 403 });
    expect(await api.getAdminSession()).toMatchObject({ admin: false });

    // admin routes open without session (current behavior)
    const app = await api.createApp(uniq('ae1-app'));
    expect((await api.getApp(app.id)).id).toBe(app.id);
    await api.deleteApp(app.id);
  });

  it('API page without ADMIN_PASSWORD: sidebar link lands on the notice, no create form', async () => {
    location.hash = '#/';
    render(<App />);
    const side = within(screen.getByRole('navigation', { name: 'App sections' }));
    fireEvent.click(await side.findByRole('link', { name: 'API' }));
    go('#/api'); // happy-dom: link click does not move the hash
    await screen.findByText(/API tokens need an admin password/);
    expect(screen.queryByRole('button', { name: 'Create token' })).toBeNull();
    expect(side.getByRole('link', { name: 'API' }).getAttribute('aria-current')).toBe('page');
    expect(side.getByRole('link', { name: 'Apps' }).getAttribute('aria-current')).toBeNull();
  });
});
