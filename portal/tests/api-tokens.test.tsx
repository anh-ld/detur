import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/preact';
import { App } from '../src/app';
import { setAdminAvailable, setElevated } from '../src/ui';
import { go, openDialog } from './integration';

// unit, mocked fetch: token routes (server side tested in Go). In-memory token store.
let tokens: any[];
let sent: { method: string; path: string; body: any }[];

const apps = [
  { id: 'a1', name: 'Alpha' },
  { id: 'b1', name: 'Bravo' },
];
const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }));
const tokenRow = (id: string, name: string, appIds: string[]) => ({
  id,
  name,
  prefix: 'dtr_abcd',
  appIds,
  createdAt: '2026-11-01T00:00:00Z',
  lastUsedAt: null,
});

function stubServer(adminSet: boolean) {
  vi.stubGlobal('fetch', (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET';
    const body = init?.body ? JSON.parse(init.body as string) : undefined;
    sent.push({ method, path: url, body });
    if (url === '/api/config') return json({ logoutUrl: '', adminSet, linkBase: 'https://go.example.com' });
    if (url === '/api/admin/session') return json({ admin: true, expiresAt: '2099-01-01T00:00:00Z' });
    if (url === '/api/apps') return json(apps);
    if (url === '/api/tokens' && method === 'GET') return json(tokens);
    if (url === '/api/tokens' && method === 'POST') {
      const t = tokenRow('t-new', body.name, body.appIds);
      tokens.push(t);
      return json({ ...t, token: 'dtr_SECRETSECRET' }, 201);
    }
    const m = url.match(/^\/api\/tokens\/(.+)$/);
    if (m && method === 'PATCH') {
      const t = tokens.find((x) => x.id === m[1]);
      Object.assign(t, body);
      return json(t);
    }
    if (m && method === 'DELETE') {
      tokens = tokens.filter((x) => x.id !== m[1]);
      return Promise.resolve(new Response(null, { status: 204 }));
    }
    return json({});
  });
}

beforeEach(() => {
  tokens = [];
  sent = [];
});
afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  setAdminAvailable(false);
  setElevated(false);
});

const side = () => within(screen.getByRole('navigation', { name: 'App sections' }));
const check = (scope: HTMLElement, name: string) => within(scope).getByLabelText(name) as HTMLInputElement;

it('AE4: ADMIN_PASSWORD unset, API page shows the notice and no create button', async () => {
  stubServer(false);
  go('#/');
  render(<App />);
  fireEvent.click(await side().findByRole('link', { name: 'API' }));
  go('#/api');
  await screen.findByText(/API tokens need an admin password/);
  expect(screen.queryByRole('button', { name: 'Create token' })).toBeNull();
  expect(side().getByRole('link', { name: 'API' }).getAttribute('aria-current')).toBe('page');
  expect(side().getByRole('link', { name: 'Apps' }).getAttribute('aria-current')).toBeNull();
});

it('create shows token + MCP config once; list shows prefix only; zero apps disables Create', async () => {
  stubServer(true);
  setAdminAvailable(true);
  setElevated(true, '2099-01-01T00:00:00Z');
  go('#/api');
  render(<App />);
  const create = await screen.findByRole('button', { name: 'Create token' });
  const form = (await screen.findByRole('button', { name: 'Create token' })).closest('form')!;
  await within(form).findByLabelText('Alpha');
  // how-to stays on the page: snippets with a placeholder, never a real token
  expect(screen.getByRole('heading', { name: 'Connect' })).toBeTruthy();
  expect(screen.getByText(/\/api\/mcp$/)).toBeTruthy();
  expect(screen.getByText('Authorization: Bearer <token>')).toBeTruthy();
  fireEvent.input(within(form).getByLabelText('Name'), { target: { value: 'Claude Code' } });
  expect((create as HTMLButtonElement).disabled).toBe(true);
  fireEvent.click(within(form).getByLabelText('Alpha'));
  await waitFor(() => expect((create as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(create);

  await waitFor(() => openDialog());
  const dlg = openDialog();
  expect(dlg.textContent).toContain("This token won't be shown again");
  const vals = [...dlg.querySelectorAll('input')].map((i) => i.value);
  expect(vals).toContain('dtr_SECRETSECRET');
  const cfg = JSON.parse(dlg.querySelector('pre')!.textContent!);
  expect(cfg.mcpServers.detur).toEqual({
    type: 'http',
    url: 'https://go.example.com/api/mcp',
    headers: { Authorization: 'Bearer dtr_SECRETSECRET' },
  });
  expect(sent.find((s) => s.method === 'POST')!.body).toEqual({ name: 'Claude Code', appIds: ['a1'] });

  fireEvent.click(within(dlg).getByRole('button', { name: 'Done' }));
  await waitFor(() => expect(document.body.innerHTML).not.toContain('dtr_SECRETSECRET'));
  const row = (await screen.findByText('Claude Code', { selector: 'td' })).closest('tr')!;
  expect(row.textContent).toContain('dtr_abcd');
  expect(row.textContent).toContain('Alpha');
  expect(row.textContent).toContain('Never');
});

it('edit {A,B} -> {B}; unchecking all disables Save; revoke removes the row after confirm', async () => {
  tokens = [tokenRow('t1', 'CI', ['a1', 'b1'])];
  stubServer(true);
  setAdminAvailable(true);
  setElevated(true, '2099-01-01T00:00:00Z');
  go('#/api');
  render(<App />);
  const tr = (await screen.findByText('CI', { selector: 'td' })).closest('tr')!;
  expect(tr.textContent).toContain('Alpha, Bravo');

  fireEvent.click(within(tr).getByRole('button', { name: 'Edit' }));
  const dlg = await waitFor(() => openDialog());
  expect(check(dlg, 'Alpha').checked).toBe(true);
  expect(check(dlg, 'Bravo').checked).toBe(true);
  fireEvent.click(check(dlg, 'Alpha'));
  fireEvent.click(check(dlg, 'Bravo'));
  await waitFor(() => expect((within(dlg).getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true));
  fireEvent.click(check(dlg, 'Bravo'));
  await waitFor(() => expect((within(dlg).getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(false));
  fireEvent.click(within(dlg).getByRole('button', { name: 'Save' }));
  const rowText = () => screen.getByText('CI', { selector: 'td' }).closest('tr')!.textContent!;
  await waitFor(() => expect(rowText()).not.toContain('Alpha'));
  expect(rowText()).toContain('Bravo');
  expect(sent.find((s) => s.method === 'PATCH')!.body).toEqual({ name: 'CI', appIds: ['b1'] });

  fireEvent.click(within(screen.getByText('CI', { selector: 'td' }).closest('tr')!).getByRole('button', { name: 'Revoke' }));
  const confirm = await waitFor(() => {
    const d = openDialog();
    if (!within(d).queryByText(/Anything using it stops/)) throw new Error('not revoke dialog');
    return d;
  });
  expect(screen.queryByText('CI', { selector: 'td' })).not.toBeNull(); // not yet
  fireEvent.click(within(confirm).getByRole('button', { name: 'Revoke' }));
  await waitFor(() => expect(screen.queryByText('CI', { selector: 'td' })).toBeNull());
  expect(sent.some((s) => s.method === 'DELETE' && s.path === '/api/tokens/t1')).toBe(true);
});
