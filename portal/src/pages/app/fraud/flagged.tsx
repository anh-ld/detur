import { useEffect, useState } from 'preact/hooks';
import { Alert, Badge, Card, Empty, Table } from 'kinu';
import { api } from '../../../lib/api';
import { DateRangeFilter } from '../../../lib/range';
import { Fraud } from '../../../lib/types';
import { SIGNALS } from '../../../lib/matching';
import { fmt } from '../../../lib/format';
import { mono, muted, row, stack } from '../../../lib/styles';
import { useLatest } from '../../../lib/use-latest';
import { DateRangePicker } from '../../../components/date-range-picker';
import { Skeleton } from '../../../components/page';
import { Outcome } from './outcome';

const signalLabel = (k: string) => SIGNALS.find((s) => s.key === k)?.label ?? k;

const fmtTime = (t: string) => new Date(t).toLocaleString('en', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });

export function Flagged({ appId, tick }: { appId: string; tick: number }) {
  const [range, setRange] = useState<DateRangeFilter>({ days: 7 });
  const [data, setData] = useState<Fraud | null>(null);
  const [error, setError] = useState('');
  const latest = useLatest();
  useEffect(() => {
    const live = latest.begin();
    setError('');
    api.getFraud(appId, range).then(
      (f) => live() && setData(f),
      (e) => live() && setError(String(e)),
    );
    return latest.cancel;
  }, [appId, range.days, range.window, range.from, range.to, tick]);

  return (
    <section aria-label="Flagged installs" style={stack}>
      <div style={{ ...row, justifyContent: 'space-between', flexWrap: 'wrap', gap: 12 }}>
        <div style={{ display: 'grid', gap: 4 }}>
          <h2 style={{ margin: 0 }}>Flagged installs</h2>
          <p style={muted}>Installs at least one signal flagged, newest first.</p>
        </div>
        <DateRangePicker ariaLabel="Flagged range" range={range} allowWindows={false} onRange={(r) => setRange(r.window ? { days: 7 } : r)} />
      </div>
      {error ? (
        <Alert variant="destructive">{error}</Alert>
      ) : !data ? (
        <Skeleton h={120} />
      ) : data.installs.length === 0 ? (
        <Card>
          <Empty>
            <h3>All clear</h3>
            Nothing flagged in this range.
          </Empty>
        </Card>
      ) : (
        <FlaggedTable data={data} />
      )}
    </section>
  );
}

function FlaggedTable({ data }: { data: Fraud }) {
  return (
    <>
      <dl class="signal-counts" aria-label="Flagged installs per signal">
        {SIGNALS.map((s) => (
          <div key={s.key}>
            <dt>{s.label}</dt>
            <dd>{fmt(data.signals[s.key] ?? 0)}</dd>
          </div>
        ))}
      </dl>
      <Table>
        <thead>
          <tr>
            <th>When</th>
            <th>Link</th>
            <th>Signals</th>
            <th>Outcome</th>
          </tr>
        </thead>
        <tbody>
          {data.installs.map((i) => (
            <tr key={i.id}>
              <td style={{ whiteSpace: 'nowrap' }}>{fmtTime(i.createdAt)}</td>
              <td data-label="Link" style={mono}>
                {i.linkKey || '—'}
              </td>
              <td data-label="Signals">
                <div style={{ ...row, flexWrap: 'wrap', gap: 4 }}>
                  {i.fraud.map((f) => (
                    <Badge key={f} variant="outline" style={{ letterSpacing: 'normal' }}>
                      {signalLabel(f)}
                    </Badge>
                  ))}
                </div>
              </td>
              <td data-label="Outcome">
                <Outcome i={i} />
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
      {data.installs.length >= 100 && <p style={muted}>Showing the latest 100 flagged installs.</p>}
    </>
  );
}
