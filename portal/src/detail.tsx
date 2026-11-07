import { cloneElement, JSX } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { Alert, Badge, Button, Card, Checkbox, Dialog, Empty, Label, Popover, PopoverContent, PopoverTrigger, Table } from 'kinu';
import { Analytics, api, App, DateRangeFilter, Link, Platform, RedirectProfile, SocialPreset, VariantStat } from './api';
import { closeDialog, ConfirmDelete, IconButton, ICONS, Loading, muted, PageHeader, row, ShortLink, useLatest } from './ui';
import { AnalyticsView, Filters, fmt } from './analytics';
import { FraudPanel } from './fraud';
import { SettingsPanel } from './settings';
import { AddWebhookButton, WebhooksPanel } from './webhooks';
import { LinkEditor } from './link-editor';
import { AppTab, filteredHash, TABS } from './route';
import { LinkDraft, draftError, emptyDraft, LinkFields, linkPayload } from './link-fields';

const isExpired = (l: Link) => !!l.expiresAt && new Date(l.expiresAt).getTime() < Date.now();

// App page: one header, tab per job. Links first: most touched.
export function AppPage({
  id,
  tab,
  linkId: routeLinkId = '',
  platform = '',
  range = { days: 7 },
  onName,
}: {
  id: string;
  tab: AppTab;
  linkId?: string;
  platform?: Platform;
  range?: DateRangeFilter;
  onName?: (name: string) => void;
}) {
  const [app, setApp] = useState<App | null>(null);
  const [links, setLinks] = useState<Link[] | null>(null);
  const [stats, setStats] = useState<Analytics | null>(null);
  const [tagFilter, setTagFilter] = useState<string[]>([]);
  const [variants, setVariants] = useState<VariantStat[] | null>(null);

  // filters live in the hash; route re-parses and re-renders
  // stale link id (not in loaded links) counts as all links
  const selectedLinkId = links && !links.some((l) => l.id === routeLinkId) ? '' : routeLinkId;
  const go = (p: { platform?: Platform; range?: DateRangeFilter; linkId?: string }) => {
    const next = filteredHash(id, tab, {
      platform: p.platform ?? platform,
      range: p.range ?? range,
      linkId: p.linkId ?? selectedLinkId,
    });
    if (location.hash !== next) location.hash = next;
  };
  const handlePlatform = (p: Platform) => go({ platform: p });
  const handleRange = (r: DateRangeFilter) => go({ range: r });
  const handleLinkId = (lid: string) => go({ linkId: lid });
  useEffect(() => {
    if (tab === 'analytics' && links && routeLinkId && !selectedLinkId) go({ linkId: '' });
  }, [tab, links, routeLinkId]);
  const [error, setError] = useState('');
  const [statsError, setStatsError] = useState(''); // own slot: a fixed range clears it without hiding an app-load error
  const [linkBase, setLinkBase] = useState('');
  useEffect(() => {
    api.getConfig().then(
      (c) => setLinkBase(c.linkBase ?? ''),
      () => {},
    );
  }, []);

  const appLoad = useLatest();
  const statsLoad = useLatest();
  const variantsLoad = useLatest();
  const loadAll = () => {
    setError('');
    const ok = appLoad.begin();
    return Promise.all([
      api
        .getApp(id)
        .then((a) => ok() && setApp(a))
        .catch((e) => ok() && setError(String(e))),
      api
        .listLinks(id)
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
    const live = statsLoad.begin();
    setStatsLoading(true);
    api
      .getAnalytics(id, range, platform, tab === 'analytics' ? selectedLinkId : undefined)
      .then((s) => {
        if (!live()) return;
        setStats(s);
        setStatsError('');
      })
      .catch((e) => live() && setStatsError(String(e)))
      .finally(() => live() && setStatsLoading(false));
    return () => {
      statsLoad.cancel();
      setStatsLoading(false);
    };
  }, [id, range.days, range.window, range.from, range.to, platform, selectedLinkId, tick, filtered, tab]);
  // spin until loaded, min 600ms so fast reloads show
  const [refreshing, setRefreshing] = useState(false);
  const refresh = () => {
    setRefreshing(true);
    setTick((t) => t + 1);
    Promise.all([loadAll(), new Promise((r) => setTimeout(r, 600))]).then(() => setRefreshing(false));
  };
  // sidebar switcher follows renames too
  useEffect(() => onName?.(app?.name ?? ''), [app?.name]);
  useEffect(() => {
    setApp(null);
    setLinks(null);
    setStats(null);
    setStatsError('');
    setTagFilter([]);
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
  const allTags = [...new Set((links ?? []).flatMap((l) => l.tags ?? []))].sort((a, b) => a.localeCompare(b));

  // variant breakdown belongs to Analytics: only for a split link selected there
  useEffect(() => {
    const l = (links ?? []).find((x) => x.id === selectedLinkId);
    if (tab !== 'analytics' || !app || !l?.hasSplit || range.window) {
      setVariants(null);
      return;
    }
    const live = variantsLoad.begin();
    api.getLinkVariantStats(app.id, l.id, range).then(
      (v) => live() && setVariants(v),
      () => live() && setVariants([]),
    );
    return variantsLoad.cancel;
  }, [tab, app, selectedLinkId, links, range.days, range.window, range.from, range.to]);
  // any selected tag matches (OR), like label filters
  const shown = tagFilter.length ? (links ?? []).filter((l) => l.tags?.some((t) => tagFilter.includes(t))) : (links ?? []);

  return (
    <div>
      <PageHeader
        title={TABS.find((t) => t.tab === tab)!.label}
        actions={
          <>
            {filtered && (
              <Filters
                range={range}
                platform={platform}
                links={tab === 'analytics' ? (links ?? undefined) : undefined}
                linkId={selectedLinkId}
                onRange={handleRange}
                onPlatform={handlePlatform}
                onLinkId={handleLinkId}
              >
                {tab === 'links' && <TagFilter tags={allTags} selected={tagFilter} onChange={setTagFilter} />}
              </Filters>
            )}
            {tab !== 'settings' && (
              <IconButton
                icon="M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7"
                label="Refresh"
                spin={refreshing || statsLoading}
                aria-busy={refreshing || statsLoading}
                onClick={refresh}
                variant="outline"
              />
            )}
            {tab === 'links' && (
              <LinkDialog
                appId={app.id}
                linkBase={linkBase}
                existingTags={allTags}
                profiles={app.redirectProfiles ?? []}
                socialPresets={app.socialPresets ?? []}
                onSaved={refresh}
                trigger={<Button>New link</Button>}
              />
            )}
            {tab === 'webhooks' && <AddWebhookButton />}
          </>
        }
      />
      {(error || (filtered && statsError)) && <Alert variant="destructive">{error || statsError}</Alert>}

      {tab === 'links' && (
        <section aria-label="Links" style={{ display: 'grid', gap: 16 }}>
          {links === null ? (
            <Loading />
          ) : links.length === 0 ? (
            <Card>
              <Empty>
                <h3>No links yet</h3>
                Create a link to start tracking clicks and installs.
              </Empty>
            </Card>
          ) : shown.length === 0 ? (
            <Card>
              <Empty>
                <h3>No links with this tag</h3>
                Clear the tag filter to see all links.
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
                {shown.map((l) => (
                  <tr key={l.id}>
                    <td>
                      <div class="link-cell">
                        <div style={row}>
                          <ShortLink base={linkBase} linkKey={l.key} />
                          {isExpired(l) && <Badge variant="outline">Expired</Badge>}
                        </div>
                        <div style={{ ...row, gap: 6, minWidth: 0, overflow: 'hidden' }}>
                          <span class="link-dest" title={l.url}>
                            {l.url}
                          </span>
                          {l.ruleSummary && (
                            <Badge variant="secondary" title={l.ruleSummary}>
                              {l.ruleSummary}
                            </Badge>
                          )}
                        </div>
                        {!!(l.title || l.tags?.length) && (
                          <div style={{ ...row, gap: 6, minWidth: 0, overflow: 'hidden' }}>
                            {l.title && (
                              <span class="link-title" title={l.title}>
                                {l.title}
                              </span>
                            )}
                            {l.tags?.map((t) => (
                              <Badge key={t} variant="outline">
                                {t}
                              </Badge>
                            ))}
                          </div>
                        )}
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
                        <IconButton
                          icon="M3 3v18h18M18 17V9M13 17V5M8 17v-3"
                          label="Analytics"
                          onClick={() => {
                            location.hash = filteredHash(app.id, 'analytics', { linkId: l.id, platform, range });
                          }}
                        />
                        <LinkEditor
                          appId={app.id}
                          link={l}
                          linkBase={linkBase}
                          existingTags={allTags}
                          profiles={app.redirectProfiles ?? []}
                          socialPresets={app.socialPresets ?? []}
                          onSaved={refresh}
                          trigger={<IconButton icon={ICONS.edit} label="Edit" />}
                        />
                        <ConfirmDelete
                          id={`dlg-del-link-${l.id}`}
                          title={`Delete ${l.key}?`}
                          body="The short link stops working."
                          onConfirm={() => del(l)}
                          icon
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
      {tab === 'analytics' && <AnalyticsView data={stats} variants={variants} range={range} linkId={selectedLinkId} />}
      {tab === 'fraud' && <FraudPanel appId={app.id} tick={tick} />}
      {tab === 'settings' && <SettingsPanel app={app} onApp={setApp} onReload={loadAll} />}
      {tab === 'webhooks' && <WebhooksPanel app={app} tick={tick} />}
    </div>
  );
}

// Multi-select tag filter: a link shows when it carries any selected tag.
function TagFilter({ tags, selected, onChange }: { tags: string[]; selected: string[]; onChange: (t: string[]) => void }) {
  const toggle = (t: string) => onChange(selected.includes(t) ? selected.filter((x) => x !== t) : [...selected, t]);
  return (
    <Popover>
      <PopoverTrigger>
        <Button variant="outline">Tags{selected.length ? ` · ${selected.length}` : ''}</Button>
      </PopoverTrigger>
      <PopoverContent>
        <div class="tag-filter-panel">
          {tags.length === 0 && <p style={muted}>No tags yet.</p>}
          {tags.map((t) => (
            <Label class="check-option" key={t}>
              <Checkbox checked={selected.includes(t)} onChange={() => toggle(t)} />
              {t}
            </Label>
          ))}
          {selected.length > 0 && (
            <Button size="sm" variant="outline" onClick={() => onChange([])}>
              Clear
            </Button>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function LinkDialog({
  appId,
  linkBase,
  existingTags,
  profiles,
  socialPresets,
  onSaved,
  trigger,
}: {
  appId: string;
  linkBase: string;
  existingTags: string[];
  profiles: RedirectProfile[];
  socialPresets: SocialPreset[];
  onSaved: () => void;
  trigger: JSX.Element;
}) {
  const [draft, setDraft] = useState<LinkDraft>(emptyDraft());
  const [err, setErr] = useState('');
  const id = 'dlg-create-link';

  const save = async () => {
    setErr('');
    const bad = draftError(draft, 'create');
    if (bad) {
      setErr(bad);
      return;
    }
    try {
      await api.createLink(appId, { key: draft.key.trim(), ...linkPayload(draft) });
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
            setDraft(emptyDraft());
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
          <h2 style={{ margin: 0 }}>Create link</h2>
          {err && <Alert variant="destructive">{err}</Alert>}
          <LinkFields
            id={id}
            draft={draft}
            setDraft={setDraft}
            existingTags={existingTags}
            profiles={profiles}
            socialPresets={socialPresets}
            linkBase={linkBase}
            showKey
          />

          <div class="dialog-actions">
            <Dialog.Close>
              <Button variant="outline">Cancel</Button>
            </Dialog.Close>
            <Button type="submit">Create</Button>
          </div>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}
