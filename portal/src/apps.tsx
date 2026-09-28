import { useEffect, useState } from 'preact/hooks';
import { Alert, Badge, Button, Card, Dialog, Empty, Field, Input, Label, Spinner } from 'kinu';
import { api, App, CreatedApp } from './api';
import { closeDialog, ConfirmDelete, mono, muted, PageHeader, row } from './ui';

function generateKey(): string {
  const b = crypto.getRandomValues(new Uint8Array(16));
  return 'dk_' + Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
}

export function AppsPage() {
  const [apps, setApps] = useState<App[] | null>(null);
  const [error, setError] = useState('');
  const [name, setName] = useState('');
  const [key, setKey] = useState(generateKey());
  const [created, setCreated] = useState<CreatedApp | null>(null);
  const [copied, setCopied] = useState(false);

  const load = () => {
    api
      .listApps()
      .then(setApps)
      .catch((e) => setError(String(e)));
  };
  useEffect(load, []);

  // Runs when the trigger is clicked, before the dialog opens.
  const resetCreate = () => {
    setCreated(null);
    setCopied(false);
    setName('');
    setKey(generateKey());
  };

  const submitCreate = async () => {
    setError('');
    try {
      const app = await api.createApp(name.trim(), key.trim());
      setCreated(app); // show-once panel: plaintext key from this one response
      load();
    } catch (e) {
      setError(String(e));
    }
  };

  const copyKey = async () => {
    if (!created) return;
    await navigator.clipboard.writeText(created.apiKey);
    setCopied(true);
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
              This is the only time the API key for <strong>{created.name}</strong> is shown. Copy it now.
            </Alert>
            <Input readOnly value={created.apiKey} style={mono} />
            <div style={{ ...row, justifyContent: 'flex-end' }}>
              <Dialog.Close>
                <Button variant="outline">Done</Button>
              </Dialog.Close>
              <Button onClick={copyKey}>{copied ? 'Copied' : 'Copy key'}</Button>
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
              <p style={muted}>One app per mobile app. Its API key goes in the SDK config.</p>
            </div>
            <Field>
              <Label htmlFor="new-name">Name</Label>
              <Input id="new-name" value={name} onInput={(e) => setName(e.currentTarget.value)} placeholder="My app" />
            </Field>
            <Field>
              <Label htmlFor="new-key">API key</Label>
              <Input id="new-key" style={mono} value={key} onInput={(e) => setKey(e.currentTarget.value)} />
              <Field.Description>Auto-generated. Replace it to use your own.</Field.Description>
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
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(300px, 1fr))', gap: 16 }}>
          {apps.map((a) => (
            <Card key={a.id} style={{ display: 'grid', gap: 12 }}>
              <div style={{ ...row, justifyContent: 'space-between' }}>
                <h3 style={{ margin: 0 }}>
                  <a href={`#/apps/${a.id}`} style={{ color: 'inherit', textDecoration: 'none' }}>
                    {a.name}
                  </a>
                </h3>
                <div style={row}>
                  {a.iosAppId && <Badge variant="secondary">iOS</Badge>}
                  {a.androidPackage && <Badge variant="secondary">Android</Badge>}
                  {!a.iosAppId && !a.androidPackage && <Badge variant="outline">Not set up</Badge>}
                </div>
              </div>
              <div style={{ ...row, justifyContent: 'space-between' }}>
                <span style={{ ...muted, ...mono }}>key {a.apiKeyHash.slice(0, 12)}…</span>
                <div style={row}>
                  <EditDialog
                    key={a.id + '|' + a.iosAppId + '|' + a.androidPackage + '|' + a.androidCertFingerprint}
                    app={a}
                    onSaved={load}
                  />
                  <ConfirmDelete
                    title={`Delete ${a.name}?`}
                    body="Its links are deleted too, and the SDK stops accepting this API key."
                    onConfirm={() => del(a)}
                  />
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}

function EditDialog({ app, onSaved }: { app: App; onSaved: () => void }) {
  const [draft, setDraft] = useState({
    iosAppId: app.iosAppId,
    androidPackage: app.androidPackage,
    androidCertFingerprint: app.androidCertFingerprint,
  });
  const [err, setErr] = useState('');
  const id = `dlg-edit-app-${app.id}`;

  const save = async () => {
    setErr('');
    try {
      await api.updateApp(app.id, draft);
      closeDialog(id);
      onSaved();
    } catch (e) {
      setErr(String(e));
    }
  };

  return (
    <Dialog id={id}>
      <Dialog.Trigger>
        <Button size="sm" variant="ghost">
          Edit
        </Button>
      </Dialog.Trigger>
      <Dialog.Content>
        <form
          style={{ display: 'grid', gap: 16 }}
          onSubmit={(e) => {
            e.preventDefault();
            save();
          }}
        >
          <div style={{ display: 'grid', gap: 4 }}>
            <h2 style={{ margin: 0 }}>Edit {app.name}</h2>
            <p style={muted}>Used to serve the iOS and Android well-known files.</p>
          </div>
          <Field>
            <Label htmlFor={`${id}-ios`}>iOS App ID</Label>
            <Input
              id={`${id}-ios`}
              value={draft.iosAppId}
              onInput={(e) => setDraft({ ...draft, iosAppId: e.currentTarget.value })}
              placeholder="ABCDE12345.com.example.app"
            />
            <Field.Description>TEAMID.BUNDLEID</Field.Description>
          </Field>
          <Field>
            <Label htmlFor={`${id}-android`}>Android package</Label>
            <Input
              id={`${id}-android`}
              value={draft.androidPackage}
              onInput={(e) => setDraft({ ...draft, androidPackage: e.currentTarget.value })}
              placeholder="com.example.app"
            />
          </Field>
          <Field>
            <Label htmlFor={`${id}-cert`}>Android cert fingerprint</Label>
            <Input
              id={`${id}-cert`}
              style={mono}
              value={draft.androidCertFingerprint}
              onInput={(e) => setDraft({ ...draft, androidCertFingerprint: e.currentTarget.value })}
              placeholder="AA:BB:CC:…"
            />
            <Field.Description>SHA-256 of the signing certificate.</Field.Description>
          </Field>
          {err && <Alert variant="destructive">{err}</Alert>}
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Dialog.Close>
              <Button variant="outline">Cancel</Button>
            </Dialog.Close>
            <Button type="submit">Save</Button>
          </div>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}
