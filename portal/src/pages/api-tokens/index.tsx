import { useEffect, useState } from 'preact/hooks';
import { Alert, Card, Empty } from 'kinu';
import { api } from '../../lib/api';
import { ApiToken, App, CreatedToken } from '../../lib/types';
import { adminCall, useAdmin, useGatedLoad } from '../../lib/admin';
import { AdminButton, AdminRequired } from '../../components/admin';
import { actionErrors } from '../../components/action-errors';
import { openDialog } from '../../lib/dialog';
import { Loading, PageHeader } from '../../components/page';
import { Connect } from './connect';
import { CreatedTokenDialog } from './created-token-dialog';
import { EditTokenDialog } from './edit-token-dialog';
import { NEW_TOKEN_DIALOG, NewTokenDialog } from './new-token-form';
import { RevokeTokenDialog } from './revoke-token-dialog';
import { TokenTable } from './token-table';

// Admin-managed data-API / MCP tokens. Plaintext token lives in state only while its dialog is open.
export function ApiTokensPage() {
  const [apps, setApps] = useState<App[]>([]);
  const [linkBase, setLinkBase] = useState('');
  const [actionError, setError] = useState('');
  const errors = actionErrors(setError);
  const admin = useAdmin();
  const {
    data: tokens,
    setData: setTokens,
    loading,
    locked,
    error: loadError,
  } = useGatedLoad<ApiToken[]>({
    load: () => api.listTokens(),
    deps: [],
    mode: 'needs-password',
    empty: [],
  });
  const error = actionError || loadError;

  const [created, setCreated] = useState<CreatedToken | null>(null);
  const [editing, setEditing] = useState<ApiToken | null>(null);
  const [revoking, setRevoking] = useState<ApiToken | null>(null);

  useEffect(() => {
    api.listApps().then(setApps, errors.fail);
    api.getConfig().then(
      (c) => setLinkBase(c.linkBase),
      () => {},
    );
  }, []);

  const revoke = async ({ id }: ApiToken) => {
    try {
      await adminCall(() => api.deleteToken(id));
      setTokens((prev) => prev && prev.filter((t) => t.id !== id));
    } catch (e) {
      errors.fail(e);
    }
  };

  if (loading) return <Loading />;

  const base = linkBase || location.origin;

  return (
    <div>
      <PageHeader
        title="API"
        description="Tokens let agents and scripts read analytics, read-only, for the apps you pick."
        actions={admin.adminSet && <AdminButton onClick={() => openDialog(NEW_TOKEN_DIALOG)}>New token</AdminButton>}
      />

      {error && (
        <Alert variant="destructive" style={{ marginBottom: 16 }}>
          {error}
        </Alert>
      )}

      {!admin.adminSet ? (
        <Alert>
          API tokens need an admin password. Set <code>ADMIN_PASSWORD</code> on the server, then restart to create tokens.
        </Alert>
      ) : (
        <div style={{ display: 'grid', gap: 24 }}>
          {!admin.admin && <AdminRequired>Enter your admin password to view, create, edit or revoke API tokens.</AdminRequired>}

          {admin.admin &&
            !locked &&
            (tokens && tokens.length === 0 ? (
              <Card>
                <Empty>
                  <h3>No tokens yet</h3>
                  Use New token to connect an agent.
                </Empty>
              </Card>
            ) : (
              <TokenTable tokens={tokens ?? []} apps={apps} onEdit={setEditing} onRevoke={setRevoking} />
            ))}

          {admin.admin && <Connect base={base} />}
        </div>
      )}

      {admin.admin && (
        <NewTokenDialog
          apps={apps}
          errors={errors}
          onCreated={(t) => {
            const { token: _plain, ...rest } = t;
            setTokens((prev) => [...(prev ?? []), rest]);
            setCreated(t);
            openDialog('dlg-created-token');
          }}
        />
      )}
      <CreatedTokenDialog created={created} base={base} onDone={() => setCreated(null)} />
      <EditTokenDialog
        token={editing}
        apps={apps}
        errors={errors}
        onSaved={(t) => setTokens((prev) => prev && prev.map((x) => (x.id === t.id ? t : x)))}
      />
      <RevokeTokenDialog token={revoking} onRevoke={revoke} />
    </div>
  );
}
