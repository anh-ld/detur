import { JSX } from 'preact';
import { useState } from 'preact/hooks';
import { Alert, Button, Checkbox, Dialog, Field, Input, Label } from 'kinu';
import { api } from '../../../lib/api';
import { Webhook, WebhookEventType } from '../../../lib/types';
import { adminCall, AdminGateAborted } from '../../../lib/admin';
import { closeDialog } from '../../../lib/dialog';
import { muted, row } from '../../../lib/styles';

const EVENT_OPTIONS: { type: WebhookEventType; desc: string }[] = [
  { type: 'installs', desc: 'Deferred installs attributed from links · permanent retention' },
  { type: 'events', desc: 'Custom SDK in-app analytics events · kept RETENTION_HOURS (default 24h)' },
  { type: 'clicks', desc: 'Raw incoming link visits · high volume, kept RETENTION_HOURS (default 24h)' },
];

const DEFAULT_TYPES: WebhookEventType[] = ['installs', 'events'];

export function AddWebhookDialog({ appId, onCreated }: { appId: string; onCreated: (wh: Webhook) => void }) {
  const [url, setUrl] = useState('');
  const [types, setTypes] = useState<WebhookEventType[]>(DEFAULT_TYPES);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  const toggleType = (t: WebhookEventType) =>
    setTypes((prev) => (prev.includes(t) ? prev.filter((x) => x !== t) : [...prev, t]));

  const submit = async (e: JSX.TargetedEvent<HTMLFormElement>) => {
    e.preventDefault();
    setErr('');
    if (!url.trim()) return setErr('Destination URL is required');
    if (types.length === 0) return setErr('Select at least one event type');
    setBusy(true);
    try {
      const created = await adminCall(() => api.createWebhook(appId, { url: url.trim(), types }));
      closeDialog('dlg-add-webhook');
      setUrl('');
      setTypes(DEFAULT_TYPES);
      onCreated(created);
    } catch (e) {
      if (!(e instanceof AdminGateAborted)) setErr(String(e));
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
          {err && <Alert variant="destructive">{err}</Alert>}
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
                <Label key={o.type} class="check-option">
                  <Checkbox checked={types.includes(o.type)} onChange={() => toggleType(o.type)} />
                  <span>
                    <span class="check-option-title">{o.type}</span>
                    <span class="check-option-desc">{o.desc}</span>
                  </span>
                </Label>
              ))}
            </div>
          </Field>

          <div style={{ ...row, justifyContent: 'flex-end' }}>
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
