import { JSX } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import {
  Alert,
  Badge,
  Button,
  Checkbox,
  Dialog,
  Empty,
  Field,
  Input,
  Label,
  Select,
  Table,
} from 'kinu';
import { api, App, HttpError, Webhook, WebhookEventType } from './api';
import {
  adminCall,
  AdminGateAborted,
  AdminRequired,
  closeDialog,
  ConfirmDelete,
  CopyButton,
  gateOpen,
  getAdmin,
  getAdminSet,
  Loading,
  mono,
  muted,
  openDialog,
  row,
  subscribeAdmin,
} from './ui';

const EVENT_OPTIONS: { type: WebhookEventType; desc: string }[] = [
  { type: 'installs', desc: 'Deferred installs attributed from links · permanent retention' },
  { type: 'events', desc: 'Custom SDK in-app analytics events · 24h retention' },
  { type: 'clicks', desc: 'Raw incoming link visits · high volume, 24h retention' },
];

const surface = {
  background: 'hsl(var(--k-card))',
  border: '1px solid hsl(var(--k-border))',
  borderRadius: 'var(--k-radius)',
};

const DEFAULT_TYPES: WebhookEventType[] = ['installs', 'events'];

// tick: the page's Refresh counter; bumping it reloads the list.
export function WebhooksPanel({ app, tick }: { app: App; tick: number }) {
  const [webhooks, setWebhooks] = useState<Webhook[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [adminState, setAdminState] = useState(() => ({
    adminSet: getAdminSet(),
    admin: getAdmin(),
  }));

  // Replay dialog state
  const [replayTarget, setReplayTarget] = useState<Webhook | null>(null);
  const [replayHours, setReplayHours] = useState('1');
  const [replaying, setReplaying] = useState(false);

  // Rotate secret dialog state
  const [rotateTarget, setRotateTarget] = useState<Webhook | null>(null);
  const [newSecret, setNewSecret] = useState('');
  const [rotating, setRotating] = useState(false);

  // New secret reveal dialog (after create)
  const [createdSecret, setCreatedSecret] = useState<{ id: string; url: string; secret: string } | null>(null);

  useEffect(() => {
    return subscribeAdmin(() => {
      setAdminState({ adminSet: getAdminSet(), admin: getAdmin() });
    });
  }, []);

  const fail = (e: unknown) => {
    if (!(e instanceof AdminGateAborted)) setError(String(e));
  };

  const reload = async () => {
    setError('');
    if (!getAdmin()) {
      // Server answers 403 until elevated: skip the request
      setWebhooks([]);
      setLoading(false);
      return;
    }
    try {
      const list = await api.listWebhooks(app.id);
      setWebhooks(list);
    } catch (e) {
      if (e instanceof HttpError && e.status === 403) {
        // Admin elevation required or ADMIN_PASSWORD unset
        setWebhooks([]);
      } else {
        setError(String(e));
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    reload();
  }, [app.id, adminState.admin, tick]);

  const toggleEnabled = async (wh: Webhook) => {
    setError('');
    try {
      const updated = await adminCall(() =>
        api.updateWebhook(wh.id, { enabled: !wh.enabled })
      );
      setWebhooks((prev) => (prev ? prev.map((w) => (w.id === wh.id ? updated : w)) : null));
    } catch (e) {
      fail(e);
    }
  };

  const handleReplay = async () => {
    if (!replayTarget) return;
    setReplaying(true);
    setError('');
    try {
      const hours = parseInt(replayHours, 10) || 1;
      const fromDate = new Date(Date.now() - hours * 3600 * 1000).toISOString();
      await adminCall(() => api.replayWebhook(replayTarget.id, fromDate));
      closeDialog('dlg-replay-webhook');
      setReplayTarget(null);
      await reload();
    } catch (e) {
      fail(e);
    } finally {
      setReplaying(false);
    }
  };

  const handleRotate = async () => {
    if (!rotateTarget) return;
    setRotating(true);
    setError('');
    try {
      const res = await adminCall(() => api.rotateWebhookSecret(rotateTarget.id));
      setNewSecret(res.secret);
      await reload();
    } catch (e) {
      fail(e);
    } finally {
      setRotating(false);
    }
  };

  const handleDelete = async (id: string) => {
    setError('');
    try {
      await adminCall(() => api.deleteWebhook(id));
      setWebhooks((prev) => (prev ? prev.filter((w) => w.id !== id) : null));
    } catch (e) {
      fail(e);
    }
  };

  if (loading) return <Loading />;

  return (
    <div>
      {error && (
        <Alert variant="destructive" style={{ marginBottom: 16 }}>
          {error}
        </Alert>
      )}

      {!adminState.adminSet ? (
        <Alert variant="destructive" style={{ marginBottom: 20 }}>
          Webhooks management is disabled because <strong>ADMIN_PASSWORD</strong> is not set in the server environment. Configure ADMIN_PASSWORD to enable webhook delivery and administration.
        </Alert>
      ) : !adminState.admin ? (
        <div style={{ marginBottom: 20 }}>
          <AdminRequired>
            Enter your admin password to view signing secrets, manage endpoints, or replay historical events.
          </AdminRequired>
        </div>
      ) : null}

      <div style={{ ...row, flexWrap: 'wrap', gap: 12, justifyContent: 'space-between', marginBottom: 16 }}>
        <div style={{ flex: '1 1 320px' }}>
          <h2 style={{ margin: 0, fontSize: 20 }}>Webhook endpoints</h2>
          <p style={{ ...muted, margin: '4px 0 0' }}>
            Signed POSTs of installs, events and clicks to your URLs.
          </p>
        </div>
        {adminState.adminSet && (
          <Button onClick={() => gateOpen('dlg-add-webhook')}>
            Add webhook
          </Button>
        )}
      </div>

      {webhooks && webhooks.length === 0 ? (
        <Empty style={{ padding: '48px 0' }}>
          <p style={muted}>No webhook endpoints configured for this app.</p>
          {adminState.adminSet && (
            <Button variant="outline" style={{ marginTop: 12 }} onClick={() => gateOpen('dlg-add-webhook')}>
              Create first webhook
            </Button>
          )}
        </Empty>
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
            {webhooks?.map((wh) => {
              const inBackoff = [wh.backoffInstalls, wh.backoffClicks, wh.backoffEvents].some(
                (b) => b && new Date(b).getTime() > Date.now()
              );

              return (
                <tr key={wh.id}>
                  <td style={{ verticalAlign: 'middle' }}>
                    {!wh.enabled ? (
                      <Badge variant="outline">Paused</Badge>
                    ) : inBackoff ? (
                      <Badge variant="destructive">Retrying</Badge>
                    ) : (
                      <Badge>Active</Badge>
                    )}
                  </td>
                  <td style={{ verticalAlign: 'middle' }}>
                    <div class="wh-url" title={wh.url}>{wh.url}</div>
                    <div style={{ ...muted, fontSize: 12, marginTop: 2 }}>
                      Created {new Date(wh.createdAt).toLocaleDateString('en', { month: 'short', day: 'numeric', year: 'numeric' })}
                    </div>
                  </td>
                  <td data-label="Events" style={{ verticalAlign: 'middle' }}>
                    <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                      {wh.types.map((t) => (
                        <Badge key={t} variant="outline" style={{ fontSize: 11 }}>
                          {t}
                        </Badge>
                      ))}
                    </div>
                  </td>
                  <td data-label="Secret" style={{ verticalAlign: 'middle' }}>
                    {adminState.admin ? (
                      <div style={{ ...row, gap: 8 }}>
                        <span style={{ ...mono, fontSize: 12 }}>
                          {wh.secret ? wh.secret.slice(0, 10) + '…' : '—'}
                        </span>
                        <CopyButton value={wh.secret} label="Copy" />
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => {
                            setRotateTarget(wh);
                            setNewSecret('');
                            openDialog('dlg-rotate-secret');
                          }}
                        >
                          Rotate
                        </Button>
                      </div>
                    ) : (
                      <span style={muted}>Locked</span>
                    )}
                  </td>
                  <td style={{ verticalAlign: 'middle', textAlign: 'right' }}>
                    <div style={{ ...row, justifyContent: 'flex-end', gap: 8 }}>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => toggleEnabled(wh)}
                      >
                        {wh.enabled ? 'Pause' : 'Resume'}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => {
                          setReplayTarget(wh);
                          openDialog('dlg-replay-webhook');
                        }}
                      >
                        Replay
                      </Button>
                      <ConfirmDelete
                        id={`dlg-del-wh-${wh.id}`}
                        title="Delete webhook"
                        body={`Delete endpoint ${wh.url}? Pending deliveries for this webhook will be discarded.`}
                        onConfirm={() => handleDelete(wh.id)}
                        requireAdmin
                      />
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </Table>
      )}

      {/* Add Webhook Dialog */}
      <AddWebhookDialog
        appId={app.id}
        onCreated={(wh) => {
          setWebhooks((prev) => (prev ? [...prev, wh] : [wh]));
          setCreatedSecret({ id: wh.id, url: wh.url, secret: wh.secret });
          openDialog('dlg-created-secret');
        }}
      />

      {/* Created Secret Dialog */}
      <Dialog id="dlg-created-secret">
        <Dialog.Content>
          <div style={{ display: 'grid', gap: 16 }}>
            <h2 style={{ margin: 0 }}>Webhook Created</h2>
            <p style={muted}>
              Your endpoint has been configured. Save this signing secret now to verify incoming <code>Detur-Signature</code> headers on your destination server.
            </p>
            {createdSecret && (
              <Field>
                <Label>Signing Secret</Label>
                <div style={{ ...row, gap: 8 }}>
                  <Input readOnly value={createdSecret.secret} style={mono} />
                  <CopyButton value={createdSecret.secret} label="Copy secret" />
                </div>
              </Field>
            )}
            <div style={{ ...row, justifyContent: 'flex-end' }}>
              <Button onClick={() => closeDialog('dlg-created-secret')}>Done</Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog>

      {/* Replay Webhook Dialog */}
      <Dialog id="dlg-replay-webhook">
        <Dialog.Content>
          <div style={{ display: 'grid', gap: 16 }}>
            <h2 style={{ margin: 0 }}>Replay Historical Events</h2>
            <p style={muted}>
              Rewind cursors for <strong>{replayTarget?.url}</strong> to re-deliver past events. Requests sent during replay include the <code>Detur-Replay: true</code> header.
            </p>
            <p style={{ ...muted, fontSize: 13, ...surface, padding: 10 }}>
              <strong>Note:</strong> Raw clicks and custom events older than 24 hours are purged by retention and cannot be replayed. Installs are preserved permanently.
            </p>
            <Field>
              <Label htmlFor="replay-preset">Replay from</Label>
              <Select
                id="replay-preset"
                value={replayHours}
                onChange={(e) => setReplayHours(e.currentTarget.value)}
              >
                <option value="1">1 hour ago</option>
                <option value="6">6 hours ago</option>
                <option value="12">12 hours ago</option>
                <option value="24">24 hours ago (retention window)</option>
              </Select>
            </Field>
            <div style={{ ...row, justifyContent: 'flex-end', gap: 8 }}>
              <Button variant="outline" onClick={() => closeDialog('dlg-replay-webhook')}>
                Cancel
              </Button>
              <Button disabled={replaying} onClick={handleReplay}>
                {replaying ? 'Rewinding…' : 'Start Replay'}
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog>

      {/* Rotate Secret Dialog */}
      <Dialog id="dlg-rotate-secret">
        <Dialog.Content>
          <div style={{ display: 'grid', gap: 16 }}>
            <h2 style={{ margin: 0 }}>Rotate Signing Secret</h2>
            <p style={muted}>
              Rotating the secret invalidates the old secret immediately. Existing signature verification on your receiver will fail until updated with the new secret.
            </p>
            {newSecret ? (
              <Field>
                <Label>New Signing Secret</Label>
                <div style={{ ...row, gap: 8 }}>
                  <Input readOnly value={newSecret} style={mono} />
                  <CopyButton value={newSecret} label="Copy secret" />
                </div>
              </Field>
            ) : null}
            <div style={{ ...row, justifyContent: 'flex-end', gap: 8 }}>
              <Button
                variant="outline"
                onClick={() => {
                  closeDialog('dlg-rotate-secret');
                  setRotateTarget(null);
                  setNewSecret('');
                }}
              >
                {newSecret ? 'Close' : 'Cancel'}
              </Button>
              {!newSecret && (
                <Button variant="destructive" disabled={rotating} onClick={handleRotate}>
                  {rotating ? 'Rotating…' : 'Rotate Secret Now'}
                </Button>
              )}
            </div>
          </div>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}

function AddWebhookDialog({
  appId,
  onCreated,
}: {
  appId: string;
  onCreated: (wh: Webhook) => void;
}) {
  const [url, setUrl] = useState('');
  const [types, setTypes] = useState<WebhookEventType[]>(DEFAULT_TYPES);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  const toggleType = (t: WebhookEventType) => {
    setTypes((prev) =>
      prev.includes(t) ? prev.filter((x) => x !== t) : [...prev, t]
    );
  };

  const submit = async (e: JSX.TargetedEvent<HTMLFormElement>) => {
    e.preventDefault();
    setErr('');
    if (!url.trim()) {
      setErr('Destination URL is required');
      return;
    }
    if (types.length === 0) {
      setErr('Select at least one event type');
      return;
    }
    setBusy(true);
    try {
      const created = await adminCall(() =>
        api.createWebhook(appId, { url: url.trim(), types })
      );
      closeDialog('dlg-add-webhook');
      setUrl('');
      setTypes(DEFAULT_TYPES);
      onCreated(created);
    } catch (e) {
      if (!(e instanceof AdminGateAborted)) {
        setErr(String(e));
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog id="dlg-add-webhook">
      <Dialog.Content>
        <form style={{ display: 'grid', gap: 16 }} onSubmit={submit}>
          <div style={{ display: 'grid', gap: 4 }}>
            <h2 style={{ margin: 0 }}>Add Webhook Endpoint</h2>
            <p style={muted}>Configure an HTTPS URL to receive real-time batched app activity.</p>
          </div>
          <Field>
            <Label htmlFor="wh-url">Destination URL</Label>
            <Input
              id="wh-url"
              placeholder="https://api.yourdomain.com/webhooks/detur"
              value={url}
              onInput={(e) => setUrl(e.currentTarget.value)}
            />
            <p style={{ ...muted, fontSize: 12, margin: '4px 0 0' }}>
              Must use HTTPS (HTTP permitted for localhost development only). Private/internal IP destinations are blocked.
            </p>
          </Field>

          <Field>
            <Label>Subscribed Events</Label>
            <div style={{ display: 'grid', gap: 12, marginTop: 4 }}>
              {EVENT_OPTIONS.map((o) => (
                <label key={o.type} class="check-option">
                  <Checkbox checked={types.includes(o.type)} onChange={() => toggleType(o.type)} />
                  <span>
                    <span class="check-option-title">{o.type}</span>
                    <span class="check-option-desc">{o.desc}</span>
                  </span>
                </label>
              ))}
            </div>
          </Field>

          {err && <Alert variant="destructive">{err}</Alert>}

          <div style={{ ...row, justifyContent: 'flex-end', gap: 8 }}>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                closeDialog('dlg-add-webhook');
                setErr('');
              }}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={busy}>
              {busy ? 'Creating…' : 'Create Webhook'}
            </Button>
          </div>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}
