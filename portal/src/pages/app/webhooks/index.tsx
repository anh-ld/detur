import { useEffect, useState } from 'preact/hooks';
import { Alert, Button, Card, Empty, Table } from 'kinu';
import { api } from '../../../lib/api';
import { App, Webhook } from '../../../lib/types';
import { adminCall, gateOpen, useAdmin, useGatedLoad } from '../../../lib/admin';
import { AdminRequired } from '../../../components/admin';
import { openDialog } from '../../../lib/dialog';
import { Loading } from '../../../components/page';
import { actionErrors } from '../../../components/action-errors';
import { AddWebhookDialog } from './add-webhook-dialog';
import { CreatedSecretDialog } from './created-secret-dialog';
import { ReplayDialog } from './replay-dialog';
import { RotateDialog } from './rotate-dialog';
import { WebhookRow } from './webhook-row';

// Header trigger: opens the dialog WebhooksPanel renders on the same tab.
export function AddWebhookButton() {
  const { adminSet } = useAdmin();
  if (!adminSet) return null;
  return <Button onClick={() => gateOpen('dlg-add-webhook')}>Add webhook</Button>;
}

// tick: page Refresh counter; bump reloads list
export function WebhooksPanel({ app, tick }: { app: App; tick: number }) {
  const [actionError, setError] = useState('');
  const errors = actionErrors(setError);
  const admin = useAdmin();
  const {
    data: webhooks,
    setData: setWebhooks,
    loading,
    locked,
    error: loadError,
    reload,
  } = useGatedLoad<Webhook[]>({
    load: () => api.listWebhooks(app.id),
    deps: [app.id, tick],
    mode: 'needs-password',
    empty: [],
  });
  const error = actionError || loadError;
  // stale action errors don't outlive Refresh or an app switch
  useEffect(() => setError(''), [app.id, tick]);

  const [replayTarget, setReplayTarget] = useState<Webhook | null>(null);
  const [rotateOpen, setRotateOpen] = useState<{ wh: Webhook } | null>(null);
  const [createdSecret, setCreatedSecret] = useState<string | null>(null);

  const toggleEnabled = async (wh: Webhook) => {
    errors.clear();
    try {
      const updated = await adminCall(() => api.updateWebhook(wh.id, { enabled: !wh.enabled }));
      setWebhooks((prev) => prev && prev.map((w) => (w.id === wh.id ? updated : w)));
    } catch (e) {
      errors.fail(e);
    }
  };

  const remove = async (id: string) => {
    errors.clear();
    try {
      await adminCall(() => api.deleteWebhook(id));
      setWebhooks((prev) => prev && prev.filter((w) => w.id !== id));
    } catch (e) {
      errors.fail(e);
    }
  };

  return (
    <div>
      {/* mounted while loading too: the header's Add button opens it */}
      <AddWebhookDialog
        appId={app.id}
        onCreated={(wh) => {
          setWebhooks((prev) => [...(prev ?? []), wh]);
          setCreatedSecret(wh.secret);
          openDialog('dlg-created-secret');
        }}
      />
      {loading ? (
        <Loading />
      ) : (
        <>
          {error && (
            <Alert variant="destructive" style={{ marginBottom: 16 }}>
              {error}
            </Alert>
          )}

          {!admin.adminSet ? (
            <Alert style={{ marginBottom: 20 }}>
              Webhooks need an admin password. Set <code>ADMIN_PASSWORD</code> on the server, then restart to add endpoints.
            </Alert>
          ) : !admin.admin ? (
            <div style={{ marginBottom: 20 }}>
              <AdminRequired>Enter your admin password to view signing secrets, manage endpoints, or replay historical events.</AdminRequired>
            </div>
          ) : null}

          {!admin.adminSet || !admin.admin || locked ? null : webhooks && webhooks.length === 0 ? (
            <Card>
              <Empty>
                <h3>No webhooks yet</h3>
                Add one to get installs, events and clicks pushed to your server.
              </Empty>
            </Card>
          ) : (
            <Table>
              <thead>
                <tr>
                  <th>Status</th>
                  <th>URL</th>
                  <th>Events</th>
                  <th>Secret</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {webhooks?.map((wh) => (
                  <WebhookRow
                    key={wh.id}
                    wh={wh}
                    admin={admin.admin}
                    onToggle={() => toggleEnabled(wh)}
                    onReplay={() => {
                      setReplayTarget(wh);
                      openDialog('dlg-replay-webhook');
                    }}
                    onRotate={() => {
                      setRotateOpen({ wh });
                      openDialog('dlg-rotate-secret');
                    }}
                    onDelete={() => remove(wh.id)}
                  />
                ))}
              </tbody>
            </Table>
          )}

          <CreatedSecretDialog secret={createdSecret} />
          <ReplayDialog target={replayTarget} errors={errors} onReplayed={reload} />
          <RotateDialog open={rotateOpen} errors={errors} onRotated={reload} />
        </>
      )}
    </div>
  );
}
