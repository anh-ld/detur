import { cloneElement, JSX } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
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
  InputGroup,
  Label,
  Separator,
  Tab,
  Table,
  TabList,
  Textarea,
} from 'kinu';
import { Analytics, api, App, DateRangeFilter, Link, Platform } from './api';
import { closeDialog, ConfirmDelete, Icon, Loading, mono, muted, PageHeader, row, ShortLink } from './ui';
import { AnalyticsView, Filters, fmt } from './analytics';
import { FraudPanel } from './fraud';
import { SettingsPanel } from './settings';
import { WebhooksPanel } from './webhooks';
import { RulesDialog } from './rules-drawer';

interface LinkDraft {
  key: string;
  url: string;
  ios: string;
  android: string;
  fallbackUrl: string;
  ogTitle: string;
  ogDescription: string;
  ogImage: string;
  expiresAt: string; // datetime-local value, read as UTC
  expiredUrl: string;
}

// RFC3339 UTC <-> datetime-local (UTC wall time, no zone shift)
const toLocalInput = (iso: string) => (iso ? iso.slice(0, 16) : '');
const fromLocalInput = (v: string) => (v ? `${v}:00Z` : '');
const isExpired = (l: Link) => !!l.expiresAt && new Date(l.expiresAt).getTime() < Date.now();

// social preview limits: match the server's
const OG_TITLE_MAX = 100;
const OG_DESC_MAX = 300;
const httpsUrl = (s: string) => /^https:\/\/[^/\s]+/i.test(s);

const emptyDraft = (): LinkDraft => ({
  key: '',
  url: '',
  ios: '',
  android: '',
  fallbackUrl: '',
  ogTitle: '',
  ogDescription: '',
  ogImage: '',
  expiresAt: '',
  expiredUrl: '',
});

const draftFrom = (l: Link): LinkDraft => ({
  key: l.key,
  url: l.url,
  ios: l.ios,
  android: l.android,
  fallbackUrl: l.fallbackUrl,
  ogTitle: l.ogTitle ?? '',
  ogDescription: l.ogDescription ?? '',
  ogImage: l.ogImage ?? '',
  expiresAt: toLocalInput(l.expiresAt ?? ''),
  expiredUrl: l.expiredUrl ?? '',
});

export type AppTab = 'links' | 'analytics' | 'fraud' | 'settings' | 'webhooks';

const TABS: { tab: AppTab; label: string }[] = [
  { tab: 'links', label: 'Links' },
  { tab: 'analytics', label: 'Analytics' },
  { tab: 'fraud', label: 'Fraud' },
  { tab: 'settings', label: 'Settings' },
  { tab: 'webhooks', label: 'Webhooks' },
];

const tabHref = (id: string, tab: AppTab) => `#/apps/${encodeURIComponent(id)}${tab === 'links' ? '' : '/' + tab}`;

// App page: one header, tab per job. Links first: most touched.
export function AppPage({ id, tab }: { id: string; tab: AppTab }) {
  const [app, setApp] = useState<App | null>(null);
  const [links, setLinks] = useState<Link[] | null>(null);
  const [stats, setStats] = useState<Analytics | null>(null);
  const [range, setRange] = useState<DateRangeFilter>({ days: 7 });
  const [platform, setPlatform] = useState<Platform>('');
  const [error, setError] = useState('');
  const [linkBase, setLinkBase] = useState('');
  useEffect(() => {
    api.getConfig().then((c) => setLinkBase(c.linkBase ?? ''), () => {});
  }, []);

  // drop responses for an app already left
  const current = useRef(id);
  current.current = id;
  const loadAll = () => {
    setError('');
    const req = id;
    const ok = () => current.current === req;
    return Promise.all([
      api
        .getApp(req)
        .then((a) => ok() && setApp(a))
        .catch((e) => ok() && setError(String(e))),
      api
        .listLinks(req)
        .then((l) => ok() && setLinks(l))
        .catch((e) => ok() && setError(String(e))),
    ]);
  };
  // analytics + link counts: Links/Analytics tabs only; stale dropped
  const filtered = tab === 'links' || tab === 'analytics';
  const [tick, setTick] = useState(0);
  const [statsLoading, setStatsLoading] = useState(false);
  useEffect(() => {
    if (!filtered) return;
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
  }, [id, range.days, range.from, range.to, platform, tick, filtered]);
  // spin until loaded, min 600ms so fast reloads show
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
            <LinkDialog appId={app.id} link={null} linkBase={linkBase} onSaved={refresh} trigger={<Button>New link</Button>} />
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
                      <div class="link-cell">
                        <div style={row}>
                          <ShortLink base={linkBase} linkKey={l.key} />
                          {isExpired(l) && <Badge variant="outline">Expired</Badge>}
                        </div>
                        <span class="link-dest" title={l.url}>
                          {l.url}
                        </span>
                      </div>
                    </td>
                    <td>
                      <div style={row}>
                        {l.ios && <Badge variant="secondary">iOS</Badge>}
                        {l.android && <Badge variant="secondary">Android</Badge>}
                        {l.fallbackUrl && <Badge variant="secondary">Web</Badge>}
                        {!l.ios && !l.android && !l.fallbackUrl && <Badge variant="outline">Route only</Badge>}
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
                        <RulesDialog
                          appId={app.id}
                          link={l}
                          linkBase={linkBase}
                          days={range.days ?? 30}
                          onSaved={refresh}
                          trigger={
                            <Button size="sm" variant="outline">
                              Rules & splits
                            </Button>
                          }
                        />
                        <LinkDialog
                          appId={app.id}
                          link={l}
                          linkBase={linkBase}
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
  linkBase,
  onSaved,
  trigger,
}: {
  appId: string;
  link: Link | null;
  linkBase: string;
  onSaved: () => void;
  trigger: JSX.Element;
}) {
  const [draft, setDraft] = useState<LinkDraft>(link ? draftFrom(link) : emptyDraft());
  const [err, setErr] = useState('');
  const id = link ? `dlg-edit-link-${link.id}` : 'dlg-create-link';
  const ogImageBad = !!draft.ogImage.trim() && !httpsUrl(draft.ogImage.trim());

  const save = async () => {
    setErr('');
    if (!draft.key.trim() || !draft.url.trim()) {
      setErr('Key and destination are required');
      return;
    }
    if (ogImageBad) {
      setErr('Social preview image must be an absolute https URL');
      return;
    }
    const payload = {
      url: draft.url.trim(),
      ios: draft.ios,
      android: draft.android,
      fallbackUrl: draft.fallbackUrl,
      ogTitle: draft.ogTitle,
      ogDescription: draft.ogDescription,
      ogImage: draft.ogImage,
      expiresAt: fromLocalInput(draft.expiresAt),
      expiredUrl: draft.expiredUrl,
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
          {link ? (
            <div class="short-link-box">
              <span style={muted}>Short link</span>
              <ShortLink base={linkBase} linkKey={link.key} />
            </div>
          ) : (
            <Field>
              <Label htmlFor={`${id}-key`}>Short link</Label>
              <InputGroup class="prefix-group">
                {linkBase && <legend>{linkBase.replace(/^https?:\/\//, '')}/</legend>}
                <Input
                  id={`${id}-key`}
                  style={mono}
                  value={draft.key}
                  onInput={(e) => setDraft({ ...draft, key: e.currentTarget.value })}
                  placeholder="summer-sale"
                />
              </InputGroup>
              <Field.Description>The path people tap. Can't be changed later.</Field.Description>
            </Field>
          )}
          <Field>
            <Label htmlFor={`${id}-url`}>Destination</Label>
            <Input
              id={`${id}-url`}
              value={draft.url}
              onInput={(e) => setDraft({ ...draft, url: e.currentTarget.value })}
              placeholder="/product/42?ref=promo"
            />
            <Field.Description>App route the app opens, e.g. /product/42. Web visitors use the web fallback.</Field.Description>
          </Field>

          <Separator />
          <div class="form-section">
            <h3>Store and web redirects</h3>
            <p style={muted}>Optional. Where people without the app land.</p>
          </div>
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
            <Label htmlFor={`${id}-fb`}>Web fallback</Label>
            <Input
              id={`${id}-fb`}
              value={draft.fallbackUrl}
              onInput={(e) => setDraft({ ...draft, fallbackUrl: e.currentTarget.value })}
              placeholder="https://example.com"
            />
            <Field.Description>Desktop visitors, and phones with no store page above for their platform.</Field.Description>
          </Field>

          <details open={!!(draft.expiresAt || draft.expiredUrl)}>
            <summary>
              Expiry <span style={muted}>optional</span>
            </summary>
            <div style={{ display: 'grid', gap: 16, marginTop: 12 }}>
              <Field>
                <Label htmlFor={`${id}-exp`}>Expires at (UTC)</Label>
                <Input
                  id={`${id}-exp`}
                  type="datetime-local"
                  value={draft.expiresAt}
                  onInput={(e) => setDraft({ ...draft, expiresAt: e.currentTarget.value })}
                />
                <Field.Description>After this, the link goes to the expired URL, or shows "link expired".</Field.Description>
              </Field>
              <Field>
                <Label htmlFor={`${id}-expurl`}>Expired URL</Label>
                <Input
                  id={`${id}-expurl`}
                  value={draft.expiredUrl}
                  onInput={(e) => setDraft({ ...draft, expiredUrl: e.currentTarget.value })}
                  placeholder="https://example.com/offer-ended"
                />
              </Field>
            </div>
          </details>

          <details open={!!(draft.ogTitle || draft.ogDescription || draft.ogImage)}>
            <summary>
              Social preview <span style={muted}>optional</span>
            </summary>
            <div style={{ display: 'grid', gap: 16, marginTop: 12 }}>
              <Field>
                <Label htmlFor={`${id}-ogt`}>Title</Label>
                <Input
                  id={`${id}-ogt`}
                  value={draft.ogTitle}
                  maxLength={OG_TITLE_MAX}
                  onInput={(e) => setDraft({ ...draft, ogTitle: e.currentTarget.value })}
                  placeholder="Sam invited you"
                />
                <Field.Description>{draft.ogTitle.length}/{OG_TITLE_MAX}</Field.Description>
              </Field>
              <Field>
                <Label htmlFor={`${id}-ogd`}>Description</Label>
                <Textarea
                  id={`${id}-ogd`}
                  value={draft.ogDescription}
                  maxLength={OG_DESC_MAX}
                  rows={2}
                  onInput={(e) => setDraft({ ...draft, ogDescription: e.currentTarget.value })}
                />
                <Field.Description>{draft.ogDescription.length}/{OG_DESC_MAX}</Field.Description>
              </Field>
              <Field>
                <Label htmlFor={`${id}-ogi`}>Image URL</Label>
                <Input
                  id={`${id}-ogi`}
                  value={draft.ogImage}
                  aria-invalid={ogImageBad}
                  onInput={(e) => setDraft({ ...draft, ogImage: e.currentTarget.value })}
                  placeholder="https://example.com/card.png"
                />
                <Field.Description>
                  {ogImageBad
                    ? 'Must be an absolute https URL'
                    : 'Shown when the link is shared in chats and social apps. 1200×630 works best.'}
                </Field.Description>
              </Field>
              {(draft.ogTitle || draft.ogDescription || draft.ogImage) && (
                <figure class="og-preview">
                  <figcaption style={muted}>Preview</figcaption>
                  <div class="og-card">
                    {httpsUrl(draft.ogImage.trim()) && <img src={draft.ogImage.trim()} alt="" />}
                    <div>
                      <small>{linkBase.replace(/^https?:\/\//, '') || 'detur'}</small>
                      <strong>{draft.ogTitle || draft.key || 'Untitled'}</strong>
                      {draft.ogDescription && <p>{draft.ogDescription}</p>}
                    </div>
                  </div>
                </figure>
              )}
            </div>
          </details>

          {err && <Alert variant="destructive">{err}</Alert>}
          <div class="dialog-actions">
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
