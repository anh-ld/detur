import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { execSync, spawn, ChildProcess } from 'node:child_process';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/preact';
import { api, setApiBase } from '../src/api';
import { App } from '../src/app';
import { AppsPage } from '../src/apps';
import { DetailPage } from '../src/detail';
import { SettingsPage } from '../src/settings';

// Client-flow integration: real pages vs real Go binary + fresh SQLite. SDK :8080 hardcoded (must be free); portal on :8091.
const PORT = 8091;
const BASE = `http://127.0.0.1:${PORT}`;
const repoRoot = resolve(process.cwd(), '..'); // vitest runs from portal/
const bin = join(tmpdir(), 'detur-integ');

let server: ChildProcess;
let dbDir: string;
let stderr = '';

async function waitForServer(url: string, ms: number): Promise<void> {
  const start = Date.now();
  for (;;) {
    if (server.exitCode !== null) {
      throw new Error(`detur exited early (${server.exitCode}): ${stderr}`);
    }
    try {
      const res = await fetch(url);
      if (res.ok) return;
    } catch {
      // not up yet
    }
    if (Date.now() - start > ms) {
      throw new Error(`server not ready at ${url}: ${stderr}`);
    }
    await new Promise((r) => setTimeout(r, 300));
  }
}

// Open <dialog>: kinu shows one modal at a time.
const openDialog = (): HTMLElement => {
  const d = document.querySelector('dialog[open]');
  if (!d) throw new Error('no open dialog');
  return d as HTMLElement;
};

const uniq = (s: string) => `${s}-${Date.now().toString(36)}`;

// The same binary serves short links and the SDK API on :8080.
const SDK = 'http://127.0.0.1:8080';
const IPHONE = 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148 Safari/604.1';
const DESKTOP = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/537.36 Chrome/125.0 Safari/537.36';

async function click(key: string, ua: string, done = false): Promise<Response> {
  return fetch(`${SDK}/${key}${done ? '?_dt=1' : ''}`, { headers: { 'User-Agent': ua }, redirect: 'manual' });
}

// Analytics tile number by label (labels repeat in chart legends; tiles are the padding=sm cards).
const tileValue = (label: string): number => {
  const tile = screen
    .getAllByText(label, { exact: true })
    .map((e) => e.closest('[k=card][padding=sm]'))
    .find(Boolean)!;
  return parseInt(tile.querySelector(':scope > div')!.textContent!, 10);
};

beforeAll(async () => {
  execSync(`go build -o ${bin} ./cmd/detur`, { cwd: join(repoRoot, 'server'), stdio: 'inherit' });
  dbDir = mkdtempSync(join(tmpdir(), 'detur-integ-'));
  server = spawn(bin, ['-portal-addr', `127.0.0.1:${PORT}`], {
    cwd: repoRoot,
    env: { ...process.env, DB_PATH: join(dbDir, 'test.db') },
    stdio: ['ignore', 'ignore', 'pipe'],
  });
  server.stderr!.on('data', (d: Buffer) => {
    stderr += d.toString();
  });
  await waitForServer(`${BASE}/api/apps`, 60_000);
  setApiBase(BASE);
});

afterAll(() => {
  setApiBase('');
  server?.kill();
  if (dbDir) rmSync(dbDir, { recursive: true, force: true });
  rmSync(bin, { force: true });
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

  it('detail flow: create a link, then delete it', async () => {
    const app = await api.createApp(uniq('detail-app'));
    render(<DetailPage id={app.id} />);
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

  it('analytics flow: real clicks and SDK events reach tiles, links and top events; platform filter narrows', async () => {
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

    render(<DetailPage id={app.id} />);
    await waitFor(() => expect(tileValue('Clicks')).toBe(2));
    expect(tileValue('Web fallbacks')).toBe(1);
    expect(tileValue('Installs via link')).toBe(0);
    expect(tileValue('Already installed opens')).toBe(0);
    const linkClicks = () => screen.getByText(key).closest('tr')!.querySelector('[data-label="Clicks"]')!.textContent;
    expect(linkClicks()).toBe('2');
    expect(screen.getByText('purchase').closest('tr')!.querySelector('[data-label="Count"]')!.textContent).toBe('1');

    fireEvent.change(screen.getByLabelText('Platform'), { target: { value: 'ios' } });
    await waitFor(() => expect(tileValue('Clicks')).toBe(1));
    expect(tileValue('Web fallbacks')).toBe(0);
    expect(linkClicks()).toBe('1');
    await api.deleteApp(app.id);
  });

  it('matching flow: save persists to the server, empty fields are rejected', async () => {
    const app = await api.createApp(uniq('m'));
    render(<SettingsPage id={app.id} />);
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
    await api.deleteApp(app.id);
  });

  it('rotate key flow: new key is shown once', async () => {
    const app = await api.createApp(uniq('rotate-app'));
    render(<SettingsPage id={app.id} />);
    await screen.findByRole('heading', { name: 'Settings' });

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

  it('app config flow: settings page saves iOS App ID, list row shows the iOS badge', async () => {
    const name = uniq('edit-app');
    const app = await api.createApp(name);
    render(<SettingsPage id={app.id} />);
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
    render(<SettingsPage id={app.id} />);
    await screen.findByRole('heading', { name: 'Settings' });

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
    render(<DetailPage id={app.id} />);
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
    render(<DetailPage id={app.id} />);
    await screen.findByRole('heading', { name: app.name });

    fireEvent.click(screen.getByRole('button', { name: 'New link' }));
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Create', exact: true }));
    await screen.findByText('key and url are required');

    expect(await api.listLinks(app.id)).toHaveLength(0);

    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Cancel' }));
    await api.deleteApp(app.id);
  });

  it('app not found flow: unknown id shows the recovery card', async () => {
    render(<DetailPage id="does-not-exist" />);
    await screen.findByText('App not found');
    screen.getByRole('link', { name: 'Back to apps' });
  });

  it('shell flow: hash routes reach apps, detail and settings; malformed hash falls back to apps', async () => {
    const app = await api.createApp(uniq('shell-app'));
    render(<App />);
    await screen.findByRole('heading', { name: 'Apps' });

    location.hash = `#/apps/${app.id}`;
    window.dispatchEvent(new Event('hashchange'));
    await screen.findByRole('heading', { name: app.name });

    location.hash = `#/apps/${app.id}/settings`;
    window.dispatchEvent(new Event('hashchange'));
    await screen.findByRole('heading', { name: 'Settings' });
    await api.deleteApp(app.id);

    // malformed percent-encoding: crash-safe fallback to the apps page
    location.hash = '#/apps/%zz';
    window.dispatchEvent(new Event('hashchange'));
    await screen.findByRole('heading', { name: 'Apps' });
    expect(screen.queryByText('Something went wrong')).toBeNull();
  });
});