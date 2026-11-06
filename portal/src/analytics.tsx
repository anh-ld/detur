import { ComponentChildren } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { Badge, Button, Card, Empty, Input, Select, Table } from 'kinu';
import { Analytics, DateRangeFilter, DayStat, Link, Mark, Platform, VariantStat } from './api';
import { mono, muted, row } from './ui';

// blue ramp (theme --chart-*) + one warm gray
const BLUE = 'hsl(var(--chart-1))';

// Detour overview parity: filters, totals, daily charts, top events. Plain SVG charts.

type Metric = keyof Omit<DayStat, 'day'>;

const LINES: { key: Metric; label: string; color: string }[] = [
  { key: 'clicks', label: 'Clicks', color: BLUE },
  { key: 'nonOrganic', label: 'Installs via link', color: 'hsl(var(--chart-2))' },
  { key: 'webFallbacks', label: 'Web fallbacks', color: 'hsl(var(--chart-3))' },
  { key: 'opens', label: 'Already installed opens', color: 'hsl(240 4% 62%)' },
];

const BARS: { key: Metric; label: string; color: string }[] = [
  { key: 'nonOrganic', label: 'Non-organic', color: BLUE },
  { key: 'organic', label: 'Organic', color: 'hsl(var(--chart-wash))' },
];

// server ua.InApp ids → display names
const SOURCE_NAMES: Record<string, string> = {
  messenger: 'Messenger',
  facebook: 'Facebook',
  instagram: 'Instagram',
  threads: 'Threads',
  zalo: 'Zalo',
  tiktok: 'TikTok',
  linkedin: 'LinkedIn',
  snapchat: 'Snapchat',
  line: 'LINE',
  x: 'X',
  telegram: 'Telegram',
  wechat: 'WeChat',
  'unknown-inapp': 'Unknown in-app',
};

const fmtDay = (d: string) => {
  if (d.includes('T')) {
    const date = new Date(d);
    return date.toLocaleTimeString('en', { hour: '2-digit', minute: '2-digit', hour12: false, timeZone: 'UTC' });
  }
  return new Date(d + 'T00:00:00Z').toLocaleDateString('en', { month: 'short', day: 'numeric', timeZone: 'UTC' });
};

const sum = (days: DayStat[], k: Metric) => days.reduce((n, d) => n + d[k], 0);

// 12500 → 12,500
export const fmt = (n: number) => n.toLocaleString('en');

export const Skeleton = ({ w = '100%', h }: { w?: number | string; h: number }) => (
  <span class="skeleton" style={{ width: w, height: h }} />
);

export type RangePreset =
  { window: '15m' | '1h' | '3h' | '12h'; label: string; days?: never } | { days: number; label: string; window?: never };

export const RANGE_OPTIONS: readonly RangePreset[] = [
  { window: '15m', label: 'Last 15 minutes' },
  { window: '1h', label: 'Last 1 hour' },
  { window: '3h', label: 'Last 3 hours' },
  { window: '12h', label: 'Last 12 hours' },
  { days: 7, label: 'Last 7 days' },
  { days: 14, label: 'Last 14 days' },
  { days: 30, label: 'Last 30 days' },
  { days: 60, label: 'Last 60 days' },
  { days: 90, label: 'Last 90 days' },
  { days: 180, label: 'Last 180 days' },
  { days: 365, label: 'Last 365 days' },
] as const;

const toDateStr = (d: Date) => d.toISOString().slice(0, 10);
const todayStr = () => toDateStr(new Date());
const daysAgoStr = (n: number) => {
  const d = new Date();
  d.setDate(d.getDate() - (n - 1));
  return toDateStr(d);
};

export function DateRangePicker({
  ariaLabel = 'Date range',
  range,
  onRange,
  allowWindows = true,
}: {
  ariaLabel?: string;
  allowWindows?: boolean;
  range: DateRangeFilter;
  onRange: (r: DateRangeFilter) => void;
}) {
  const [customMode, setCustomMode] = useState(Boolean(range.from && range.to));
  const [from, setFrom] = useState(range.from ?? daysAgoStr(range.days ?? 7));
  const [to, setTo] = useState(range.to ?? todayStr());

  useEffect(() => {
    if (range.from && range.to) {
      setCustomMode(true);
      setFrom(range.from);
      setTo(range.to);
    } else {
      setCustomMode(false);
    }
  }, [range.days, range.window, range.from, range.to]);

  const selectVal = customMode ? 'custom' : range.window ? range.window : String(range.days ?? 7);

  const handleSelect = (val: string) => {
    if (val === 'custom') {
      setCustomMode(true);
      const f = from || daysAgoStr(range.days ?? 7);
      const t = to || todayStr();
      setFrom(f);
      setTo(t);
      onRange({ from: f, to: t, linkId: range.linkId });
    } else if (val === '15m' || val === '1h' || val === '3h' || val === '12h') {
      setCustomMode(false);
      onRange({ window: val, linkId: range.linkId });
    } else {
      setCustomMode(false);
      onRange({ days: Number(val), linkId: range.linkId });
    }
  };

  const handleApply = (e?: Event) => {
    e?.preventDefault();
    if (from && to && from <= to) {
      onRange({ from, to, linkId: range.linkId });
    }
  };

  return (
    <div style={{ ...row, flexWrap: 'wrap', gap: 6 }}>
      <Select aria-label={ariaLabel} value={selectVal} onChange={(e) => handleSelect(e.currentTarget.value)}>
        {RANGE_OPTIONS.filter((o) => allowWindows || !o.window).map((o) => (
          <option key={o.window ?? o.days} value={o.window ?? String(o.days)}>
            {o.label}
          </option>
        ))}
        <option value="custom">Custom range</option>
      </Select>
      {customMode && (
        <form onSubmit={handleApply} style={{ ...row, gap: 6, margin: 0, flexWrap: 'wrap' }}>
          <Input
            type="date"
            aria-label="Start date"
            value={from}
            max={to || todayStr()}
            onInput={(e) => setFrom(e.currentTarget.value)}
            style={{ width: 'auto', padding: '4px 8px', fontSize: 13 }}
          />
          <span style={muted}>to</span>
          <Input
            type="date"
            aria-label="End date"
            value={to}
            min={from}
            max={todayStr()}
            onInput={(e) => setTo(e.currentTarget.value)}
            style={{ width: 'auto', padding: '4px 8px', fontSize: 13 }}
          />
          <Button type="submit" size="sm" variant="secondary" disabled={!from || !to || from > to}>
            Apply
          </Button>
        </form>
      )}
    </div>
  );
}

export function Filters({
  range,
  days,
  platform,
  links,
  linkId,
  children,
  onRange,
  onDays,
  onPlatform,
  onLinkId,
}: {
  range?: DateRangeFilter;
  days?: number;
  platform: Platform;
  links?: Link[];
  linkId?: string;
  children?: ComponentChildren;
  onRange?: (r: DateRangeFilter) => void;
  onDays?: (d: number) => void;
  onPlatform: (p: Platform) => void;
  onLinkId?: (id: string) => void;
}) {
  const currentRange: DateRangeFilter = range ?? { days: days ?? 7 };
  const handleRange = (r: DateRangeFilter) => {
    if (onRange) onRange(r);
    if (onDays && r.days) onDays(r.days);
  };

  return (
    <div style={{ ...row, flexWrap: 'wrap', gap: 6 }}>
      {links && onLinkId && (
        <Select aria-label="Link" value={linkId ?? ''} onChange={(e) => onLinkId(e.currentTarget.value)}>
          <option value="">All links</option>
          {links.map((l) => (
            <option key={l.id} value={l.id}>
              {l.title ? `${l.title} (/${l.key})` : `/${l.key}`}
            </option>
          ))}
        </Select>
      )}
      <Select aria-label="Platform" value={platform} onChange={(e) => onPlatform(e.currentTarget.value as Platform)}>
        <option value="">All platforms</option>
        <option value="ios">iOS</option>
        <option value="android">Android</option>
        <option value="desktop">Desktop</option>
      </Select>
      <DateRangePicker range={currentRange} onRange={handleRange} />
      {children}
    </div>
  );
}

// Lucide icon paths (ISC), 24px grid
const ICONS: Partial<Record<Metric, string>> = {
  clicks:
    'M14 4.1 12 6M5.1 8l-2.9-.8M6 12l-1.9 2M7.2 2.2 8 5.1M9.037 9.69a.498.498 0 0 1 .653-.653l11 4.5a.5.5 0 0 1-.074.949l-4.349 1.041a1 1 0 0 0-.74.739l-1.04 4.35a.5.5 0 0 1-.95.074z',
  opens: 'M7 2h10a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2zM12 18h.01',
  nonOrganic: 'M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3',
  webFallbacks: 'M12 2a10 10 0 1 0 0 20a10 10 0 1 0 0-20M12 2a14.5 14.5 0 0 0 0 20a14.5 14.5 0 0 0 0-20M2 12h20',
};

// stat: icon + label, total; hint as tooltip
function Stat({ data, metric, hint, extra }: { data: Analytics | null; metric: Metric; hint: string; extra?: ComponentChildren }) {
  const s = LINES.find((l) => l.key === metric)!;
  return (
    <div class="stat" title={hint}>
      <p class="stat-label">
        <span style={{ color: s.color, display: 'inline-grid' }}>
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.75"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d={ICONS[metric]} />
          </svg>
        </span>
        {s.label}
      </p>
      <div class="stat-value">
        {data ? fmt(sum(data.days, metric)) : <Skeleton w={56} h={26} />}
        {extra}
      </div>
    </div>
  );
}

// chart card: title + subtitle left, big total right
function ChartCard({
  title,
  sub,
  data,
  total,
  unit,
  children,
}: {
  title: string;
  sub: string;
  data: Analytics | null;
  total: number;
  unit: string;
  children: ComponentChildren;
}) {
  return (
    <Card class="chart-card">
      <div class="chart-head">
        <div>
          <h3>{title}</h3>
          <p style={muted}>{sub}</p>
        </div>
        <div class="chart-total">
          {data ? <strong>{fmt(total)}</strong> : <Skeleton w={72} h={36} />}
          <span style={muted}>{unit}</span>
        </div>
      </div>
      {children}
    </Card>
  );
}

// ranked rows, tinted bar behind each = share of top row
function BarList({
  label,
  items,
  empty,
  mono: isMono,
}: {
  label: string;
  items: { name: ComponentChildren; count: number; key: string }[] | null;
  empty: [string, string];
  mono?: boolean;
}) {
  if (!items)
    return (
      <div style={{ display: 'grid', gap: 8 }}>
        {[0, 1, 2].map((i) => (
          <Skeleton key={i} h={36} />
        ))}
      </div>
    );
  if (items.length === 0)
    return (
      <div class="chart-empty" style={{ height: 160 }}>
        <strong>{empty[0]}</strong>
        {empty[1]}
      </div>
    );
  const top = Math.max(1, ...items.map((i) => i.count));
  return (
    <ul class="bar-list" aria-label={label}>
      {items.map((i) => (
        <li key={i.key}>
          <span class="bar-fill" style={{ width: `${(i.count / top) * 100}%` }} />
          <span class="bar-name" style={isMono ? mono : undefined}>
            {i.name}
          </span>
          <span class="num">{fmt(i.count)}</span>
        </li>
      ))}
    </ul>
  );
}

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
  const days = data?.days ?? [];
  const clicks = sum(days, 'clicks');
  const viaLink = sum(days, 'nonOrganic');
  const installs = viaLink + sum(days, 'organic');
  const sources = data?.sources ?? []; // older servers / fixtures omit it
  const retention = data?.retention ?? [];
  const conversions = data?.conversions ?? [];
  return (
    <div style={{ display: 'grid', gap: 16 }}>
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

      {variants && !windowed && (
        <section aria-label="Variants" style={{ display: 'grid', gap: 12 }}>
          <div style={{ display: 'grid', gap: 4 }}>
            <h3 style={{ margin: 0 }}>Variants</h3>
            <p style={muted}>Clicks, installs and conversion per A/B variant</p>
          </div>
          {variants.length === 0 ? (
            <Empty>
              <h3>No variant stats yet</h3>
              <p style={muted}>Variant clicks and matched installs appear here after taps.</p>
            </Empty>
          ) : (
            <Table>
              <thead>
                <tr>
                  <th>Variant</th>
                  <th style={{ textAlign: 'right' }}>Clicks</th>
                  <th style={{ textAlign: 'right' }}>Installs</th>
                  <th style={{ textAlign: 'right' }}>Conversion</th>
                </tr>
              </thead>
              <tbody>
                {variants.map((v) => (
                  <tr key={v.variant}>
                    <td>
                      <strong>{v.variant}</strong>
                    </td>
                    <td data-label="Clicks" className="num">
                      {v.clicks.toLocaleString()}
                    </td>
                    <td data-label="Installs" className="num">
                      {v.installs.toLocaleString()}
                    </td>
                    <td data-label="Conversion" className="num">
                      {(v.conversion_rate * 100).toFixed(1)}%
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </section>
      )}

      <div className="duo">
        <ChartCard title="Activity" sub="Clicks, opens, installs per day" data={data} total={clicks} unit="clicks">
          <ChartOr data={data} series={LINES} kind="line" empty={['No activity', 'Clicks, opens and installs show here.']} />
        </ChartCard>
        <ChartCard title="Organic vs. non-organic" sub="Installs with and without a click" data={data} total={installs} unit="installs">
          <ChartOr data={data} series={BARS} kind="bar" empty={['No installs', 'Installs with and without a click show here.']} />
        </ChartCard>
      </div>

      <div className="duo">
        <Card class="chart-card">
          <div class="chart-head">
            <div>
              <h3>Top events</h3>
              <p style={muted}>{data ? `${fmt(data.events.length)} event names from the SDK` : 'Events from the SDK'}</p>
            </div>
          </div>
          <BarList
            label="Top events"
            mono
            items={
              data && data.events.map((e) => ({ key: e.event, name: e.event || <span style={muted}>(no name)</span>, count: e.count }))
            }
            empty={
              windowed && linkId
                ? ["Events aren't per link for sub-day windows", 'Pick a day range or all links.']
                : ['No events', 'Events sent by the SDK show here.']
            }
          />
        </Card>
        <Card class="chart-card">
          <div class="chart-head">
            <div>
              <h3>In-app sources</h3>
              <p style={muted}>Clicks from in-app browsers, all platforms</p>
            </div>
          </div>
          <BarList
            label="In-app sources"
            items={data && sources.map((s) => ({ key: s.source, name: SOURCE_NAMES[s.source] ?? s.source, count: s.count }))}
            empty={['No in-app clicks', 'Taps from Messenger, Zalo, TikTok and other in-app browsers show here.']}
          />
        </Card>
      </div>

      {windowed ? (
        <p style={muted}>Conversions and retention use daily data. Pick a day range to see them.</p>
      ) : (
        <>
      <section aria-label="Retention by link" style={{ display: 'grid', gap: 12, marginTop: 16 }}>
        <div style={{ display: 'grid', gap: 4 }}>
          <h3 style={{ margin: 0 }}>Retention by link</h3>
          <p style={muted}>Tagged devices back on day 1, 7 and 30. All platforms.</p>
        </div>
        {!data ? (
          <Skeleton h={36} />
        ) : retention.length === 0 ? (
          <EmptySection title="No tagged devices yet">
            Turn on Tag deferred links in Settings, then send <code>detur_link</code> once from the app.
          </EmptySection>
        ) : (
          <Table>
            <thead>
              <tr>
                <th>Link</th>
                <th style={{ textAlign: 'right' }}>Devices</th>
                <th style={{ textAlign: 'right' }}>Day 1</th>
                <th style={{ textAlign: 'right' }}>Day 7</th>
                <th style={{ textAlign: 'right' }}>Day 30</th>
              </tr>
            </thead>
            <tbody>
              {retention.map((r) => (
                <tr key={r.linkId}>
                  <td style={mono}>{r.key}</td>
                  <td data-label="Devices" className="num">
                    {fmt(r.devices)}
                  </td>
                  {(['d1', 'd7', 'd30'] as const).map((m) => (
                    <td key={m} data-label={`Day ${m.slice(1)}`} className="num" title={markTitle(r[m])}>
                      {rate(r[m])}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </section>

      <section aria-label="Conversions by link" style={{ display: 'grid', gap: 12, marginTop: 16 }}>
        <div style={{ display: 'grid', gap: 4 }}>
          <h3 style={{ margin: 0 }}>Conversions by link</h3>
          <p style={muted}>Events from tagged devices. All platforms.</p>
        </div>
        {!data ? (
          <Skeleton h={36} />
        ) : conversions.length === 0 ? (
          <EmptySection title="No conversions yet">Events from tagged devices show here.</EmptySection>
        ) : (
          <Table>
            <thead>
              <tr>
                <th>Link</th>
                <th>Event</th>
                <th style={{ textAlign: 'right' }}>Count</th>
              </tr>
            </thead>
            <tbody>
              {conversions.map((c) => (
                <tr key={c.linkId + c.event}>
                  <td style={mono}>{c.key}</td>
                  <td style={mono}>{c.event}</td>
                  <td data-label="Count" className="num">
                    {fmt(c.count)}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </section>
        </>
      )}
    </div>
  );
}

// empty in range: same Card + Empty as Links tab
const EmptySection = ({ title, children }: { title: string; children: ComponentChildren }) => (
  <Card>
    <Empty>
      <h3>{title}</h3>
      <p style={{ margin: 0 }}>{children}</p>
    </Empty>
  </Card>
);

// share of mark's devices that returned; – = no cohort reached the mark in range
const rate = (m: Mark) => (m.devices === 0 ? '–' : `${Math.round((m.returned / m.devices) * 100)}%`);
const markTitle = (m: Mark) =>
  m.devices === 0 ? 'No cohort reached this day in range' : `${fmt(m.returned)} of ${fmt(m.devices)} devices`;

// measure container so SVG axis text stays real pixel size
function useWidth() {
  const ref = useRef<HTMLDivElement>(null);
  const [w, setW] = useState(640);
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => setW(el.clientWidth || 640));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  return [ref, w] as const;
}

type Series = { key: Metric; label: string; color: string }[];

// chart | skeleton while loading | message when all 0
function ChartOr({ data, series, kind, empty }: { data: Analytics | null; series: Series; kind: 'line' | 'bar'; empty: [string, string] }) {
  if (!data) return <Skeleton h={H + 28} />;
  const total = series.reduce((n, s) => n + sum(data.days, s.key), 0);
  if (total === 0)
    return (
      <div class="chart-empty" style={{ height: H + 28 }}>
        <strong>{empty[0]}</strong>
        {empty[1]}
      </div>
    );
  return <Chart days={data.days} series={series} kind={kind} />;
}

const H = 200;
const PAD = { l: 32, r: 8, t: 8, b: 24 };

function Chart({ days, series, kind }: { days: DayStat[]; series: Series; kind: 'line' | 'bar' }) {
  const [ref, w] = useWidth();
  const [hoverAt, setHover] = useState<number | null>(null);
  // range shrank under a resting pointer: index past the new data would throw and blank the page
  const hover = hoverAt !== null && hoverAt < days.length ? hoverAt : null;
  const n = Math.max(days.length, 1);
  const plotW = w - PAD.l - PAD.r;
  const plotH = H - PAD.t - PAD.b;
  const peak = Math.max(
    1,
    ...days.map((d) => (kind === 'bar' ? series.reduce((s, x) => s + d[x.key], 0) : Math.max(...series.map((x) => d[x.key])))),
  );
  const top = Math.ceil(peak / 4) * 4; // 4 even gridlines
  const slot = plotW / n;
  const x = (i: number) => PAD.l + slot * i + slot / 2;
  const y = (v: number) => PAD.t + plotH - (v / top) * plotH;
  const every = Math.ceil(n / Math.max(2, Math.floor(plotW / 64))); // ~64px per x label

  return (
    <div ref={ref} style={{ display: 'grid', gap: 8, minWidth: 0, position: 'relative' }}>
      <svg
        width={w}
        height={H}
        style={{ display: 'block', overflow: 'visible' }}
        role="img"
        aria-label={series.map((s) => s.label).join(', ') + ' per day'}
        onPointerLeave={() => setHover(null)}
      >
        {[0, 1, 2, 3, 4].map((g) => (
          <g key={g}>
            <line x1={PAD.l} x2={w - PAD.r} y1={y((top * g) / 4)} y2={y((top * g) / 4)} stroke="hsl(var(--k-border))" />
            <text x={PAD.l - 6} y={y((top * g) / 4) + 4} text-anchor="end" font-size="11" fill="hsl(var(--k-muted-foreground))">
              {fmt((top * g) / 4)}
            </text>
          </g>
        ))}
        {days.map((d, i) =>
          i % every === 0 ? (
            <text key={d.day} x={x(i)} y={H - 6} text-anchor="middle" font-size="11" fill="hsl(var(--k-muted-foreground))">
              {fmtDay(d.day)}
            </text>
          ) : null,
        )}
        {kind === 'line'
          ? [...series].reverse().map((s) => (
              // first series drawn last = on top
              <polyline
                key={s.key}
                fill="none"
                stroke={s.color}
                stroke-width="2"
                stroke-linejoin="round"
                points={days.map((d, i) => `${x(i)},${y(d[s.key])}`).join(' ')}
              />
            ))
          : days.map((d, i) => {
              let base = 0;
              return series.map((s) => {
                const v = d[s.key];
                const rect = (
                  <rect
                    key={d.day + s.key}
                    x={x(i) - (slot * 0.6) / 2}
                    width={slot * 0.6}
                    y={y(base + v)}
                    height={y(base) - y(base + v)}
                    fill={s.color}
                    rx="2"
                  />
                );
                base += v;
                return rect;
              });
            })}
        {/* hovered day: guide line + dot per line series */}
        {hover !== null && (
          <g pointer-events="none">
            <line x1={x(hover)} x2={x(hover)} y1={PAD.t} y2={PAD.t + plotH} stroke="hsl(var(--k-input))" stroke-dasharray="3 3" />
            {kind === 'line' &&
              series.map((s) => (
                <circle
                  key={s.key}
                  cx={x(hover)}
                  cy={y(days[hover][s.key])}
                  r="3.5"
                  fill="hsl(var(--k-card))"
                  stroke={s.color}
                  stroke-width="2"
                />
              ))}
          </g>
        )}
        {/* hover target per day */}
        {days.map((d, i) => (
          <rect
            key={'h' + d.day}
            class="chart-hover"
            x={PAD.l + slot * i}
            y={PAD.t}
            width={slot}
            height={plotH}
            onPointerEnter={() => setHover(i)}
          />
        ))}
      </svg>
      {hover !== null && (
        <div
          class="chart-tip"
          role="status"
          style={{
            top: PAD.t,
            // past middle: flip left, card stays inside chart
            ...(x(hover) > w / 2 ? { right: w - x(hover) + 10 } : { left: x(hover) + 10 }),
          }}
        >
          <strong>{fmtDay(days[hover].day)}</strong>
          {series.map((s) => (
            <span key={s.key} style={{ ...row, gap: 6, justifyContent: 'space-between' }}>
              <span style={{ ...row, gap: 6 }}>
                <span style={{ width: 8, height: 8, borderRadius: 2, background: s.color }} />
                {s.label}
              </span>
              <span style={{ fontVariantNumeric: 'tabular-nums' }}>{fmt(days[hover][s.key])}</span>
            </span>
          ))}
        </div>
      )}
      <div style={{ ...row, flexWrap: 'wrap', gap: 16, justifyContent: 'center' }}>
        {series.map((s) => (
          <span key={s.key} style={{ ...row, gap: 6, fontSize: 13 }}>
            <span style={{ width: 10, height: 10, borderRadius: 3, background: s.color }} />
            {s.label}
          </span>
        ))}
      </div>
    </div>
  );
}
