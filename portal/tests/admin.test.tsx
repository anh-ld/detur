// gated admin flows: real Go binary w/ ADMIN_PASSWORD set. Unset-password suite: flows.test.tsx.
// fileParallelism: false + per-file server -> :8080 SDK port and portal ports never collide.
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest';
import { ChildProcess } from 'node:child_process';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/preact';
import { api, setApiBase } from '../src/api';
import { App } from '../src/app';
import { AppsPage } from '../src/apps';
import { AppPage } from '../src/detail';
import { setAdminAvailable, setElevated, AdminPasswordDialog } from '../src/ui';
import { clearJar, setJarCookie } from './_setup';
import { go, openDialog, saveButton, selectedTab, startServer, stopServer, tabs, uniq, waitForServer } from './_integration';

const PORT = 8092;
const BASE = `http://127.0.0.1:${PORT}`;
const PW = 'test-admin-pw';
const SETTINGS_LOCKED = 'Enter your admin password to view and change settings.';
const SIGNALS_LOCKED = 'Enter your admin password to configure signals.';

// fill + submit the open password prompt
async function unlock() {
  const dlg = await waitFor(pwDialog);
  fireEvent.input(within(dlg).getByLabelText('Password'), { target: { value: PW } });
  fireEvent.click(within(dlg).getByRole('button', { name: 'Unlock' }));
}
const bin = join(tmpdir(), 'detur-admin-integ');

let server: ChildProcess;
let dbDir: string;
let getStderr: () => string = () => '';

// password prompt; guard against grabbing another dialog
const pwDialog = (): HTMLElement => {
  const d = openDialog();
  if (!within(d).queryByLabelText('Password')) throw new Error('not the password dialog');
  return d;
};

// create app via API, end session: each test starts as viewer
async function makeApp(prefix: string): Promise<{ id: string; name: string }> {
  await api.enterAdminMode(PW);
  const app = await api.createApp(uniq(prefix));
  await api.exitAdminMode();
  return app;
}

const adminCleanup = async (fn: () => Promise<void>) => {
  await api.enterAdminMode(PW);
  await fn();
  await api.exitAdminMode();
};

beforeAll(async () => {
  const s = startServer({ port: PORT, bin, env: { ADMIN_PASSWORD: PW } });
  server = s.server;
  dbDir = s.dbDir;
  getStderr = s.getStderr;
  await waitForServer(`${BASE}/api/apps`, 60_000, server, getStderr);
  setApiBase(BASE);
  setAdminAvailable(true); // config says adminSet true in this suite
});

afterAll(() => {
  setApiBase('');
  setAdminAvailable(false);
  setElevated(false);
  stopServer(server, dbDir, bin);
});

afterEach(() => {
  cleanup();
  clearJar();
  setElevated(false);
});

describe('portal admin flows (gated)', () => {
  it('admin entry flow: wrong password shows an error and stays viewer; the right password lifts the chip', async () => {
    location.hash = '#/';
    render(<App />);
    await screen.findByRole('button', { name: 'Enter admin' });

    fireEvent.click(screen.getByRole('button', { name: 'Enter admin' }));
    let dlg = await waitFor(pwDialog);
    fireEvent.input(within(dlg).getByLabelText('Password'), { target: { value: 'wrong-pw' } });
    fireEvent.click(within(dlg).getByRole('button', { name: 'Unlock' }));
    // wrong password: 401 (server sleeps 1s), error alert, still viewer
    await waitFor(() => within(dlg).getByRole('alert'), { timeout: 5000 });
    expect(within(dlg).getByRole('alert').textContent).toBe('Wrong password');
    expect(screen.queryByText(/Admin · until/)).toBeNull();
    expect(await api.getAdminSession()).toMatchObject({ admin: false });

    fireEvent.input(within(dlg).getByLabelText('Password'), { target: { value: PW } });
    fireEvent.click(within(dlg).getByRole('button', { name: 'Unlock' }));
    await waitFor(() => screen.getByText(/Admin · until/));
    expect(await api.getAdminSession()).toMatchObject({ admin: true });

    // exit + re-enter: prompt opens empty, no old error
    fireEvent.click(screen.getByRole('button', { name: 'Exit' }));
    await screen.findByRole('button', { name: 'Enter admin' });
    fireEvent.click(screen.getByRole('button', { name: 'Enter admin' }));
    dlg = await waitFor(pwDialog);
    expect((within(dlg).getByLabelText('Password') as HTMLInputElement).value).toBe('');
    expect(within(dlg).queryByRole('alert')).toBeNull();
    fireEvent.click(within(dlg).getByRole('button', { name: 'Cancel' }));
  });

  it('settings tab as viewer: locked notice only, no prompt; unlock renders the panel and saves', async () => {
    const app = await makeApp('viewer-settings');

    // direct render + shell's password dialog alongside
    render(
      <>
        <AdminPasswordDialog />
        <AppPage id={app.id} tab="settings" />
      </>,
    );
    await screen.findByText(SETTINGS_LOCKED);
    expect(document.querySelector('dialog[open]')).toBeNull();
    expect(screen.queryByText(/No SDK call yet/)).toBeNull();
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();

    // unlock: panel renders, save goes straight through
    fireEvent.click(screen.getByRole('button', { name: 'Unlock admin' }));
    await unlock();
    await screen.findByText(/No SDK call yet/);
    expect(screen.queryByText(SETTINGS_LOCKED)).toBeNull();
    fireEvent.click(saveButton(screen.getByLabelText('iOS App ID')));
    await screen.findByText('Saved.');
    expect(await api.getAdminSession()).toMatchObject({ admin: true });

    // shell shows chip for lifted session
    cleanup();
    location.hash = '#/';
    render(<App />);
    await screen.findByText(/Admin · until/);
    await api.deleteApp(app.id); // session is still valid
  });

  it('deep link flow: #/apps/{id}/settings as viewer stays on Settings; locked notice, no prompt', async () => {
    const app = await makeApp('deep-link');

    location.hash = '#/';
    render(<App />);
    await screen.findByRole('button', { name: 'Enter admin' }); // config + session loaded
    go(`#/apps/${app.id}/settings`);
    await screen.findByText(SETTINGS_LOCKED);
    expect(location.hash).toBe(`#/apps/${app.id}/settings`);
    expect(selectedTab()).toEqual(['Settings']);
    expect(document.querySelector('dialog[open]')).toBeNull();
    await adminCleanup(() => api.deleteApp(app.id));
  });

  it('fraud tab as viewer: the flagged report renders; Signals shows the locked notice, no prompt', async () => {
    const app = await makeApp('fraud-viewer');

    location.hash = '#/';
    render(<App />);
    await screen.findByRole('button', { name: 'Enter admin' });
    go(`#/apps/${app.id}/fraud`);

    const report = await screen.findByRole('region', { name: 'Flagged installs' });
    await within(report).findByText('Nothing flagged in this range.');
    await screen.findByText(SIGNALS_LOCKED);
    expect(document.querySelector('dialog[open]')).toBeNull();
    expect(screen.queryByText(/\b403\b/)).toBeNull();

    // Refresh: report reloads, section stays locked, still no prompt
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    await within(report).findByText('Nothing flagged in this range.');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Refresh' }).getAttribute('aria-busy')).not.toBe('true'));
    expect(document.querySelector('dialog[open]')).toBeNull();
    screen.getByText(SIGNALS_LOCKED);
    await adminCleanup(() => api.deleteApp(app.id));
  });

  it('fraud tab: unlocking from the Signals notice loads signals; a mode press saves with no prompt', async () => {
    const app = await makeApp('fraud-unlock');
    location.hash = '#/';
    render(<App />);
    await screen.findByRole('button', { name: 'Enter admin' });
    go(`#/apps/${app.id}/fraud`);
    await screen.findByText(SIGNALS_LOCKED);

    fireEvent.click(screen.getByRole('button', { name: 'Unlock admin' }));
    await unlock();
    const group = await screen.findByRole('group', { name: 'Bot traffic mode' });
    expect(screen.queryByText(SIGNALS_LOCKED)).toBeNull();
    await screen.findByText(/Admin · until/);

    fireEvent.click(within(group).getByRole('button', { name: 'Active' }));
    await waitFor(async () => expect((await api.getFraudSettings(app.id)).userAgentMode).toBe('active'));
    expect(document.querySelector('dialog[open]')).toBeNull();
    await api.deleteApp(app.id); // session is valid
  });

  it('fraud tab with a dead session: Signals locks, the chip clears, no prompt', async () => {
    await api.enterAdminMode(PW);
    const app = await api.createApp(uniq('fraud-dead'));
    setElevated(true);
    location.hash = '#/';
    render(<App />);
    await screen.findByText(/Admin · until/);

    setJarCookie(`127.0.0.1:${PORT}`, 'detur_admin', 'tampered');
    go(`#/apps/${app.id}/fraud`);
    await screen.findByText(SIGNALS_LOCKED);
    await waitFor(() => expect(screen.queryByText(/Admin · until/)).toBeNull());
    await screen.findByRole('button', { name: 'Enter admin' });
    expect(document.querySelector('dialog[open]')).toBeNull();
    expect(screen.queryByText(/\b403\b/)).toBeNull();
    clearJar();
    await adminCleanup(() => api.deleteApp(app.id));
  });

  it('exit flow: the chip clears, the entry button returns, and Settings locks again', async () => {
    const app = await makeApp('exit-app');
    location.hash = '#/';
    render(<App />);
    await screen.findByRole('button', { name: 'Enter admin' });
    fireEvent.click(screen.getByRole('button', { name: 'Enter admin' }));
    const dlg = await waitFor(pwDialog);
    fireEvent.input(within(dlg).getByLabelText('Password'), { target: { value: PW } });
    fireEvent.click(within(dlg).getByRole('button', { name: 'Unlock' }));
    await screen.findByText(/Admin · until/);

    fireEvent.click(screen.getByRole('button', { name: 'Exit' }));
    await waitFor(() => expect(screen.queryByText(/Admin · until/)).toBeNull());
    await screen.findByRole('button', { name: 'Enter admin' });
    expect(await api.getAdminSession()).toMatchObject({ admin: false });

    // direct Settings render as viewer: locked again
    cleanup();
    render(
      <>
        <AdminPasswordDialog />
        <AppPage id={app.id} tab="settings" />
      </>,
    );
    await screen.findByText(SETTINGS_LOCKED);
    expect(screen.queryByRole('button', { name: 'Save' })).toBeNull();
    await adminCleanup(() => api.deleteApp(app.id));
  });

  it('expired session mid-use: an admin call 403 clears the chip, hides Settings, re-prompts; the draft survives', async () => {
    // browser has live session, UI mid-session
    await api.enterAdminMode(PW);
    const app = await api.createApp(uniq('expired-app'));
    setElevated(true);
    location.hash = '#/';
    render(<App />);
    await screen.findByText(/Admin · until/);
    go(`#/apps/${app.id}/settings`);
    await screen.findByLabelText('iOS App ID'); // gated panel loaded

    // server session dies (tampered cookie) after the panel loaded: every admin call 403s
    setJarCookie(`127.0.0.1:${PORT}`, 'detur_admin', 'tampered');
    fireEvent.input(screen.getByLabelText('iOS App ID'), { target: { value: 'ABCDE12345.com.draft' } });
    fireEvent.click(saveButton(screen.getByLabelText('iOS App ID')));
    // 403 -> session refresh clears chip -> panel hides behind the notice + re-prompt
    await waitFor(() => expect(screen.queryByText(/Admin · until/)).toBeNull());
    const dlg = await waitFor(pwDialog);
    screen.getByText(SETTINGS_LOCKED);
    expect(screen.queryByRole('heading', { name: 'API key' })).toBeNull();
    fireEvent.click(within(dlg).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(document.querySelector('dialog[open]')).toBeNull());
    expect(screen.queryByText(/\b403\b/)).toBeNull();
    clearJar(); // drop the tampered cookie so the unlock gets a fresh session
    expect((await api.getApp(app.id)).iosAppId).toBe('');

    // unlock from the notice: panel back, unsaved draft kept, save goes through
    fireEvent.click(screen.getByRole('button', { name: 'Unlock admin' }));
    await unlock();
    await screen.findByRole('heading', { name: 'API key' });
    expect(screen.queryByText(SETTINGS_LOCKED)).toBeNull();
    expect((screen.getByLabelText('iOS App ID') as HTMLInputElement).value).toBe('ABCDE12345.com.draft');
    fireEvent.click(saveButton(screen.getByLabelText('iOS App ID')));
    await screen.findByText('Saved.');
    expect((await api.getApp(app.id)).iosAppId).toBe('ABCDE12345.com.draft');
    await api.deleteApp(app.id); // session is valid now
  });

  it('delete app flow as viewer: password first, the ConfirmDelete still confirms, the row disappears', async () => {
    const app = await makeApp('del-viewer');

    render(
      <>
        <AdminPasswordDialog />
        <AppsPage />
      </>,
    );
    const card = (await screen.findByText(app.name)).closest('tr')!;
    fireEvent.click(within(card).getByRole('button', { name: 'Delete' }));
    // viewer: password prompt before confirm
    let dlg = await waitFor(pwDialog);
    fireEvent.input(within(dlg).getByLabelText('Password'), { target: { value: PW } });
    fireEvent.click(within(dlg).getByRole('button', { name: 'Unlock' }));
    // gate opens the confirm dialog, which still asks
    dlg = await waitFor(() => {
      const d = openDialog();
      if (!within(d).queryByRole('heading', { name: `Delete ${app.name}?` })) throw new Error('not the confirm dialog');
      return d;
    });
    fireEvent.click(within(dlg).getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(screen.queryByText(app.name)).toBeNull());
    await expect(api.getApp(app.id)).rejects.toMatchObject({ status: 404 });
  });

  it('settings with a dead session on arrival: the page load 403 locks the panel, no prompt, no raw error', async () => {
    const app = await makeApp('dead-arrival');
    setElevated(true); // UI thinks lifted, server session gone
    render(
      <>
        <AdminPasswordDialog />
        <AppPage id={app.id} tab="settings" />
      </>,
    );
    await screen.findByText(SETTINGS_LOCKED);
    expect(screen.queryByRole('heading', { name: 'API key' })).toBeNull();
    expect(document.querySelector('dialog[open]')).toBeNull();
    expect(screen.queryByText(/\b403\b/)).toBeNull();
    await adminCleanup(() => api.deleteApp(app.id));
  });

  it('session dies mid-save: the 403 re-prompts, the right password retries the save into the same form', async () => {
    await api.enterAdminMode(PW);
    const app = await api.createApp(uniq('direct-exp'));
    setElevated(true);
    render(
      <>
        <AdminPasswordDialog />
        <AppPage id={app.id} tab="settings" />
      </>,
    );
    await screen.findByLabelText('iOS App ID'); // gated panel loaded
    await api.exitAdminMode(); // server session gone, UI still lifted

    fireEvent.click(screen.getByRole('tab', { name: 'Matching' }));
    fireEvent.input(screen.getByLabelText('Match threshold'), { target: { value: '900' } });
    fireEvent.click(saveButton(screen.getByLabelText('Match threshold')));
    // no first prompt (module state = lifted): 403 -> session re-read locks the panel -> re-prompt
    await waitFor(pwDialog);
    screen.getByText(SETTINGS_LOCKED);
    expect(screen.queryByText(/\b403\b/)).toBeNull();

    // right password: the save retries (ui.tsx retry branch); the same form shows the result
    await unlock();
    await screen.findByText('Saved.');
    expect(screen.queryByText(SETTINGS_LOCKED)).toBeNull();
    expect((screen.getByLabelText('Match threshold') as HTMLInputElement).value).toBe('900');
    expect((await api.getApp(app.id)).matchThreshold).toBe(900);
    expect(await api.getAdminSession()).toMatchObject({ admin: true });
    await api.deleteApp(app.id); // session is valid now
  });

  it('webhooks tab as viewer prompts for elevation; elevated admin can create and manage webhooks', async () => {
    const app = await makeApp('wh-flow');
    location.hash = '#/';
    render(<App />);
    await screen.findByRole('button', { name: 'Enter admin' });
    go(`#/apps/${app.id}/webhooks`);

    // viewer: shared admin-required notice
    await screen.findByText('Admin required');
    expect(selectedTab()).toEqual(['Webhooks']);

    fireEvent.click(screen.getByRole('button', { name: 'Unlock admin' }));
    const dlg = await waitFor(pwDialog);
    fireEvent.input(within(dlg).getByLabelText('Password'), { target: { value: PW } });
    fireEvent.click(within(dlg).getByRole('button', { name: 'Unlock' }));

    // elevated: empty state + Add webhook
    await screen.findByText('No webhooks yet');
    expect(screen.getByRole('button', { name: 'Add webhook' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Add webhook' }));
    const addDlg = await waitFor(() => {
      const d = openDialog();
      if (!within(d).queryByLabelText('Destination URL')) throw new Error('not add webhook dialog');
      return d;
    });
    fireEvent.input(within(addDlg).getByLabelText('Destination URL'), { target: { value: 'https://example.com/detur-hook' } });
    fireEvent.click(within(addDlg).getByRole('button', { name: 'Create Webhook' }));

    const secretDlg = await waitFor(() => {
      const d = openDialog();
      if (!within(d).queryByText('Webhook Created')) throw new Error('not created secret dialog');
      return d;
    });
    fireEvent.click(within(secretDlg).getByRole('button', { name: 'Done' }));
    await waitFor(() => expect(document.querySelector('dialog[open]')).toBeNull());

    await screen.findByText('https://example.com/detur-hook');
    await screen.findByText('Active');

    await adminCleanup(() => api.deleteApp(app.id));
  });

  it('API tokens: viewer unlocks, creates a token (shown once with MCP config), edits its apps, revokes it', async () => {
    const a = await makeApp('tok-a');
    const b = await makeApp('tok-b');
    go('#/api');
    render(<App />);
    await screen.findByText('Admin required');
    // locked: no form, no setup guide
    expect(screen.queryByRole('button', { name: 'Create token' })).toBeNull();
    expect(screen.queryByRole('heading', { name: 'Connect' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Unlock admin' }));
    await unlock();

    const create = (await screen.findByRole('button', { name: 'Create token' })) as HTMLButtonElement;
    const form = create.closest('form')!;
    // setup guide stays on the page with a placeholder, never a real token
    screen.getByRole('heading', { name: 'Connect' });
    screen.getByText('Authorization: Bearer <token>');
    fireEvent.input(within(form).getByLabelText('Name'), { target: { value: 'ci-agent' } });
    expect(create.disabled).toBe(true); // no app picked
    fireEvent.click(await within(form).findByLabelText(a.name));
    fireEvent.click(within(form).getByLabelText(b.name));
    await waitFor(() => expect(create.disabled).toBe(false));
    fireEvent.click(create);

    const dlg = await waitFor(() => {
      const d = openDialog();
      if (!within(d).queryByText(/won't be shown again/)) throw new Error('not the created dialog');
      return d;
    });
    const token = [...dlg.querySelectorAll('input')].map((i) => i.value).find((v) => v.startsWith('dtr_'))!;
    expect(token).toBeTruthy();
    const cfg = JSON.parse(dlg.querySelector('pre')!.textContent!).mcpServers.detur;
    expect(cfg.url).toMatch(/\/api\/mcp$/);
    expect(cfg.headers).toEqual({ Authorization: `Bearer ${token}` });
    fireEvent.click(within(dlg).getByRole('button', { name: 'Done' }));
    await waitFor(() => expect(document.body.innerHTML).not.toContain(token));

    const row = () => screen.getByText('ci-agent', { selector: 'td' }).closest('tr')!;
    await screen.findByText('ci-agent', { selector: 'td' });
    expect(row().textContent).toContain(token.slice(0, 8));
    expect(row().textContent).toContain(a.name);
    expect(row().textContent).toContain(b.name);

    fireEvent.click(within(row()).getByRole('button', { name: 'Edit' }));
    const edit = await waitFor(() => {
      const d = openDialog();
      if (!within(d).queryByRole('button', { name: 'Save' })) throw new Error('not the edit dialog');
      return d;
    });
    const save = within(edit).getByRole('button', { name: 'Save' }) as HTMLButtonElement;
    fireEvent.click(within(edit).getByLabelText(a.name));
    fireEvent.click(within(edit).getByLabelText(b.name));
    await waitFor(() => expect(save.disabled).toBe(true)); // no app left
    fireEvent.click(within(edit).getByLabelText(b.name));
    await waitFor(() => expect(save.disabled).toBe(false));
    fireEvent.click(save);
    await waitFor(() => expect(row().textContent).not.toContain(a.name));
    expect(row().textContent).toContain(b.name);
    expect((await api.listTokens()).find((t) => t.name === 'ci-agent')!.appIds).toEqual([b.id]);

    fireEvent.click(within(row()).getByRole('button', { name: 'Revoke' }));
    const confirm = await waitFor(() => {
      const d = openDialog();
      if (!within(d).queryByText(/Anything using it stops/)) throw new Error('not the revoke dialog');
      return d;
    });
    fireEvent.click(within(confirm).getByRole('button', { name: 'Revoke' }));
    await waitFor(() => expect(screen.queryByText('ci-agent', { selector: 'td' })).toBeNull());
    expect((await api.listTokens()).some((t) => t.name === 'ci-agent')).toBe(false);

    await adminCleanup(async () => {
      await api.deleteApp(a.id);
      await api.deleteApp(b.id);
    });
  });
});
