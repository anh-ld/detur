import { cloneElement, JSX } from 'preact';
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
import { Analytics, api, App, CreatedApp, Link, Platform } from './api';
import { closeDialog, ConfirmDelete, CopyButton, mono, muted, PageHeader, row } from './ui';
import { MatchingTable } from './settings';
import { AnalyticsView, Filters } from './analytics';

interface LinkDraft {
  key: string;
  url: string;
  ios: string;
  android: string;
  fallbackUrl: string;
}

const emptyDraft = (): LinkDraft => ({
  key: '',
  url: '',
  ios: '',
  android: '',
  fallbackUrl: '',
});

const draftFrom = (l: Link): LinkDraft => ({
  key: l.key,
  url: l.url,
  ios: l.ios,
  android: l.android,
  fallbackUrl: l.fallbackUrl,
});

export function DetailPage({ id }: { id: string }) {
  const [app, setApp] = useState<App | null>(null);
  const [links, setLinks] = useState<Link[] | null>(null);
  const [stats, setStats] = useState<Analytics | null>(null);
  const [days, setDays] = useState(7);
  const [platform, setPlatform] = useState<Platform>('');
  const [error, setError] = useState('');

  const loadAll = () => {
    setError('');
    api
      .getApp(id)
      .then(setApp)
      .catch((e) => setError(String(e)));
    api
      .listLinks(id)
      .then(setLinks)
      .catch((e) => setError(String(e)));
  };
  // Analytics: one request per id/filter/refresh; a superseded response is dropped (stale flag).
  const [statsTick, setStatsTick] = useState(0);
  useEffect(() => {
    let stale = false;
    api
      .getAnalytics(id, days, platform)
      .then((s) => !stale && setStats(s))
      .catch((e) => !stale && setError(String(e)));
    return () => {
      stale = true;
    };
  }, [id, days, platform, statsTick]);
  const refresh = () => {
    loadAll();
    setStatsTick((t) => t + 1);
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
      refresh();
    } catch (e) {
      setError(String(e));
    }
  };

  const linkStats = new Map(stats?.links.map((l) => [l.linkId, l]));

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
            <Button variant="outline" onClick={refresh}>
              Refresh
            </Button>
          </>
        }
      />
      {error && <Alert variant="destructive">{error}</Alert>}

      <div style={{ ...row, justifyContent: 'space-between', flexWrap: 'wrap', gap: 12, marginBottom: 16 }}>
        <h2 style={{ margin: 0, fontSize: 20 }}>Analytics</h2>
        <Filters days={days} platform={platform} onDays={setDays} onPlatform={setPlatform} />
      </div>
      <AnalyticsView data={stats} />

      <div style={{ ...row, justifyContent: 'space-between', flexWrap: 'wrap', gap: 12, margin: '40px 0 16px' }}>
        <div style={{ display: 'grid', gap: 4 }}>
          <h2 style={{ margin: 0, fontSize: 20 }}>Links</h2>
          <p style={muted}>Clicks and matches follow the analytics filters.</p>
        </div>
        <LinkDialog appId={app.id} link={null} onSaved={refresh} trigger={<Button>New link</Button>} />
      </div>
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
              <th style={{ textAlign: 'right' }}>Clicks</th>
              <th style={{ textAlign: 'right' }}>Matches</th>
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
                <td data-label="Clicks" className="num">
                  {linkStats.get(l.id)?.clicks ?? 0}
                </td>
                <td data-label="Matches" className="num">
                  {linkStats.get(l.id)?.matches ?? 0}
                </td>
                <td>
                  <div style={{ ...row, justifyContent: 'flex-end' }}>
                    <LinkDialog
                      appId={app.id}
                      link={l}
                      onSaved={refresh}
                      trigger={
                        <Button size="sm" variant="outline">
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



      <h2 style={{ margin: '40px 0 16px', fontSize: 20 }}>API key</h2>
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

      <h2 style={{ margin: '40px 0 16px', fontSize: 20 }}>Matching</h2>
      <p style={{ ...muted, marginBottom: 16 }}>Applies to every link of this app.</p>
      <MatchingTable app={app} onSaved={setApp} />
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
    if (!draft.key.trim() || !draft.url.trim()) {
      setErr('key and url are required');
      return;
    }
    const payload = {
      url: draft.url.trim(),
      ios: draft.ios,
      android: draft.android,
      fallbackUrl: draft.fallbackUrl,
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
      <Dialog.Trigger>
        {cloneElement(trigger, {
          onClick: () => {
            setDraft(link ? draftFrom(link) : emptyDraft());
            setErr('');
          },
        })}
      </Dialog.Trigger>
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
