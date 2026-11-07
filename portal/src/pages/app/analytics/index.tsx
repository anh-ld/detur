import { Badge, Card } from 'kinu';
import { Analytics, VariantStat } from '../../../lib/types';
import { DateRangeFilter } from '../../../lib/range';
import { fmt } from '../../../lib/format';
import { muted } from '../../../lib/styles';
import { BARS, LINES, SOURCE_NAMES } from './constants';
import { sum } from './helpers';
import { ChartOr } from './chart';
import { BarList, ChartCard, Stat } from './stat-cards';
import { ConversionsSection, RetentionSection, VariantsSection } from './tables';

export function AnalyticsView({
  data,
  variants,
  range,
  linkId,
}: {
  data: Analytics | null;
  variants?: VariantStat[] | null;
  range?: DateRangeFilter;
  linkId?: string;
}) {
  const windowed = Boolean(range?.window); // sub-day: no daily cohorts, no per-link events
  return (
    <div style={{ display: 'grid', gap: 16 }}>
      <Stats data={data} />
      {variants && !windowed && <VariantsSection variants={variants} />}
      <Charts data={data} />
      <Breakdowns data={data} windowed={windowed} linkId={linkId} />
      {windowed ? (
        <p style={muted}>Conversions and retention use daily data. Pick a day range to see them.</p>
      ) : (
        <>
          <div style={{ marginTop: 16 }}>
            <RetentionSection data={data} />
          </div>
          <div style={{ marginTop: 16 }}>
            <ConversionsSection data={data} />
          </div>
        </>
      )}
    </div>
  );
}

function Stats({ data }: { data: Analytics | null }) {
  const days = data?.days ?? [];
  const clicks = sum(days, 'clicks');
  const viaLink = sum(days, 'nonOrganic');
  return (
    <Card class="stats">
      <Stat data={data} metric="clicks" hint="Link opens on any platform" />
      <Stat data={data} metric="opens" hint="Opened straight in the app" />
      <Stat
        data={data}
        metric="nonOrganic"
        hint="Installs matched to a click"
        extra={
          data &&
          clicks > 0 && (
            <Badge variant="secondary" style={{ letterSpacing: 'normal', whiteSpace: 'nowrap' }}>
              {Math.round((viaLink / clicks) * 100)}% match
            </Badge>
          )
        }
      />
      <Stat data={data} metric="webFallbacks" hint="Sent to the web URL" />
    </Card>
  );
}

function Charts({ data }: { data: Analytics | null }) {
  const days = data?.days ?? [];
  const installs = sum(days, 'nonOrganic') + sum(days, 'organic');
  return (
    <div className="duo">
      <ChartCard title="Activity" sub="Clicks, opens, installs per day" loading={!data} total={sum(days, 'clicks')} unit="clicks">
        <ChartOr data={data} series={LINES} kind="line" empty={['No activity', 'Clicks, opens and installs show here.']} />
      </ChartCard>
      <ChartCard title="Organic vs. non-organic" sub="Installs with and without a click" loading={!data} total={installs} unit="installs">
        <ChartOr data={data} series={BARS} kind="bar" empty={['No installs', 'Installs with and without a click show here.']} />
      </ChartCard>
    </div>
  );
}

function Breakdowns({ data, windowed, linkId }: { data: Analytics | null; windowed: boolean; linkId?: string }) {
  const sources = data?.sources ?? []; // older servers / fixtures omit it
  return (
    <div className="duo">
      <ChartCard title="Top events" sub={data ? `${fmt(data.events.length)} event names from the SDK` : 'Events from the SDK'}>
        <BarList
          label="Top events"
          mono
          items={data && data.events.map((e) => ({ key: e.event, name: e.event || <span style={muted}>(no name)</span>, count: e.count }))}
          empty={
            windowed && linkId
              ? ["Events aren't per link for sub-day windows", 'Pick a day range or all links.']
              : ['No events', 'Events sent by the SDK show here.']
          }
        />
      </ChartCard>
      <ChartCard title="In-app sources" sub="Clicks from in-app browsers, all platforms">
        <BarList
          label="In-app sources"
          items={data && sources.map((s) => ({ key: s.source, name: SOURCE_NAMES[s.source] ?? s.source, count: s.count }))}
          empty={['No in-app clicks', 'Taps from Messenger, Zalo, TikTok and other in-app browsers show here.']}
        />
      </ChartCard>
    </div>
  );
}
