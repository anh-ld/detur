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
import { clearJar, setJarCookie } from './setup';
import { go, openDialog, selectedTab, startServer, stopServer, tabs, uniq, waitForServer } from './integration';

const PORT = 8092;
const BASE = `http://127.0.0.1:${PORT}`;
const PW = 'test-admin-pw';
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

  it('settings tab as viewer: health and match quality render; gated saves prompt; the right password saves and lifts the chip', async () => {
    const app = await makeApp('viewer-settings');

    // direct render + shell's password dialog alongside
    render(
      <>
        <AdminPasswordDialog />
        <AppPage id={app.id} tab="settings" />
      </>,
    );
    await screen.findByText(/No SDK call yet/); // Health renders (viewer-safe)
    await screen.findByText('No installs yet.'); // Match quality renders (viewer-safe)

    // gated save prompts; cancel leaves viewer untouched
    fireEvent.click(screen.getByRole('button', { name: 'Save config' }));
    let dlg = await waitFor(pwDialog);
    fireEvent.click(within(dlg).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(document.querySelector('dialog[open]')).toBeNull());
    expect(screen.queryByText('Saved.')).toBeNull();
    expect(await api.getAdminSession()).toMatchObject({ admin: false });
    expect((await api.getApp(app.id)).iosAppId).toBe('');

    // right password: saves + lifts session
    fireEvent.click(screen.getByRole('button', { name: 'Save config' }));
    dlg = await waitFor(pwDialog);
    fireEvent.input(within(dlg).getByLabelText('Password'), { target: { value: PW } });
    fireEvent.click(within(dlg).getByRole('button', { name: 'Unlock' }));
    await screen.findByText('Saved.');
    expect(await api.getAdminSession()).toMatchObject({ admin: true });

    // shell shows chip for lifted session
    cleanup();
    location.hash = '#/';
    render(<App />);
    await screen.findByText(/Admin · until/);
    await api.deleteApp(app.id); // session is still valid
  });

  it('deep link flow: #/apps/{id}/settings as viewer stays on Settings; monitoring renders, no prompt', async () => {
    const app = await makeApp('deep-link');

    location.hash = '#/';
    render(<App />);
    await screen.findByRole('button', { name: 'Enter admin' }); // config + session loaded
    go(`#/apps/${app.id}/settings`);
    await screen.findByText(/No SDK call yet/); // Health (viewer-safe)
    await screen.findByText('No installs yet.'); // Match quality (viewer-safe)
    expect(location.hash).toBe(`#/apps/${app.id}/settings`);
    expect(selectedTab()).toEqual(['Settings']);
    expect(document.querySelector('dialog[open]')).toBeNull(); // only gated clicks prompt
    await adminCleanup(() => api.deleteApp(app.id));
  });

  it('fraud tab as viewer: the flagged report renders; the Signals section prompts instead of a raw 403', async () => {
    const app = await makeApp('fraud-viewer');

    location.hash = '#/';
    render(<App />);
    await screen.findByRole('button', { name: 'Enter admin' });
    go(`#/apps/${app.id}/fraud`);

    // viewer-open report loads; gated Signals load 403s -> password prompt
    const report = await screen.findByRole('region', { name: 'Flagged installs' });
    await within(report).findByText('Nothing flagged in this range.');
    await waitFor(pwDialog);

    // cancel: report stays, section locked, no raw 403 text
    fireEvent.click(within(openDialog()).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(document.querySelector('dialog[open]')).toBeNull());
    screen.getByText('Enter your admin password to configure signals.');
    expect(screen.queryByText(/\b403\b/)).toBeNull();
    await within(report).findByText('Nothing flagged in this range.');

    // Refresh after cancel: report reloads, section stays locked, no new prompt
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    await within(report).findByText('Nothing flagged in this range.');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Refresh' }).getAttribute('aria-busy')).not.toBe('true'));
    expect(document.querySelector('dialog[open]')).toBeNull();
    screen.getByText('Enter your admin password to configure signals.');
    await adminCleanup(() => api.deleteApp(app.id));
  });

  it('exit flow: the chip clears, the entry button returns, and gated saves prompt again', async () => {
    const app = await makeApp('exit-app');
    location.hash = '#/';
    render(<App />);
    await screen.findByRole('button', { name: 'Enter admin' });
    fireEvent.click(screen.getByRole('button', { name: 'Enter admin' }));
    let dlg = await waitFor(pwDialog);
    fireEvent.input(within(dlg).getByLabelText('Password'), { target: { value: PW } });
    fireEvent.click(within(dlg).getByRole('button', { name: 'Unlock' }));
    await screen.findByText(/Admin · until/);

    fireEvent.click(screen.getByRole('button', { name: 'Exit' }));
    await waitFor(() => expect(screen.queryByText(/Admin · until/)).toBeNull());
    await screen.findByRole('button', { name: 'Enter admin' });
    expect(await api.getAdminSession()).toMatchObject({ admin: false });

    // direct Settings render as viewer: saves prompt again
    cleanup();
    render(
      <>
        <AdminPasswordDialog />
        <AppPage id={app.id} tab="settings" />
      </>,
    );
    await screen.findByText(/No SDK call yet/);
    fireEvent.click(screen.getByRole('button', { name: 'Save config' }));
    dlg = await waitFor(pwDialog);
    fireEvent.click(within(dlg).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(document.querySelector('dialog[open]')).toBeNull());
    expect(screen.queryByText('Saved.')).toBeNull();
    await adminCleanup(() => api.deleteApp(app.id));
  });

  it('expired session mid-use: an admin call 403 clears the chip and re-prompts', async () => {
    // browser has live session, UI mid-session
    await api.enterAdminMode(PW);
    const app = await api.createApp(uniq('expired-app'));
    setElevated(true);
    location.hash = '#/';
    render(<App />);
    await screen.findByText(/Admin · until/);

    // server session dies (tampered cookie): every admin call 403s
    setJarCookie(`127.0.0.1:${PORT}`, 'detur_admin', 'tampered');

    // admin surface renders, UI still thinks lifted
    go(`#/apps/${app.id}/settings`);
    await screen.findByRole('heading', { name: 'API key' });
    fireEvent.click(screen.getByRole('button', { name: 'Save config' }));
    // 403 -> session refresh clears chip -> re-prompt
    await waitFor(() => expect(screen.queryByText(/Admin · until/)).toBeNull());
    const dlg = await waitFor(pwDialog);
    fireEvent.click(within(dlg).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(document.querySelector('dialog[open]')).toBeNull());
    expect(screen.queryByText(/\b403\b/)).toBeNull();
    clearJar(); // unlock jar: fresh session for cleanup
    await adminCleanup(() => api.deleteApp(app.id));
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
    await expect(api.getApp(app.id)).rejects.toThrow(/404/);
  });

  it('viewer admin call: a stale-lifted call that 403s routes to the prompt, not a raw error', async () => {
    await api.enterAdminMode(PW);
    const app = await api.createApp(uniq('direct-exp'));
    await api.exitAdminMode();
    setElevated(true); // UI thinks lifted, server session gone

    render(
      <>
        <AdminPasswordDialog />
        <AppPage id={app.id} tab="settings" />
      </>,
    );
    await screen.findByText(/No SDK call yet/);
    fireEvent.click(screen.getByRole('button', { name: 'Save matching' }));
    // no first prompt (module state = lifted): 403 -> re-prompt
    const dlg = await waitFor(pwDialog);
    fireEvent.click(within(dlg).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(document.querySelector('dialog[open]')).toBeNull());
    expect(screen.queryByText(/\b403\b/)).toBeNull();
    expect(screen.queryByText('Saved.')).toBeNull();
    expect((await api.getApp(app.id)).matchThreshold).toBe(850);

    // re-prompt recovers: right password retries the save (ui.tsx retry branch)
    fireEvent.click(screen.getByRole('button', { name: 'Save matching' }));
    const dlg2 = await waitFor(pwDialog);
    fireEvent.input(within(dlg2).getByLabelText('Password'), { target: { value: PW } });
    fireEvent.click(within(dlg2).getByRole('button', { name: 'Unlock' }));
    await screen.findByText('Saved.');
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
    await screen.findByText('No webhook endpoints configured for this app.');
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
});