import { useEffect, useState } from 'preact/hooks';
import { Table } from 'kinu';
import { api } from '../../../lib/api';
import { Loading } from '../../../components/page';
import { muted } from '../../../lib/styles';
import { App, HealthCheck } from '../../../lib/types';

const STATUS = { ok: 'OK', warn: 'Check', fail: 'Fix' };

export function HealthTable({ app }: { app: App }) {
  const [checks, setChecks] = useState<HealthCheck[] | null>(null);
  useEffect(() => {
    api.getHealth(app.id).then(setChecks, () => setChecks([]));
  }, [app]);
  if (!checks) return <Loading />;
  return (
    <Table>
      <thead>
        <tr>
          <th>Check</th>
          <th>Status</th>
          <th>Detail</th>
        </tr>
      </thead>
      <tbody>
        {checks.map((c) => (
          <tr key={c.check}>
            <td>{c.check}</td>
            <td data-label="Status">
              <span class={`health ${c.status}`}>{STATUS[c.status]}</span>
            </td>
            <td style={muted}>{c.detail}</td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}
