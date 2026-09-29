import { JSX } from 'preact';
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
  Separator,
  Spinner,
  Table,
} from 'kinu';
import { api, App, CreatedApp, Link, Readout } from './api';
import { closeDialog, ConfirmDelete, mono, muted, PageHeader, row } from './ui';

const copyText = async (v: string) => {
  await navigator.clipboard.writeText(v);
};

interface LinkDraft {
  key: string;
  url: string;
  ios: string;
  android: string;
  fallbackUrl: string;
  threshold: string;
  windowMinutes: string;
}

const emptyDraft = (): LinkDraft => ({
  key: '',
  url: '',
  ios: '',
  android: '',
  fallbackUrl: '',
  threshold: '',
  windowMinutes: '',
});

const draftFrom = (l: Link): LinkDraft => ({
  key: l.key,
  url: l.url,
  ios: l.ios,
  android: l.android,
  fallbackUrl: l.fallbackUrl,
  threshold: l.threshold ? String(l.threshold) : '',
  windowMinutes: l.windowMinutes ? String(l.windowMinutes) : '',
});

export function DetailPage({ id }: { id: string }) {
  const [app, setApp] = useState<App | null>(null);
  const [links, setLinks] = useState<Link[] | null>(null);
  const [readout, setReadout] = useState<Readout | null>(null);
  const [error, setError] = useState('');

  const loadAll = () => {
    setError('');
    api
      .listApps()
      .then((apps) => setApp(apps.find((a) => a.id === id) ?? null))
      .catch((e) => setError(String(e)));
    api
      .listLinks(id)
      .then(setLinks)
      .catch((e) => setError(String(e)));
    api
      .getReadout(id)
      .then(setReadout)
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

  const del = async (l: Link) => {
    setError('');
    try {
      await api.deleteLink(l.id);
      loadAll();
    } catch (e) {
      setError(String(e));
    }
  };

  const stat = (label: string, n: number | undefined) => (
    <Card padding="sm" style={{ display: 'grid', gap: 4 }}>
      <p style={muted}>{label}</p>
      <div style={{ fontSize: 28, fontWeight: 600 }}>{n ?? '–'}</div>
    </Card>
  );

  return (
    <div>
      <Breadcrumb style={{ ...muted, marginTop: 24, marginBottom: -16 }}>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink href="#/">Apps</BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbItem>{app.name}</BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
      <PageHeader title={app.name} actions={
          <>
            <Button variant="outline" onClick={loadAll}>
              Refresh
            </Button>
            <LinkDialog
              key={'create' + app.id}
              appId={app.id}
              link={null}
              onSaved={loadAll}
              trigger={<Button>New link</Button>}
            />
          </>
        }
      />
      {error && <Alert variant="destructive">{error}</Alert>}

      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 16 }}>
        {stat('Clicks', readout?.clicks)}
        {stat('Non-organic installs', readout?.nonOrganic)}
        {stat('Organic installs', readout?.organic)}
      </div>

      <h2 style={{ margin: '40px 0 16px', fontSize: 20 }}>API key</h2>
      <Card style={{ display: 'grid' }}>
        <div
          style={{
            ...row,
            justifyContent: 'space-between',
            paddingBottom: 14,
            borderBottom: '1px solid hsl(var(--k-border))',
          }}
        >
          <div style={{ display: 'grid', gap: 2 }}>
            <p style={{ ...muted, fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.08em' }}>App ID</p>
            <span style={mono}>{app.id}</span>
          </div>
          <Button size="sm" variant="outline" onClick={() => copyText(app.id)}>
            Copy ID
          </Button>
        </div>
        <div style={{ ...row, justifyContent: 'space-between', paddingTop: 14 }}>
          <div style={{ display: 'grid', gap: 2 }}>
            <p style={{ ...muted, fontSize: 12, textTransform: 'uppercase', letterSpacing: '0.08em' }}>API key</p>
            <span style={mono}>
              {app.apiKeyHash ? app.apiKeyHash.slice(0, 12) + '…' : 'revoked — SDK calls rejected'}
            </span>
          </div>
          <div style={row}>
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
        </div>
      </Card>

      <h2 style={{ margin: '40px 0 16px', fontSize: 20 }}>Links</h2>
      {links === null ? (
        <Spinner />
      ) : links.length === 0 ? (
        <Card>
          <Empty>
            <h3>No links yet</h3>
            Create a link to start tracking clicks and installs.
          </Empty>
        </Card>
      ) : (
        <Table>
          <thead>
            <tr>
              <th>Link</th>
              <th>Platforms</th>
              <th>Matching</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {links.map((l) => (
              <tr key={l.id}>
                <td>
                  <div style={{ display: 'grid', gap: 2 }}>
                    <strong style={mono}>{l.key}</strong>
                    <a href={l.url} target="_blank" rel="noreferrer" style={{ ...muted, textDecoration: 'none' }}>
                      {l.url}
                    </a>
                  </div>
                </td>
                <td>
                  <div style={row}>
                    {l.ios && <Badge variant="secondary">iOS</Badge>}
                    {l.android && <Badge variant="secondary">Android</Badge>}
                    {l.fallbackUrl && <Badge variant="secondary">Web</Badge>}
                    {!l.ios && !l.android && !l.fallbackUrl && <Badge variant="outline">URL only</Badge>}
                  </div>
                </td>
                <td style={muted}>
                  {l.threshold || l.windowMinutes
                    ? `${l.threshold || 'global'} · ${l.windowMinutes ? l.windowMinutes + ' min' : 'global'}`
                    : 'Global'}
                </td>
                <td>
                  <div style={{ ...row, justifyContent: 'flex-end' }}>
                    <LinkDialog
                      key={'edit' + l.id}
                      appId={app.id}
                      link={l}
                      onSaved={loadAll}
                      trigger={
                        <Button size="sm" variant="ghost">
                          Edit
                        </Button>
                      }
                    />
                    <ConfirmDelete
                      title={`Delete ${l.key}?`}
                      body="The short link stops working."
                      onConfirm={() => del(l)}
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

function LinkDialog({
  appId,
  link,
  onSaved,
  trigger,
}: {
  appId: string;
  link: Link | null;
  onSaved: () => void;
  trigger: JSX.Element;
}) {
  const [draft, setDraft] = useState<LinkDraft>(link ? draftFrom(link) : emptyDraft());
  const [err, setErr] = useState('');
  const id = link ? `dlg-edit-link-${link.id}` : 'dlg-create-link';

  const save = async () => {
    setErr('');
    const threshold = draft.threshold === '' ? 0 : Number(draft.threshold);
    const windowMinutes = draft.windowMinutes === '' ? 0 : Number(draft.windowMinutes);
    if (!draft.key.trim() || !draft.url.trim()) {
      setErr('key and url are required');
      return;
    }
    if ((draft.threshold !== '' && !Number.isFinite(threshold)) || (draft.windowMinutes !== '' && !Number.isFinite(windowMinutes))) {
      setErr('threshold and window must be numbers');
      return;
    }
    const payload = {
      url: draft.url.trim(),
      ios: draft.ios,
      android: draft.android,
      fallbackUrl: draft.fallbackUrl,
      threshold,
      windowMinutes,
    };
    try {
      if (link) {
        await api.updateLink(link.id, payload);
      } else {
        await api.createLink(appId, { key: draft.key.trim(), ...payload });
      }
      closeDialog(id);
      onSaved();
    } catch (e) {
      setErr(String(e));
    }
  };

  return (
    <Dialog id={id}>
      <Dialog.Trigger>{trigger}</Dialog.Trigger>
      <Dialog.Content>
        <form
          style={{ display: 'grid', gap: 16 }}
          onSubmit={(e) => {
            e.preventDefault();
            save();
          }}
        >
          <h2 style={{ margin: 0 }}>{link ? 'Edit link' : 'Create link'}</h2>
          <Field>
            <Label htmlFor={`${id}-key`}>Key</Label>
            <Input
              id={`${id}-key`}
              style={mono}
              value={draft.key}
              onInput={(e) => setDraft({ ...draft, key: e.currentTarget.value })}
              placeholder="summer-sale"
              disabled={!!link}
            />
            <Field.Description>The short link path. Can't be changed later.</Field.Description>
          </Field>
          <Field>
            <Label htmlFor={`${id}-url`}>Destination URL</Label>
            <Input
              id={`${id}-url`}
              value={draft.url}
              onInput={(e) => setDraft({ ...draft, url: e.currentTarget.value })}
              placeholder="https://example.com/product"
            />
            <Field.Description>Handed to the app after install.</Field.Description>
          </Field>

          <Separator />
          <p style={muted}>Redirects (optional)</p>
          <Field>
            <Label htmlFor={`${id}-ios`}>iOS store page</Label>
            <Input
              id={`${id}-ios`}
              value={draft.ios}
              onInput={(e) => setDraft({ ...draft, ios: e.currentTarget.value })}
              placeholder="https://apps.apple.com/app/id123"
            />
          </Field>
          <Field>
            <Label htmlFor={`${id}-android`}>Android store page</Label>
            <Input
              id={`${id}-android`}
              value={draft.android}
              onInput={(e) => setDraft({ ...draft, android: e.currentTarget.value })}
              placeholder="https://play.google.com/store/apps/details?id=com.example"
            />
          </Field>
          <Field>
            <Label htmlFor={`${id}-fb`}>Desktop fallback</Label>
            <Input
              id={`${id}-fb`}
              value={draft.fallbackUrl}
              onInput={(e) => setDraft({ ...draft, fallbackUrl: e.currentTarget.value })}
              placeholder="https://example.com"
            />
          </Field>

          <Separator />
          <p style={muted}>Matching (optional, empty = global setting)</p>
          <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <Field>
              <Label htmlFor={`${id}-th`}>Threshold</Label>
              <Input
                id={`${id}-th`}
                type="number"
                value={draft.threshold}
                onInput={(e) => setDraft({ ...draft, threshold: e.currentTarget.value })}
                placeholder="850"
              />
              <Field.Description>700–1200</Field.Description>
            </Field>
            <Field>
              <Label htmlFor={`${id}-win`}>Window (minutes)</Label>
              <Input
                id={`${id}-win`}
                type="number"
                value={draft.windowMinutes}
                onInput={(e) => setDraft({ ...draft, windowMinutes: e.currentTarget.value })}
                placeholder="15"
              />
              <Field.Description>5–180</Field.Description>
            </Field>
          </div>
          {err && <Alert variant="destructive">{err}</Alert>}
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Dialog.Close>
              <Button variant="outline">Cancel</Button>
            </Dialog.Close>
            <Button type="submit">{link ? 'Save' : 'Create'}</Button>
          </div>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}

// RotateKeyDialog mints a new API key (old one dies immediately) and shows
// the plaintext once, like the create flow.
function RotateKeyDialog({ app, onChanged }: { app: App; onChanged: () => void }) {
  const [rotated, setRotated] = useState<CreatedApp | null>(null);
  const [copied, setCopied] = useState(false);
  const [err, setErr] = useState('');
  const id = `dlg-rotate-${app.id}`;

  const doRotate = async () => {
    setErr('');
    try {
      setRotated(await api.rotateKey(app.id));
      setCopied(false);
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
              <Button
                onClick={async () => {
                  await navigator.clipboard.writeText(rotated.apiKey);
                  setCopied(true);
                }}
              >
                {copied ? 'Copied' : 'Copy key'}
              </Button>
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
