import { Badge, Button, Table } from 'kinu';
import { ApiToken, App } from '../../lib/types';
import { fmtDate } from '../../lib/format';
import { AdminButton } from '../../components/admin';
import { openDialog } from '../../lib/dialog';
import { REVOKE_TOKEN_DIALOG } from './revoke-token-dialog';
import { EDIT_TOKEN_DIALOG } from './edit-token-dialog';
import { mono, muted, row, end } from '../../lib/styles';

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
  const appName = (id: string) => apps.find((a) => a.id === id)?.name ?? id;
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
            <td data-label="Apps">
              <div style={{ ...row, flexWrap: 'wrap' }}>
                {t.appIds.map((id) => (
                  <Badge key={id} variant="secondary">
                    {appName(id)}
                  </Badge>
                ))}
              </div>
            </td>
            <td>{fmtDate(t.createdAt)}</td>
            <td style={t.lastUsedAt ? undefined : muted}>{t.lastUsedAt ? fmtDate(t.lastUsedAt) : 'Never'}</td>
            <td style={{ textAlign: 'right' }}>
              <div style={end}>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    onEdit(t);
                    openDialog(EDIT_TOKEN_DIALOG);
                  }}
                >
                  Edit
                </Button>
                <AdminButton
                  size="sm"
                  variant="destructive"
                  onClick={() => {
                    onRevoke(t);
                    openDialog(REVOKE_TOKEN_DIALOG);
                  }}
                >
                  Revoke
                </AdminButton>
              </div>
            </td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}
