import { useEffect, useState } from 'preact/hooks';
import { Alert, Badge, Button, Card, Dialog, Empty, Field, Input, Label, Spinner, Table } from 'kinu';
import { api, App, CreatedApp } from './api';
import { ConfirmDelete, CopyButton, mono, muted, PageHeader, row } from './ui';

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

  // Runs when trigger clicked, before dialog opens.
  const resetCreate = () => {
    setCreated(null);
    setName('');
  };

  const submitCreate = async () => {
    setError('');
    try {
      const app = await api.createApp(name.trim());
      setCreated(app); // show-once panel: plaintext key from this one response
      load();
    } catch (e) {
      setError(String(e));
    }
  };

  const del = async (a: App) => {
    setError('');
    try {
      await api.deleteApp(a.id);
      load();
    } catch (e) {
      setError(String(e));
    }
  };

  const createDialog = (
    <Dialog id="dlg-create-app">
      <Dialog.Trigger>
        <Button onClick={resetCreate}>Create app</Button>
      </Dialog.Trigger>
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
              <Dialog.Close>
                <Button variant="outline">Done</Button>
              </Dialog.Close>
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
              <Dialog.Close>
                <Button variant="outline">Cancel</Button>
              </Dialog.Close>
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
        <Spinner />
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
    </div>
  );
}
