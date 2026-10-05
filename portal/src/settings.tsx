import { useEffect, useState } from 'preact/hooks';
import {
  Alert,
  Badge,
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
import { api, App, CreatedApp, HealthCheck, MatchQuality } from './api';
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
        </BreadcrumbList>
      </Breadcrumb>
      <PageHeader title="Settings" />
      {error && <Alert variant="destructive">{error}</Alert>}

      <h2 style={{ ...sectionTitle, marginTop: 0 }}>Health</h2>
      <p style={{ ...muted, marginBottom: 16 }}>Setup checks. Refreshes after you save.</p>
      <HealthTable app={app} />

      <h2 style={sectionTitle}>App config</h2>
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

      <h2 style={sectionTitle}>Match quality</h2>
      <p style={{ ...muted, marginBottom: 16 }}>Installs from the last 30 days: how each was attributed, and the score it got.</p>
      <MatchQualityView app={app} />
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

const STATUS = { ok: 'OK', warn: 'Check', fail: 'Fix' };

// Refetch on app change (config save).
function HealthTable({ app }: { app: App }) {
  const [checks, setChecks] = useState<HealthCheck[] | null>(null);
  useEffect(() => {
    api.getHealth(app.id).then(setChecks, () => setChecks([]));
  }, [app]);
  if (!checks) return <Spinner />;
  return (
    <Table>
      <thead>
        <tr>
          <th>Check</th>
          <th>Status</th>
          <th>Detail</th>
        </tr>
      </thead>
      <tbody>
        {checks.map((c) => (
          <tr key={c.check}>
            <td>{c.check}</td>
            <td data-label="Status">
              <span class={`health ${c.status}`}>{STATUS[c.status]}</span>
            </td>
            <td style={muted}>{c.detail}</td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

const METHODS: [string, string][] = [
  ['click_id', 'Click ID'],
  ['probabilistic', 'Fingerprint'],
  ['prior', 'Retry'],
  ['organic', 'Organic'],
  ['', 'Before receipts'],
];

// Method mix, score histogram, threshold what-if.
export function MatchQualityView({ app }: { app: App }) {
  const [q, setQ] = useState<MatchQuality | null>(null);
  const [at, setAt] = useState(app.matchThreshold);
  useEffect(() => {
    api.getMatchQuality(app.id).then(setQ, () => setQ({ methods: {}, buckets: [] }));
  }, [app.id]);
  useEffect(() => setAt(app.matchThreshold), [app.matchThreshold]);
  if (!q) return <Spinner />;

  const top = Math.max(1, ...q.buckets.map((b) => b.matched + b.organic));
  const lose = q.buckets.filter((b) => b.from < at).reduce((n, b) => n + b.matched, 0);
  const gain = q.buckets.filter((b) => b.from >= at).reduce((n, b) => n + b.organic, 0);
  return (
    <Card style={{ display: 'grid', gap: 16 }}>
      <div style={{ ...row, flexWrap: 'wrap' }}>
        {METHODS.filter(([k]) => q.methods[k]).map(([k, label]) => (
          <Badge key={k} variant="secondary" style={{ letterSpacing: 'normal' }}>
            {label} {q.methods[k]}
          </Badge>
        ))}
        {Object.keys(q.methods).length === 0 && <p style={muted}>No installs yet.</p>}
      </div>
      {q.buckets.length > 0 && (
        <>
          <div class="score-bars" role="img" aria-label="Installs per fingerprint score">
            {q.buckets.map((b) => (
              <div key={b.from} title={`${b.from}–${b.from + 49}: ${b.matched} matched, ${b.organic} organic`}>
                <span class="score-bar organic" style={{ height: `${(b.organic / top) * 100}%` }} />
                <span class="score-bar matched" style={{ height: `${(b.matched / top) * 100}%`, opacity: b.from < at ? 0.35 : 1 }} />
                <small>{b.from}</small>
              </div>
            ))}
          </div>
          <Field>
            <Label htmlFor="whatif">
              At threshold {at}: lose {lose} matches, gain up to {gain} organic installs
            </Label>
            <input
              id="whatif"
              type="range"
              min={THRESHOLD.min}
              max={THRESHOLD.max}
              step={50}
              value={at}
              onInput={(e) => setAt(Number(e.currentTarget.value))}
            />
            <Field.Description>Fingerprint matches only. Click ID matches never depend on the threshold.</Field.Description>
          </Field>
        </>
      )}
    </Card>
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
