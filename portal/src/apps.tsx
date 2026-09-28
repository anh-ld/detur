import { useEffect, useState } from 'preact/hooks';
import { Button, Dialog, Input, Label } from 'kinu';
import { api, App, CreatedApp } from './api';
import { closeDialog } from './ui';

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
    if (!confirm(`Delete app "${a.name}"? Its links are deleted too.`)) return;
    setError('');
    try {
      await api.deleteApp(a.id);
      load();
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <div>
      <div class="page-head">
        <h1>Apps</h1>
        <Dialog id="dlg-create-app">
          <Dialog.Trigger>
            <Button onClick={resetCreate}>Create app</Button>
          </Dialog.Trigger>
          <Dialog.Content>
            {created ? (
              <div>
                <h2>App created</h2>
                <p>
                  API key for <strong>{created.name}</strong> — shown once. Copy it now.
                </p>
                <div class="key mono">{created.apiKey}</div>
                <div class="row">
                  <Button onClick={copyKey}>{copied ? 'Copied' : 'Copy key'}</Button>
                  <Dialog.Close>
                    <Button variant="outline">Done</Button>
                  </Dialog.Close>
                </div>
              </div>
            ) : (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  submitCreate();
                }}
              >
                <h2>Create app</h2>
                <Label htmlFor="new-name">Name</Label>
                <Input id="new-name" value={name} onInput={(e) => setName(e.currentTarget.value)} placeholder="My app" />
                <Label htmlFor="new-key">API key (auto-generated — edit to use your own)</Label>
                <Input id="new-key" class="mono" value={key} onInput={(e) => setKey(e.currentTarget.value)} />
                <div class="row">
                  <Button type="submit">Create</Button>
                  <Dialog.Close>
                    <Button variant="outline">Cancel</Button>
                  </Dialog.Close>
                </div>
              </form>
            )}
          </Dialog.Content>
        </Dialog>
      </div>
      {error && <p class="error">{error}</p>}
      {apps === null ? (
        <p class="hint">Loading…</p>
      ) : apps.length === 0 ? (
        <p class="hint">No apps yet — create one.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Name</th>
              <th>API key (hash)</th>
              <th>Well-known details</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {apps.map((a) => (
              <tr key={a.id}>
                <td>
                  <a href={`#/apps/${a.id}`}>{a.name}</a>
                </td>
                <td class="mono">{a.apiKeyHash.slice(0, 12)}…</td>
                <td class="hint">{a.iosAppId || a.androidPackage || '—'}</td>
                <td class="actions">
                  <EditDialog
                    key={a.id + '|' + a.iosAppId + '|' + a.androidPackage + '|' + a.androidCertFingerprint}
                    app={a}
                    onSaved={load}
                  />
                  <Button size="sm" variant="destructive" onClick={() => del(a)}>
                    Delete
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
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
        <Button size="sm" variant="secondary">
          Edit
        </Button>
      </Dialog.Trigger>
      <Dialog.Content>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            save();
          }}
        >
          <h2>Edit {app.name}</h2>
          <Label htmlFor={`${id}-ios`}>iOS App ID (TEAMID.BUNDLEID)</Label>
          <Input
            id={`${id}-ios`}
            value={draft.iosAppId}
            onInput={(e) => setDraft({ ...draft, iosAppId: e.currentTarget.value })}
            placeholder="ABCDE12345.com.example.app"
          />
          <Label htmlFor={`${id}-android`}>Android package</Label>
          <Input
            id={`${id}-android`}
            value={draft.androidPackage}
            onInput={(e) => setDraft({ ...draft, androidPackage: e.currentTarget.value })}
            placeholder="com.example.app"
          />
          <Label htmlFor={`${id}-cert`}>Android cert SHA-256 fingerprint</Label>
          <Input
            id={`${id}-cert`}
            class="mono"
            value={draft.androidCertFingerprint}
            onInput={(e) => setDraft({ ...draft, androidCertFingerprint: e.currentTarget.value })}
            placeholder="AA:BB:CC:…"
          />
          {err && <p class="error">{err}</p>}
          <div class="row">
            <Button type="submit">Save</Button>
            <Dialog.Close>
              <Button variant="outline">Cancel</Button>
            </Dialog.Close>
          </div>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}