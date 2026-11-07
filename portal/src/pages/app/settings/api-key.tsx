import { useState } from 'preact/hooks';
import { Alert, Table } from 'kinu';
import { api } from '../../../lib/api';
import { adminCall, AdminGateAborted } from '../../../lib/admin';
import { ConfirmDelete } from '../../../components/confirm-delete';
import { CopyButton } from '../../../components/copy-button';
import { mono, muted, row } from '../../../lib/styles';
import { App } from '../../../lib/types';
import { RotateKeyDialog } from './rotate-key';

const end = { ...row, justifyContent: 'flex-end' };

export function ApiKeyTable({ app, onReload }: { app: App; onReload: () => void }) {
  const [error, setError] = useState('');
  const revoke = async () => {
    setError('');
    try {
      await adminCall(() => api.revokeKey(app.id));
      onReload();
    } catch (e) {
      if (e instanceof AdminGateAborted) return;
      setError(String(e));
    }
  };

  return (
    <>
      {error && <Alert variant="destructive">{error}</Alert>}
      <Table>
        <thead>
          <tr>
            <th>Credential</th>
            <th>Value</th>
            <th />
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>App ID</td>
            <td style={mono}>{app.id}</td>
            <td>
              <div style={end}>
                <CopyButton value={app.id} label="Copy ID" />
              </div>
            </td>
          </tr>
          <tr>
            <td>API key</td>
            <td>
              {app.apiKeyHash ? (
                <span style={row}>
                  <span style={{ ...mono, letterSpacing: 2 }} aria-label="Hidden">
                    ••••••••••••
                  </span>
                  <span style={{ ...muted, fontSize: 13 }}>Shown once when created or rotated.</span>
                </span>
              ) : (
                <span style={{ ...muted, color: 'hsl(var(--k-destructive))' }}>Revoked. SDK calls are rejected.</span>
              )}
            </td>
            <td>
              <div style={end}>
                <RotateKeyDialog app={app} onChanged={onReload} />
                <ConfirmDelete
                  id={`dlg-revoke-${app.id}`}
                  requireAdmin
                  title={`Remove the API key for ${app.name}?`}
                  body="The SDK stops accepting it. Rotate a new key to get access back."
                  onConfirm={revoke}
                />
              </div>
            </td>
          </tr>
        </tbody>
      </Table>
    </>
  );
}
