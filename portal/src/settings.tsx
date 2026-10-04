import { useEffect, useState } from 'preact/hooks';
import {
  Alert,
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  Button,
  Card,
  Dialog,
  Empty,
  Field,
  Input,
  Label,
  Spinner,
  Table,
} from 'kinu';
import { api, App, CreatedApp } from './api';
import { ConfirmDelete, CopyButton, mono, muted, PageHeader, row } from './ui';
import { inRange, THRESHOLD, WINDOW } from './matching';

const sectionTitle = { margin: '40px 0 16px', fontSize: 20 };

// Per-app settings: app config (well-known files), API key, matching.
export function SettingsPage({ id }: { id: string }) {
  const [app, setApp] = useState<App | null>(null);
  const [error, setError] = useState('');

  const loadAll = () => {
    setError('');
    api
      .getApp(id)
      .then(setApp)
      .catch((e) => setError(String(e)));
  };
  useEffect(loadAll, [id]);

  if (app === null && error === '') return <Spinner />;
  if (app === null)
    return (
      <Card style={{ marginTop: 32 }}>
        <Empty>
          <h3>App not found</h3>
          <Button variant="outline" href="#/">
            Back to apps
          </Button>
        </Empty>
      </Card>
    );

  return (
    <div>
      <Breadcrumb style={{ ...muted, marginTop: 24, marginBottom: -16 }}>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink href="#/">Apps</BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbItem>
            <BreadcrumbLink href={`#/apps/${app.id}`}>{app.name}</BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbItem>Settings</BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
      <PageHeader title="Settings" description={app.name} />
      {error && <Alert variant="destructive">{error}</Alert>}

      <h2 style={{ ...sectionTitle, marginTop: 0 }}>App config</h2>
      <p style={{ ...muted, marginBottom: 16 }}>Used to serve the iOS and Android well-known files.</p>
      <AppConfigTable app={app} onSaved={setApp} />

      <h2 style={sectionTitle}>API key</h2>
      <Table>
        <thead>
          <tr>
            <th>Credential</th>
            <th>Value</th>
            <th />
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>App ID</td>
            <td style={mono}>{app.id}</td>
            <td>
              <div style={{ ...row, justifyContent: 'flex-end' }}>
                <CopyButton value={app.id} label="Copy ID" />
              </div>
            </td>
          </tr>
          <tr>
            <td>API key</td>
            <td style={mono}>
              {app.apiKeyHash ? app.apiKeyHash.slice(0, 12) + '…' : 'revoked — SDK calls rejected'}
            </td>
            <td>
              <div style={{ ...row, justifyContent: 'flex-end' }}>
                <RotateKeyDialog app={app} onChanged={loadAll} />
                <ConfirmDelete
                  title={`Remove the API key for ${app.name}?`}
                  body="The SDK stops accepting it. Rotate a new key to get access back."
                  onConfirm={async () => {
                    setError('');
                    try {
                      await api.revokeKey(app.id);
                      loadAll();
                    } catch (e) {
                      setError(String(e));
                    }
                  }}
                />
              </div>
            </td>
          </tr>
        </tbody>
      </Table>

      <h2 style={sectionTitle}>Matching</h2>
      <p style={{ ...muted, marginBottom: 16 }}>Applies to every link of this app.</p>
      <MatchingTable app={app} onSaved={setApp} />
    </div>
  );
}

function AppConfigTable({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  const [draft, setDraft] = useState({
    iosAppId: app.iosAppId,
    androidPackage: app.androidPackage,
    androidCertFingerprint: app.androidCertFingerprint,
  });
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  const save = async () => {
    setError('');
    setSaved(false);
    try {
      onSaved(await api.updateApp(app.id, draft));
      setSaved(true);
    } catch (e) {
      setError(String(e));
    }
  };

  const field = (id: string, key: keyof typeof draft, placeholder: string, style?: object) => (
    <Input
      id={id}
      value={draft[key]}
      placeholder={placeholder}
      style={style}
      onInput={(e) => setDraft({ ...draft, [key]: e.currentTarget.value })}
    />
  );

  return (
    <form
      style={{ display: 'grid', gap: 16 }}
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <Table>
        <thead>
          <tr>
            <th>Setting</th>
            <th>Value</th>
            <th>Format</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <Label htmlFor="cfg-ios">iOS App ID</Label>
            </td>
            <td>{field('cfg-ios', 'iosAppId', 'ABCDE12345.com.example.app')}</td>
            <td data-label="Format" style={muted}>
              TEAMID.BUNDLEID
            </td>
          </tr>
          <tr>
            <td>
              <Label htmlFor="cfg-android">Android package</Label>
            </td>
            <td>{field('cfg-android', 'androidPackage', 'com.example.app')}</td>
            <td data-label="Format" style={muted}>
              Application ID
            </td>
          </tr>
          <tr>
            <td>
              <Label htmlFor="cfg-cert">Android cert fingerprint</Label>
            </td>
            <td>{field('cfg-cert', 'androidCertFingerprint', 'AA:BB:CC:…', mono)}</td>
            <td data-label="Format" style={muted}>
              SHA-256 of the signing certificate
            </td>
          </tr>
        </tbody>
      </Table>
      {error && <Alert variant="destructive">{error}</Alert>}
      {saved && <Alert variant="success">Saved.</Alert>}
      <div style={{ ...row, justifyContent: 'flex-end' }}>
        <Button type="submit">Save config</Button>
      </div>
    </form>
  );
}

export function MatchingTable({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  const [threshold, setThreshold] = useState(String(app.matchThreshold));
  const [windowMinutes, setWindowMinutes] = useState(String(app.matchWindowMinutes));
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  const save = async () => {
    setError('');
    setSaved(false);
    if (threshold.trim() === '' || windowMinutes.trim() === '') {
      setError('threshold and window are required');
      return;
    }
    const th = Number(threshold);
    const win = Number(windowMinutes);
    if (!Number.isFinite(th) || !Number.isFinite(win)) {
      setError('threshold and window must be numbers');
      return;
    }
    if (!inRange(th, THRESHOLD) || !inRange(win, WINDOW)) {
      setError(`threshold must be ${THRESHOLD.min}–${THRESHOLD.max}, window ${WINDOW.min}–${WINDOW.max} minutes`);
      return;
    }
    try {
      const updated = await api.saveMatching(app.id, { threshold: th, windowMinutes: win });
      setThreshold(String(updated.matchThreshold));
      setWindowMinutes(String(updated.matchWindowMinutes));
      setSaved(true);
      onSaved(updated);
    } catch (e) {
      setError(String(e));
    }
  };

  const field = (id: string, value: string, set: (v: string) => void) => (
    <Input id={id} type="number" value={value} onInput={(e) => set(e.currentTarget.value)} style={{ maxWidth: 120 }} />
  );

  return (
    <form
      style={{ display: 'grid', gap: 16 }}
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <Table>
        <thead>
          <tr>
            <th>Setting</th>
            <th>Value</th>
            <th>Range</th>
            <th>Effect</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <Label htmlFor="set-th">Match threshold</Label>
            </td>
            <td>{field('set-th', threshold, setThreshold)}</td>
            <td data-label="Range" style={muted}>
              {THRESHOLD.min}–{THRESHOLD.max}
            </td>
            <td style={muted}>Higher means stricter install matching.</td>
          </tr>
          <tr>
            <td>
              <Label htmlFor="set-win">Match window (minutes)</Label>
            </td>
            <td>{field('set-win', windowMinutes, setWindowMinutes)}</td>
            <td data-label="Range" style={muted}>
              {WINDOW.min}–{WINDOW.max}
            </td>
            <td style={muted}>How long after a click an install can still match it.</td>
          </tr>
        </tbody>
      </Table>
      {error && <Alert variant="destructive">{error}</Alert>}
      {saved && <Alert variant="success">Saved.</Alert>}
      <div style={{ ...row, justifyContent: 'flex-end' }}>
        <Button type="submit">Save matching</Button>
      </div>
    </form>
  );
}

// RotateKeyDialog mints a key (old dies immediately); plaintext shown once, like create.
function RotateKeyDialog({ app, onChanged }: { app: App; onChanged: () => void }) {
  const [rotated, setRotated] = useState<CreatedApp | null>(null);
  const [err, setErr] = useState('');
  const id = `dlg-rotate-${app.id}`;

  const doRotate = async () => {
    setErr('');
    try {
      setRotated(await api.rotateKey(app.id));
      onChanged();
    } catch (e) {
      setErr(String(e));
    }
  };

  return (
    <Dialog id={id}>
      <Dialog.Trigger>
        <Button size="sm" variant="outline">
          Rotate
        </Button>
      </Dialog.Trigger>
      <Dialog.Content>
        {rotated ? (
          <div style={{ display: 'grid', gap: 16 }}>
            <h2 style={{ margin: 0 }}>Key rotated</h2>
            <Alert variant="warning">Old key stopped working. New key shown once.</Alert>
            <Field>
              <Label htmlFor={`${id}-key`}>API key</Label>
              <Input id={`${id}-key`} readOnly value={rotated.apiKey} style={mono} />
              <Field.Description>EXPO_PUBLIC_DETOUR_API_KEY in the SDK config.</Field.Description>
            </Field>
            <div style={{ ...row, justifyContent: 'flex-end' }}>
              <Dialog.Close>
                <Button variant="outline" onClick={() => setRotated(null)}>
                  Done
                </Button>
              </Dialog.Close>
              <CopyButton value={rotated.apiKey} label="Copy API key" />
            </div>
          </div>
        ) : (
          <form
            style={{ display: 'grid', gap: 16 }}
            onSubmit={(e) => {
              e.preventDefault();
              doRotate();
            }}
          >
            <h2 style={{ margin: 0 }}>Rotate API key</h2>
            <p style={muted}>A new key is generated; the current one stops working immediately.</p>
            {err && <Alert variant="destructive">{err}</Alert>}
            <div style={{ ...row, justifyContent: 'flex-end' }}>
              <Dialog.Close>
                <Button variant="outline">Cancel</Button>
              </Dialog.Close>
              <Button type="submit">Rotate key</Button>
            </div>
          </form>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
