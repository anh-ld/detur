import { Button, Table } from 'kinu';
import { ApiToken, App } from '../../lib/types';
import { gateOpen } from '../../lib/admin';
import { openDialog } from '../../lib/dialog';
import { mono, row } from '../../lib/styles';

const fmtDate = (iso: string) => new Date(iso).toLocaleDateString('en', { month: 'short', day: 'numeric', year: 'numeric' });

export function TokenTable({
  tokens,
  apps,
  onEdit,
  onRevoke,
}: {
  tokens: ApiToken[];
  apps: App[];
  onEdit: (t: ApiToken) => void;
  onRevoke: (t: ApiToken) => void;
}) {
  const appNames = (ids: string[]) => ids.map((id) => apps.find((a) => a.id === id)?.name ?? id).join(', ');
  return (
    <Table>
      <thead>
        <tr>
          <th>Name</th>
          <th>Token</th>
          <th>Apps</th>
          <th>Created</th>
          <th>Last used</th>
          <th />
        </tr>
      </thead>
      <tbody>
        {tokens.map((t) => (
          <tr key={t.id}>
            <td>{t.name}</td>
            <td style={mono}>{t.prefix}…</td>
            <td>{appNames(t.appIds)}</td>
            <td>{fmtDate(t.createdAt)}</td>
            <td>{t.lastUsedAt ? fmtDate(t.lastUsedAt) : 'Never'}</td>
            <td style={{ textAlign: 'right' }}>
              <div style={{ ...row, justifyContent: 'flex-end' }}>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    onEdit(t);
                    openDialog('dlg-edit-token');
                  }}
                >
                  Edit
                </Button>
                <Button
                  size="sm"
                  variant="destructive"
                  onClick={() => {
                    onRevoke(t);
                    gateOpen('dlg-revoke-token');
                  }}
                >
                  Revoke
                </Button>
              </div>
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}
