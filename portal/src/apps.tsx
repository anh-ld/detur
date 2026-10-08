import { useEffect, useState } from 'preact/hooks';
import { Alert, Badge, Button, Card, Dialog, Empty, Field, Input, Label, Table } from 'kinu';
import { api, App, CreatedApp } from './api';
import { AdminGateAborted, adminCall, closeDialog, ConfirmDelete, CopyButton, gateOpen, Loading, mono, muted, PageHeader, row } from './ui';

const CREATE_ID = 'dlg-create-app';

export function AppsPage() {
  const [apps, setApps] = useState<App[] | null>(null);
  const [error, setError] = useState('');
  const [name, setName] = useState('');
  const [created, setCreated] = useState<CreatedApp | null>(null);

  const load = () => {
    api
      .listApps()
      .then(setApps)
      .catch((e) => setError(String(e)));
  };
  useEffect(load, []);

  // Runs when trigger clicked, before the dialog opens (elevation first when gated).
  const resetCreate = () => {
    setCreated(null);
    setName('');
  };
  const createClick = () => {
    resetCreate();
    gateOpen(CREATE_ID);
  };

  const submitCreate = async () => {
    setError('');
    try {
      const app = await adminCall(() => api.createApp(name.trim()));
      setCreated(app); // show-once panel: plaintext key from this one response
      load();
    } catch (e) {
      if (e instanceof AdminGateAborted) return;
      setError(String(e));
    }
  };

  const del = async (a: App) => {
    setError('');
    try {
      await adminCall(() => api.deleteApp(a.id));
      load();
    } catch (e) {
      if (e instanceof AdminGateAborted) return;
      setError(String(e));
    }
  };

  const createDialog = (
    <Dialog id="dlg-create-app">
      <Button onClick={createClick}>Create app</Button>
      <Dialog.Content>
        {created ? (
          <div style={{ display: 'grid', gap: 16 }}>
            <h2 style={{ margin: 0 }}>App created</h2>
            <Alert variant="warning">
              Copy both now — the API key is shown only once.
            </Alert>
            <Field>
              <Label>App ID</Label>
              <Input readOnly value={created.id} style={mono} />
              <Field.Description>EXPO_PUBLIC_DETOUR_APP_ID in the SDK config.</Field.Description>
            </Field>
            <Field>
              <Label>API key</Label>
              <Input readOnly value={created.apiKey} style={mono} />
              <Field.Description>EXPO_PUBLIC_DETOUR_API_KEY in the SDK config.</Field.Description>
            </Field>
            <div style={{ ...row, justifyContent: 'flex-end' }}>
              <Button variant="outline" onClick={() => closeDialog(CREATE_ID)}>
                Done
              </Button>
              <CopyButton value={created.id} label="Copy app ID" />
              <CopyButton value={created.apiKey} label="Copy API key" />
            </div>
          </div>
        ) : (
          <form
            style={{ display: 'grid', gap: 16 }}
            onSubmit={(e) => {
              e.preventDefault();
              submitCreate();
            }}
          >
            <div style={{ display: 'grid', gap: 4 }}>
              <h2 style={{ margin: 0 }}>Create app</h2>
              <p style={muted}>One app per mobile app. The server mints its API key.</p>
            </div>
            <Field>
              <Label htmlFor="new-name">Name</Label>
              <Input id="new-name" value={name} onInput={(e) => setName(e.currentTarget.value)} placeholder="My app" />
            </Field>
            {error && <Alert variant="destructive">{error}</Alert>}
            <div style={{ ...row, justifyContent: 'flex-end' }}>
              <Button type="button" variant="outline" onClick={() => closeDialog(CREATE_ID)}>
                Cancel
              </Button>
              <Button type="submit">Create</Button>
            </div>
          </form>
        )}
      </Dialog.Content>
    </Dialog>
  );

  return (
    <div>
      <PageHeader title="Apps" description="Each app has its own API key, links and install stats." actions={createDialog} />
      {error && <Alert variant="destructive">{error}</Alert>}
      {apps === null ? (
        <Loading />
      ) : apps.length === 0 ? (
        <Card>
          <Empty>
            <h3>No apps yet</h3>
            Create an app to get an API key for the SDK.
          </Empty>
        </Card>
      ) : (
        <Table>
          <thead>
            <tr>
              <th>Name</th>
              <th>App ID</th>
              <th>Platforms</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {apps.map((a) => (
              <tr
                key={a.id}
                class="row-link"
                // row click opens the app; buttons, links and the delete dialog keep their own
                onClick={(e) => {
                  if (!(e.target as Element).closest('a, button, dialog')) location.hash = `#/apps/${a.id}`;
                }}
              >
                <td>
                  <a href={`#/apps/${a.id}`} style={{ color: 'inherit', fontWeight: 600, textDecoration: 'none' }}>
                    {a.name}
                  </a>
                </td>
                <td data-label="App ID" style={{ ...muted, ...mono }}>
                  {a.id}
                </td>
                <td>
                  <div style={row}>
                    {a.iosAppId && <Badge variant="secondary">iOS</Badge>}
                    {a.androidPackage && <Badge variant="secondary">Android</Badge>}
                    {!a.iosAppId && !a.androidPackage && <Badge variant="outline">Not set up</Badge>}
                  </div>
                </td>
                <td>
                  <div style={{ ...row, justifyContent: 'flex-end' }}>
                    <ConfirmDelete
                      id={`dlg-del-${a.id}`}
                      requireAdmin
                      title={`Delete ${a.name}?`}
                      body="Its links are deleted too, and the SDK stops accepting this API key."
                      onConfirm={() => del(a)}
                    />
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      <footer style={{ ...row, justifyContent: 'center', marginTop: 48 }}>
        <a class="nav-link" href="https://github.com/anh-ld/detur" target="_blank" rel="noreferrer">
          <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
            <path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8z" />
          </svg>
          GitHub
        </a>
      </footer>
    </div>
  );
}
