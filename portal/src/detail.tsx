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
  Tab,
  Table,
  TabList,
} from 'kinu';
import { Analytics, api, App, DateRangeFilter, Link, Platform } from './api';
import { closeDialog, ConfirmDelete, Loading, mono, muted, PageHeader, row } from './ui';
import { AnalyticsView, Filters, fmt } from './analytics';
import { FraudPanel } from './fraud';
import { SettingsPanel } from './settings';
import { WebhooksPanel } from './webhooks';

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

// 1.5px outline icon, decorative (the button label carries the name).
const Icon = ({ d, spin }: { d: string; spin?: boolean }) => (
  <svg class={spin ? 'spin' : undefined} width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5"
    stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path d={d} />
  </svg>
);

export type AppTab = 'links' | 'analytics' | 'fraud' | 'settings' | 'webhooks';

const TABS: { tab: AppTab; label: string }[] = [
  { tab: 'links', label: 'Links' },
  { tab: 'analytics', label: 'Analytics' },
  { tab: 'fraud', label: 'Fraud' },
  { tab: 'settings', label: 'Settings' },
  { tab: 'webhooks', label: 'Webhooks' },
];

const tabHref = (id: string, tab: AppTab) => `#/apps/${encodeURIComponent(id)}${tab === 'links' ? '' : '/' + tab}`;

// App page: one header, one tab per job. Links land first: they are what an operator touches most.
export function AppPage({ id, tab }: { id: string; tab: AppTab }) {
  const [app, setApp] = useState<App | null>(null);
  const [links, setLinks] = useState<Link[] | null>(null);
  const [stats, setStats] = useState<Analytics | null>(null);
  const [range, setRange] = useState<DateRangeFilter>({ days: 7 });
  const [platform, setPlatform] = useState<Platform>('');
  const [error, setError] = useState('');

  const loadAll = () => {
    setError('');
    return Promise.all([
      api
        .getApp(id)
        .then(setApp)
        .catch((e) => setError(String(e))),
      api
        .listLinks(id)
        .then(setLinks)
        .catch((e) => setError(String(e))),
    ]);
  };
  // Analytics (also feeds link counts): one request per id/filter/refresh; a superseded response is dropped (stale flag).
  const [tick, setTick] = useState(0);
  const [statsLoading, setStatsLoading] = useState(false);
  useEffect(() => {
    let stale = false;
    setStatsLoading(true);
    api
      .getAnalytics(id, range, platform)
      .then((s) => !stale && setStats(s))
      .catch((e) => !stale && setError(String(e)))
      .finally(() => !stale && setStatsLoading(false));
    return () => {
      stale = true;
    };
  }, [id, range.days, range.from, range.to, platform, tick]);
  // Refresh icon spins until app, links and analytics load; min 600ms so a fast reload stays visible.
  const [refreshing, setRefreshing] = useState(false);
  const refresh = () => {
    setRefreshing(true);
    setTick((t) => t + 1);
    Promise.all([loadAll(), new Promise((r) => setTimeout(r, 600))]).then(() => setRefreshing(false));
  };
  useEffect(() => {
    setApp(null);
    setLinks(null);
    setStats(null);
    loadAll();
  }, [id]);

  if (app === null && error === '') return <Loading />;
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
  const filtered = tab === 'links' || tab === 'analytics';

  return (
    <div>
      <Breadcrumb style={{ ...muted, marginTop: 24, marginBottom: -16 }}>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink href="#/">Apps</BreadcrumbLink>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
      <PageHeader
        title={app.name}
        actions={
          tab !== 'settings' && (
            <Button variant="secondary" onClick={refresh} aria-busy={refreshing || statsLoading}>
              <Icon d="M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7" spin={refreshing || statsLoading} />
              Refresh
            </Button>
          )
        }
      />
      <div class="tab-bar">
        <TabList role="tablist" aria-label="App sections">
          {TABS.map((t) => (
            <Tab key={t.tab} role="tab" aria-selected={t.tab === tab} onClick={() => (location.hash = tabHref(app.id, t.tab))}>
              {t.label}
            </Tab>
          ))}
        </TabList>
        {filtered && <Filters range={range} platform={platform} onRange={setRange} onPlatform={setPlatform} />}
      </div>
      {error && <Alert variant="destructive">{error}</Alert>}

      {tab === 'links' && (
        <section aria-label="Links" style={{ display: 'grid', gap: 16 }}>
          <div style={{ ...row, justifyContent: 'space-between', flexWrap: 'wrap', gap: 12 }}>
            <p style={muted}>Clicks and matches follow the filters above.</p>
            <LinkDialog appId={app.id} link={null} onSaved={refresh} trigger={<Button>New link</Button>} />
          </div>
          {links === null ? (
            <Loading />
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
                      {fmt(linkStats.get(l.id)?.clicks ?? 0)}
                    </td>
                    <td data-label="Matches" className="num">
                      {fmt(linkStats.get(l.id)?.matches ?? 0)}
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
                          id={`dlg-del-link-${l.id}`}
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
        </section>
      )}
      {tab === 'analytics' && <AnalyticsView data={stats} />}
      {tab === 'fraud' && <FraudPanel appId={app.id} tick={tick} />}
      {tab === 'settings' && <SettingsPanel app={app} onApp={setApp} onReload={loadAll} />}
      {tab === 'webhooks' && <WebhooksPanel app={app} tick={tick} />}
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
