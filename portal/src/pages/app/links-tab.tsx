import { Badge, Card, Empty, Table } from 'kinu';
import { Analytics, App, Link, Platform } from '../../lib/types';
import { api } from '../../lib/api';
import { DateRangeFilter } from '../../lib/range';
import { filteredHash } from '../../lib/route';
import { fmt } from '../../lib/format';
import { row, stack, end } from '../../lib/styles';
import { ConfirmDelete } from '../../components/confirm-delete';
import { IconButton, ICONS } from '../../components/icons';
import { Loading } from '../../components/page';
import { ShortLink } from '../../components/short-link';
import { LinkEditor } from './links/link-editor';

const isExpired = (l: Link) => !!l.expiresAt && new Date(l.expiresAt).getTime() < Date.now();
const clipped = { ...row, gap: 6, minWidth: 0, overflow: 'hidden' };

function Notice({ title, children }: { title: string; children: string }) {
  return (
    <Card>
      <Empty>
        <h3>{title}</h3>
        {children}
      </Empty>
    </Card>
  );
}

type RowProps = {
  app: App;
  link: Link;
  stats: Analytics['links'][number] | undefined;
  linkBase: string;
  allTags: string[];
  platform: Platform;
  range: DateRangeFilter;
  onChanged: () => void;
  onError: (e: string) => void;
};

function LinkRow({ app, link: l, stats, linkBase, allTags, platform, range, onChanged, onError }: RowProps) {
  const del = async () => {
    onError('');
    try {
      await api.deleteLink(l.id);
      onChanged();
    } catch (e) {
      onError(String(e));
    }
  };

  return (
    <tr>
      <td>
        <div class="link-cell">
          <div style={row}>
            <ShortLink base={linkBase} linkKey={l.key} />
            {isExpired(l) && <Badge variant="outline">Expired</Badge>}
          </div>
          <div style={clipped}>
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
            <div style={clipped}>
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
        {fmt(stats?.clicks ?? 0)}
      </td>
      <td data-label="Matches" className="num">
        {fmt(stats?.matches ?? 0)}
      </td>
      <td>
        <div style={end}>
          <IconButton
            icon={ICONS.analytics}
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
            onSaved={onChanged}
            trigger={<IconButton icon={ICONS.edit} label="Edit" />}
          />
          <ConfirmDelete
            id={`dlg-del-link-${l.id}`}
            title={`Delete ${l.key}?`}
            body="The short link stops working."
            onConfirm={del}
            icon
          />
        </div>
      </td>
    </tr>
  );
}

type TabProps = Omit<RowProps, 'link' | 'stats'> & {
  links: Link[] | null;
  shown: Link[];
  stats: Analytics | null;
};

export function LinksTab({ links, shown, stats, ...rest }: TabProps) {
  const byId = new Map(stats?.links.map((s) => [s.linkId, s]));
  return (
    <section aria-label="Links" style={stack}>
      {links === null ? (
        <Loading />
      ) : links.length === 0 ? (
        <Notice title="No links yet">Create a link to start tracking clicks and installs.</Notice>
      ) : shown.length === 0 ? (
        <Notice title="No links with this tag">Clear the tag filter to see all links.</Notice>
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
              <LinkRow key={l.id} link={l} stats={byId.get(l.id)} {...rest} />
            ))}
          </tbody>
        </Table>
      )}
    </section>
  );
}
