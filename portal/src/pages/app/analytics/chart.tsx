import { useEffect, useRef, useState } from 'preact/hooks';
import { Analytics, DayStat } from '../../../lib/types';
import { fmt } from '../../../lib/format';
import { row } from '../../../lib/styles';
import { useTz } from '../../../lib/tz';
import { Skeleton } from '../../../components/page';
import { Series } from './constants';
import { fmtDay, sum } from './helpers';
import { ChartEmpty } from './stat-cards';

type Kind = 'line' | 'bar';

const H = 200;
const PAD = { l: 32, r: 8, t: 8, b: 24 };
const GRID_LINES = 4;
const PX_PER_LABEL = 64;
const AXIS_TEXT = { 'font-size': 11, fill: 'hsl(var(--k-muted-foreground))' };

function useWidth() {
  const ref = useRef<HTMLDivElement>(null);
  const [w, setW] = useState(640);
  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === 'undefined') return;
    let raf = 0;
    const ro = new ResizeObserver(() => {
      cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => setW(el.clientWidth || 640));
    });
    ro.observe(el);
    return () => {
      cancelAnimationFrame(raf);
      ro.disconnect();
    };
  }, []);
  return [ref, w] as const;
}

export function ChartOr({ data, series, kind, empty }: { data: Analytics | null; series: Series; kind: Kind; empty: [string, string] }) {
  if (!data) return <Skeleton h={H + 28} />;
  const total = series.reduce((n, s) => n + sum(data.days, s.key), 0);
  if (total === 0) return <ChartEmpty h={H + 28} empty={empty} />;
  return <Chart days={data.days} series={series} kind={kind} />;
}

function Chart({ days, series, kind }: { days: DayStat[]; series: Series; kind: Kind }) {
  useTz();
  const [ref, w] = useWidth();
  const [hoverAt, setHover] = useState<number | null>(null);
  const hover = hoverAt !== null && hoverAt < days.length ? hoverAt : null;
  const n = Math.max(days.length, 1);
  const plotW = w - PAD.l - PAD.r;
  const plotH = H - PAD.t - PAD.b;
  const peak = Math.max(
    1,
    ...days.map((d) => (kind === 'bar' ? series.reduce((s, x) => s + d[x.key], 0) : Math.max(...series.map((x) => d[x.key])))),
  );
  const top = Math.ceil(peak / GRID_LINES) * GRID_LINES;
  const slot = plotW / n;
  const x = (i: number) => PAD.l + slot * i + slot / 2;
  const y = (v: number) => PAD.t + plotH - (v / top) * plotH;
  const every = Math.ceil(n / Math.max(2, Math.floor(plotW / PX_PER_LABEL)));

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
            <line x1={PAD.l} x2={w - PAD.r} y1={y((top * g) / GRID_LINES)} y2={y((top * g) / GRID_LINES)} stroke="hsl(var(--k-border))" />
            <text x={PAD.l - 6} y={y((top * g) / GRID_LINES) + 4} text-anchor="end" {...AXIS_TEXT}>
              {fmt((top * g) / GRID_LINES)}
            </text>
          </g>
        ))}
        {days.map((d, i) =>
          i % every === 0 ? (
            <text key={d.day} x={x(i)} y={H - 6} text-anchor="middle" {...AXIS_TEXT}>
              {fmtDay(d.day)}
            </text>
          ) : null,
        )}
        {kind === 'line'
          ? [...series].reverse().map((s) => (
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
      {hover !== null && <Tooltip day={days[hover]} series={series} at={x(hover)} width={w} />}
      <Legend series={series} />
    </div>
  );
}

function Tooltip({ day, series, at, width }: { day: DayStat; series: Series; at: number; width: number }) {
  return (
    <div
      class="chart-tip"
      role="status"
      style={{
        top: PAD.t,
        ...(at > width / 2 ? { right: width - at + 10 } : { left: at + 10 }),
      }}
    >
      <strong>{fmtDay(day.day)}</strong>
      {series.map((s) => (
        <span key={s.key} style={{ ...row, gap: 6, justifyContent: 'space-between' }}>
          <span style={{ ...row, gap: 6 }}>
            <span style={{ width: 8, height: 8, borderRadius: 2, background: s.color }} />
            {s.label}
          </span>
          <span style={{ fontVariantNumeric: 'tabular-nums' }}>{fmt(day[s.key])}</span>
        </span>
      ))}
    </div>
  );
}

function Legend({ series }: { series: Series }) {
  return (
    <div style={{ ...row, flexWrap: 'wrap', gap: 16, justifyContent: 'center' }}>
      {series.map((s) => (
        <span key={s.key} style={{ ...row, gap: 6, fontSize: 13 }}>
          <span style={{ width: 10, height: 10, borderRadius: 3, background: s.color }} />
          {s.label}
        </span>
      ))}
    </div>
  );
}
