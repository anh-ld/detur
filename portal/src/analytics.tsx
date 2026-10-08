import { ComponentChildren } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { Badge, Card, Select, Table } from 'kinu';
import { Analytics, DayStat, Mark, Platform } from './api';
import { mono, muted, row } from './ui';

// Series colors: a blue ramp around the brand cyan, one warm gray.
const CYAN = 'hsl(205 87% 59%)';

// Analytics view (Detour overview parity): filters, totals, daily charts, top events. Charts are plain SVG.

type Metric = keyof Omit<DayStat, 'day'>;

const LINES: { key: Metric; label: string; color: string }[] = [
  { key: 'clicks', label: 'Clicks', color: CYAN },
  { key: 'nonOrganic', label: 'Installs via link', color: 'hsl(212 72% 34%)' },
  { key: 'webFallbacks', label: 'Web fallbacks', color: 'hsl(192 70% 52%)' },
  { key: 'opens', label: 'Already installed opens', color: 'hsl(25 6% 55%)' },
];

const BARS: { key: Metric; label: string; color: string }[] = [
  { key: 'nonOrganic', label: 'Non-organic', color: CYAN },
  { key: 'organic', label: 'Organic', color: 'hsl(204 77% 86%)' },
];

// In-app source ids (server ua.InApp) → display names.
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

const fmtDay = (d: string) =>
  new Date(d + 'T00:00:00Z').toLocaleDateString('en', { month: 'short', day: 'numeric', timeZone: 'UTC' });

const sum = (days: DayStat[], k: Metric) => days.reduce((n, d) => n + d[k], 0);

// Thousands separators: 12500 → 12,500.
export const fmt = (n: number) => n.toLocaleString('en');

// Placeholder bar while data loads.
export const Skeleton = ({ w = '100%', h }: { w?: number | string; h: number }) => (
  <span class="skeleton" style={{ width: w, height: h }} />
);

export function Filters({
  days,
  platform,
  onDays,
  onPlatform,
}: {
  days: number;
  platform: Platform;
  onDays: (d: number) => void;
  onPlatform: (p: Platform) => void;
}) {
  return (
    <div style={{ ...row, flexWrap: 'wrap' }}>
      <Select
        aria-label="Platform"
        value={platform}
        onChange={(e) => onPlatform(e.currentTarget.value as Platform)}
      >
        <option value="">All platforms</option>
        <option value="ios">iOS</option>
        <option value="android">Android</option>
        <option value="desktop">Desktop</option>
      </Select>
      <Select aria-label="Date range" value={String(days)} onChange={(e) => onDays(Number(e.currentTarget.value))}>
        <option value="7">Last 7 days</option>
        <option value="30">Last 30 days</option>
        <option value="90">Last 90 days</option>
      </Select>
    </div>
  );
}

// Tile icons: Lucide paths (ISC), 24px grid, outline.
const ICONS: Partial<Record<Metric, string>> = {
  clicks:
    'M14 4.1 12 6M5.1 8l-2.9-.8M6 12l-1.9 2M7.2 2.2 8 5.1M9.037 9.69a.498.498 0 0 1 .653-.653l11 4.5a.5.5 0 0 1-.074.949l-4.349 1.041a1 1 0 0 0-.74.739l-1.04 4.35a.5.5 0 0 1-.95.074z',
  opens: 'M7 2h10a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2zM12 18h.01',
  nonOrganic: 'M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3',
  webFallbacks: 'M12 2a10 10 0 1 0 0 20a10 10 0 1 0 0-20M12 2a14.5 14.5 0 0 0 0 20a14.5 14.5 0 0 0 0-20M2 12h20',
};

// Stat tile: icon + label, total, one-line hint, per-day sparkline in the chart's color.
function Tile({
  data,
  metric,
  hint,
  extra,
}: {
  data: Analytics | null;
  metric: Metric;
  hint: string;
  extra?: ComponentChildren;
}) {
  const s = LINES.find((l) => l.key === metric)!;
  const days = data?.days ?? [];
  const vals = days.map((d) => d[metric]);
  const top = Math.max(1, ...vals);
  const pts = vals.map((v, i) => `${(i / Math.max(1, vals.length - 1)) * 100},${30 - (v / top) * 28}`).join(' ');
  return (
    <Card padding="sm" class="tile">
      <p style={{ ...muted, ...row, gap: 6 }}>
        <span class="tile-icon" style={{ color: s.color }}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75"
            stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <path d={ICONS[metric]} />
          </svg>
        </span>
        {s.label}
      </p>
      <div style={{ ...row, fontSize: 28, fontWeight: 400 }}>
        {data ? fmt(sum(days, metric)) : <Skeleton w={56} h={28} />}
        {extra}
      </div>
      <p style={{ ...muted, fontSize: 12 }}>{hint}</p>
      <svg viewBox="0 0 100 32" preserveAspectRatio="none" width="100%" height="32" aria-hidden="true">
        <line x1="0" x2="100" y1="30" y2="30" stroke="hsl(var(--k-border))" vector-effect="non-scaling-stroke" />
        {!data && <rect x="0" y="4" width="100" height="26" rx="2" class="skeleton-fill" />}
        {vals.length > 1 && (
          <>
            <polygon points={`0,30 ${pts} 100,30`} fill={s.color} opacity="0.18" />
            <polyline
              points={pts}
              fill="none"
              stroke={s.color}
              stroke-width="2"
              stroke-linejoin="round"
              vector-effect="non-scaling-stroke"
            />
          </>
        )}
      </svg>
    </Card>
  );
}

export function AnalyticsView({ data }: { data: Analytics | null }) {
  const days = data?.days ?? [];
  const clicks = sum(days, 'clicks');
  const viaLink = sum(days, 'nonOrganic');
  const sources = data?.sources ?? []; // older servers / fixtures omit it
  const retention = data?.retention ?? [];
  const conversions = data?.conversions ?? [];
  return (
    <div style={{ display: 'grid', gap: 16 }}>
      <div className="stats" style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(0, 1fr))', gap: 16 }}>
        <Tile data={data} metric="clicks" hint="Link opens on any platform" />
        <Tile data={data} metric="opens" hint="Opened straight in the app" />
        <Tile
          data={data}
          metric="nonOrganic"
          hint="Installs matched to a click"
          extra={
            data && clicks > 0 && (
              <Badge variant="secondary" style={{ letterSpacing: 'normal', whiteSpace: 'nowrap' }}>
                {Math.round((viaLink / clicks) * 100)}% match
              </Badge>
            )
          }
        />
        <Tile data={data} metric="webFallbacks" hint="Sent to the web URL" />
      </div>

      <Card style={{ display: 'grid', gap: 12 }}>
        <h3 style={{ margin: 0 }}>Activity</h3>
        <ChartOr data={data} series={LINES} kind="line" empty="No activity in this range." />
      </Card>

      <div className="split" style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 16 }}>
        <Card style={{ display: 'grid', gap: 12, alignContent: 'start' }}>
          <div style={{ display: 'grid', gap: 4 }}>
            <h3 style={{ margin: 0 }}>Organic vs. non-organic</h3>
            <p style={muted}>Installs matched to a link vs. installs with no click.</p>
          </div>
          <ChartOr data={data} series={BARS} kind="bar" empty="No installs in this range." />
        </Card>
        <Card style={{ display: 'grid', gap: 12, alignContent: 'start' }}>
          <div style={{ display: 'grid', gap: 4 }}>
            <h3 style={{ margin: 0 }}>Top events</h3>
            <p style={muted}>Most common events sent by the SDK.</p>
          </div>
          {!data ? (
            <div style={{ display: 'grid', gap: 10 }}>
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} h={36} />
              ))}
            </div>
          ) : data.events.length === 0 ? (
            <div class="chart-empty" style={{ height: H + 28 }}>
              No events in this range.
            </div>
          ) : (
            <Table>
              <thead>
                <tr>
                  <th>Event</th>
                  <th style={{ textAlign: 'right' }}>Count</th>
                </tr>
              </thead>
              <tbody>
                {data.events.map((e) => (
                  <tr key={e.event}>
                    <td style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace' }}>{e.event}</td>
                    <td data-label="Count" style={{ textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>
                      {fmt(e.count)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card>
      </div>

      <section aria-label="In-app sources" style={{ display: 'grid', gap: 12 }}>
        <div style={{ display: 'grid', gap: 4 }}>
          <h3 style={{ margin: 0 }}>In-app sources</h3>
          <p style={muted}>Clicks from in-app browsers such as Messenger or Zalo, on all platforms.</p>
        </div>
        {!data ? (
          <Skeleton h={36} />
        ) : sources.length === 0 ? (
          <p style={muted}>No in-app clicks in this range.</p>
        ) : (
          <Table>
            <thead>
              <tr>
                <th>App</th>
                <th style={{ textAlign: 'right' }}>Clicks</th>
              </tr>
            </thead>
            <tbody>
              {sources.map((s) => (
                <tr key={s.source}>
                  <td>{SOURCE_NAMES[s.source] ?? s.source}</td>
                  <td data-label="Clicks" style={{ textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>
                    {fmt(s.count)}
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
        )}
      </section>

      <section aria-label="Retention by link" style={{ display: 'grid', gap: 12 }}>
        <div style={{ display: 'grid', gap: 4 }}>
          <h3 style={{ margin: 0 }}>Retention by link</h3>
          <p style={muted}>
            Devices your app tagged with a link, and the share that opened the app again exactly 1, 7 and 30 days later. All
            platforms.
          </p>
        </div>
        {!data ? (
          <Skeleton h={36} />
        ) : retention.length === 0 ? (
          <p style={muted}>No tagged devices in this range.</p>
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

      <section aria-label="Conversions by link" style={{ display: 'grid', gap: 12 }}>
        <div style={{ display: 'grid', gap: 4 }}>
          <h3 style={{ margin: 0 }}>Conversions by link</h3>
          <p style={muted}>Top events from devices your app tagged with a link. All platforms.</p>
        </div>
        {!data ? (
          <Skeleton h={36} />
        ) : conversions.length === 0 ? (
          <p style={muted}>No events from tagged devices in this range.</p>
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
    </div>
  );
}

// Retention cell: share of the mark's devices that came back; – when no cohort has reached that mark in range.
const rate = (m: Mark) => (m.devices === 0 ? '–' : `${Math.round((m.returned / m.devices) * 100)}%`);
const markTitle = (m: Mark) => (m.devices === 0 ? 'No cohort reached this day in range' : `${fmt(m.returned)} of ${fmt(m.devices)} devices`);

// Container width for the SVG, so axis text stays at real pixel size on any screen.
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

// Chart, or a skeleton while loading, or a message when every value in range is 0.
function ChartOr({ data, series, kind, empty }: { data: Analytics | null; series: Series; kind: 'line' | 'bar'; empty: string }) {
  if (!data) return <Skeleton h={H + 28} />;
  const total = series.reduce((n, s) => n + sum(data.days, s.key), 0);
  if (total === 0)
    return (
      <div class="chart-empty" style={{ height: H + 28 }}>
        {empty}
      </div>
    );
  return <Chart days={data.days} series={series} kind={kind} />;
}

const H = 200;
const PAD = { l: 32, r: 8, t: 8, b: 24 };

function Chart({
  days,
  series,
  kind,
}: {
  days: DayStat[];
  series: Series;
  kind: 'line' | 'bar';
}) {
  const [ref, w] = useWidth();
  const [hover, setHover] = useState<number | null>(null);
  const n = Math.max(days.length, 1);
  const plotW = w - PAD.l - PAD.r;
  const plotH = H - PAD.t - PAD.b;
  const peak = Math.max(
    1,
    ...days.map((d) =>
      kind === 'bar' ? series.reduce((s, x) => s + d[x.key], 0) : Math.max(...series.map((x) => d[x.key])),
    ),
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
            <line
              x1={PAD.l}
              x2={w - PAD.r}
              y1={y((top * g) / 4)}
              y2={y((top * g) / 4)}
              stroke="hsl(var(--k-border))"
            />
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
          ? [...series].reverse().map((s) => ( // first series drawn last, on top
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
        {/* hovered day: guide line + a dot per line series */}
        {hover !== null && (
          <g pointer-events="none">
            <line x1={x(hover)} x2={x(hover)} y1={PAD.t} y2={PAD.t + plotH} stroke="hsl(var(--k-input))" stroke-dasharray="3 3" />
            {kind === 'line' &&
              series.map((s) => (
                <circle key={s.key} cx={x(hover)} cy={y(days[hover][s.key])} r="3.5" fill="hsl(var(--k-card))" stroke={s.color} stroke-width="2" />
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
            // past the middle, flip left so the card stays inside the chart
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
