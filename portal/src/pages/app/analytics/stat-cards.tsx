import { ComponentChildren } from 'preact';
import { Card } from 'kinu';
import { Analytics } from '../../../lib/types';
import { fmt } from '../../../lib/format';
import { mono, muted } from '../../../lib/styles';
import { Skeleton } from '../../../components/page';
import { LINES, METRIC_ICONS, Metric } from './constants';
import { sum } from './helpers';

export function Stat({ data, metric, hint, extra }: { data: Analytics | null; metric: Metric; hint: string; extra?: ComponentChildren }) {
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
            <path d={METRIC_ICONS[metric]} />
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

export function ChartCard({
  title,
  sub,
  total,
  unit,
  loading,
  children,
}: {
  title: string;
  sub: string;
  total?: number;
  unit?: string;
  loading?: boolean;
  children: ComponentChildren;
}) {
  return (
    <Card class="chart-card">
      <div class="chart-head">
        <div>
          <h3>{title}</h3>
          <p style={muted}>{sub}</p>
        </div>
        {unit && (
          <div class="chart-total">
            {loading ? <Skeleton w={72} h={36} /> : <strong>{fmt(total!)}</strong>}
            <span style={muted}>{unit}</span>
          </div>
        )}
      </div>
      {children}
    </Card>
  );
}

export function ChartEmpty({ h, empty }: { h: number; empty: [string, string] }) {
  return (
    <div class="chart-empty" style={{ height: h }}>
      <strong>{empty[0]}</strong>
      {empty[1]}
    </div>
  );
}

export function BarList({
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
  if (items.length === 0) return <ChartEmpty h={160} empty={empty} />;
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
