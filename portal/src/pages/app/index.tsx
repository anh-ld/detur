import { useEffect, useState } from 'preact/hooks';
import { Alert, Button, Card, Empty } from 'kinu';
import { Platform } from '../../lib/types';
import { DateRangeFilter } from '../../lib/range';
import { IconButton } from '../../components/icons';
import { Loading, PageHeader } from '../../components/page';
import { Filters } from '../../components/filters';
import { AppTab, filteredHash, TABS } from '../../lib/route';
import { AnalyticsView } from './analytics';
import { FraudPanel } from './fraud';
import { SettingsPanel } from './settings';
import { AddWebhookButton, WebhooksPanel } from './webhooks';
import { LinkDialog } from './link-dialog';
import { LinksTab } from './links-tab';
import { TagFilter } from './tag-filter';
import { useApp } from './use-app';

// One header, a tab per job. Links first: most touched.
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
  const d = useApp(id, tab, range, platform, routeLinkId);
  const { app, links, selectedLinkId } = d;
  const [tagFilter, setTagFilter] = useState<string[]>([]);

  // filters live in the hash; route re-parses and re-renders
  const go = (p: { platform?: Platform; range?: DateRangeFilter; linkId?: string }) => {
    const next = filteredHash(id, tab, {
      platform: p.platform ?? platform,
      range: p.range ?? range,
      linkId: p.linkId ?? selectedLinkId,
    });
    if (location.hash !== next) location.hash = next;
  };
  useEffect(() => {
    if (tab === 'analytics' && links && routeLinkId && !selectedLinkId) go({ linkId: '' });
  }, [tab, links, routeLinkId]);
  useEffect(() => setTagFilter([]), [id]);
  // sidebar switcher follows renames too
  useEffect(() => onName?.(app?.name ?? ''), [app?.name]);

  if (app === null && d.error === '') return <Loading />;
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

  const filtered = tab === 'links' || tab === 'analytics';
  const allTags = [...new Set((links ?? []).flatMap((l) => l.tags ?? []))].sort((a, b) => a.localeCompare(b));
  // any selected tag matches (OR), like label filters
  const shown = tagFilter.length ? (links ?? []).filter((l) => l.tags?.some((t) => tagFilter.includes(t))) : (links ?? []);
  const busy = d.refreshing || d.statsLoading;
  const error = d.error || (filtered && d.statsError);

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
                onRange={(r) => go({ range: r })}
                onPlatform={(p) => go({ platform: p })}
                onLinkId={(lid) => go({ linkId: lid })}
              >
                {tab === 'links' && <TagFilter tags={allTags} selected={tagFilter} onChange={setTagFilter} />}
              </Filters>
            )}
            {tab !== 'settings' && (
              <IconButton
                icon="M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7"
                label="Refresh"
                spin={busy}
                aria-busy={busy}
                onClick={d.refresh}
                variant="outline"
              />
            )}
            {tab === 'links' && (
              <LinkDialog
                appId={app.id}
                linkBase={d.linkBase}
                existingTags={allTags}
                profiles={app.redirectProfiles ?? []}
                socialPresets={app.socialPresets ?? []}
                onSaved={d.refresh}
                trigger={<Button>New link</Button>}
              />
            )}
            {tab === 'webhooks' && <AddWebhookButton />}
          </>
        }
      />
      {error && <Alert variant="destructive">{error}</Alert>}

      {tab === 'links' && (
        <LinksTab
          app={app}
          links={links}
          shown={shown}
          stats={d.stats}
          linkBase={d.linkBase}
          allTags={allTags}
          platform={platform}
          range={range}
          onChanged={d.refresh}
          onError={d.setError}
        />
      )}
      {tab === 'analytics' && <AnalyticsView data={d.stats} variants={d.variants} range={range} linkId={selectedLinkId} />}
      {tab === 'fraud' && <FraudPanel appId={app.id} tick={d.tick} />}
      {tab === 'settings' && <SettingsPanel app={app} onApp={d.setApp} onReload={d.load} />}
      {tab === 'webhooks' && <WebhooksPanel app={app} tick={d.tick} />}
    </div>
  );
}
