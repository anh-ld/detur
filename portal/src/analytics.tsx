import { ComponentChildren } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { Badge, Card, Empty, Select, Table } from 'kinu';
import { Analytics, DayStat, Platform } from './api';
import { muted, row } from './ui';

// Analytics view (Detour overview parity): filters, totals, daily charts, top events. Charts are plain SVG.

type Metric = keyof Omit<DayStat, 'day'>;

const LINES: { key: Metric; label: string; color: string }[] = [
  { key: 'clicks', label: 'Clicks', color: 'hsl(217 91% 52%)' },
  { key: 'nonOrganic', label: 'Installs via link', color: 'hsl(152 60% 40%)' },
  { key: 'webFallbacks', label: 'Web fallbacks', color: 'hsl(265 70% 60%)' },
  { key: 'opens', label: 'Already installed opens', color: 'hsl(25 90% 52%)' },
];

const BARS: { key: Metric; label: string; color: string }[] = [
  { key: 'nonOrganic', label: 'Non-organic', color: 'hsl(217 91% 52%)' },
  { key: 'organic', label: 'Organic', color: 'hsl(214 60% 80%)' },
];

const fmtDay = (d: string) =>
  new Date(d + 'T00:00:00Z').toLocaleDateString('en', { month: 'short', day: 'numeric', timeZone: 'UTC' });

const sum = (days: DayStat[], k: Metric) => days.reduce((n, d) => n + d[k], 0);

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

export function AnalyticsView({ data }: { data: Analytics | null }) {
  const days = data?.days ?? [];
  const clicks = sum(days, 'clicks');
  const viaLink = sum(days, 'nonOrganic');
  const tile = (label: string, n: number | undefined, extra?: ComponentChildren) => (
    <Card padding="sm" style={{ display: 'grid', gap: 4 }}>
      <p style={muted}>{label}</p>
      <div style={{ ...row, fontSize: 28, fontWeight: 600 }}>
        {n ?? '–'}
        {extra}
      </div>
    </Card>
  );

  return (
    <div style={{ display: 'grid', gap: 16 }}>
      <div className="stats" style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(0, 1fr))', gap: 16 }}>
        {tile('Clicks', data ? clicks : undefined)}
        {tile('Already installed opens', data ? sum(days, 'opens') : undefined)}
        {tile(
          'Installs via link',
          data ? viaLink : undefined,
          data && clicks > 0 && (
            <Badge variant="secondary" style={{ letterSpacing: 'normal', whiteSpace: 'nowrap' }}>
              {Math.round((viaLink / clicks) * 100)}% match
            </Badge>
          ),
        )}
        {tile('Web fallbacks', data ? sum(days, 'webFallbacks') : undefined)}
      </div>

      <Card style={{ display: 'grid', gap: 12 }}>
        <h3 style={{ margin: 0 }}>Activity</h3>
        <Chart days={days} series={LINES} kind="line" />
      </Card>

      <div className="split" style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 16 }}>
        <Card style={{ display: 'grid', gap: 12, alignContent: 'start' }}>
          <div style={{ display: 'grid', gap: 4 }}>
            <h3 style={{ margin: 0 }}>Organic vs. non-organic</h3>
            <p style={muted}>Installs matched to a link vs. installs with no click.</p>
          </div>
          <Chart days={days} series={BARS} kind="bar" />
        </Card>
        <Card style={{ display: 'grid', gap: 12, alignContent: 'start' }}>
          <div style={{ display: 'grid', gap: 4 }}>
            <h3 style={{ margin: 0 }}>Top events</h3>
            <p style={muted}>Most common events sent by the SDK.</p>
          </div>
          {data && data.events.length === 0 ? (
            <Empty>No events in this range.</Empty>
          ) : (
            <Table>
              <thead>
                <tr>
                  <th>Event</th>
                  <th style={{ textAlign: 'right' }}>Count</th>
                </tr>
              </thead>
              <tbody>
                {data?.events.map((e) => (
                  <tr key={e.event}>
                    <td style={{ fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace' }}>{e.event}</td>
                    <td data-label="Count" style={{ textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>
                      {e.count}
                    </td>
                  </tr>
                ))}
              </tbody>
            </Table>
          )}
        </Card>
      </div>
    </div>
  );
}

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

const H = 200;
const PAD = { l: 32, r: 8, t: 8, b: 24 };

function Chart({
  days,
  series,
  kind,
}: {
  days: DayStat[];
  series: { key: Metric; label: string; color: string }[];
  kind: 'line' | 'bar';
}) {
  const [ref, w] = useWidth();
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
    <div ref={ref} style={{ display: 'grid', gap: 8, minWidth: 0, overflow: 'hidden' }}>
      <svg width={w} height={H} style={{ display: 'block' }} role="img" aria-label={series.map((s) => s.label).join(', ') + ' per day'}>
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
              {(top * g) / 4}
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
          ? series.map((s) => (
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
        {/* hover target per day: native tooltip with that day's numbers */}
        {days.map((d, i) => (
          <rect key={'h' + d.day} class="chart-hover" x={PAD.l + slot * i} y={PAD.t} width={slot} height={plotH}>
            <title>{[fmtDay(d.day), ...series.map((s) => `${s.label}: ${d[s.key]}`)].join('\n')}</title>
          </rect>
        ))}
      </svg>
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
