import { useState } from 'preact/hooks';
import { Button, Dialog, Field, Label, Select } from 'kinu';
import { api } from '../../../lib/api';
import { Webhook } from '../../../lib/types';
import { adminCall } from '../../../lib/admin';
import { closeDialog } from '../../../lib/dialog';
import { muted, stack, end } from '../../../lib/styles';
import { ActionErrors } from '../../../components/action-errors';

const note = {
  ...muted,
  fontSize: 13,
  padding: 10,
  background: 'hsl(var(--k-card))',
  border: '1px solid hsl(var(--k-border))',
  borderRadius: 'var(--k-radius)',
};

const PRESETS: [hours: number, label: string][] = [
  [1, '1 hour ago'],
  [6, '6 hours ago'],
  [12, '12 hours ago'],
  [24, '24 hours ago'],
  [72, '3 days ago'],
  [168, '7 days ago'],
  [720, '30 days ago'],
];

export const REPLAY_DIALOG = 'dlg-replay-webhook';

export function ReplayDialog({ target, errors, onReplayed }: { target: Webhook | null; errors: ActionErrors; onReplayed: () => void }) {
  const [hours, setHours] = useState('1');
  const [busy, setBusy] = useState(false);

  const replay = async () => {
    if (!target) return;
    setBusy(true);
    errors.clear();
    try {
      const from = new Date(Date.now() - (parseInt(hours, 10) || 1) * 3600 * 1000).toISOString();
      await adminCall(() => api.replayWebhook(target.id, from));
      closeDialog(REPLAY_DIALOG);
      onReplayed();
    } catch (e) {
      errors.fail(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog id={REPLAY_DIALOG}>
      <Dialog.Content>
        <div style={stack}>
          <h2 style={{ margin: 0 }}>Replay Historical Events</h2>
          <p style={muted}>
            Rewind cursors for <strong>{target?.url}</strong> to re-deliver past events. Requests sent during replay include the{' '}
            <code>Detur-Replay: true</code> header.
          </p>
          <p style={note}>
            <strong>Note:</strong> Raw clicks and custom events older than <code>RETENTION_HOURS</code> (default 24h) are purged and cannot be
            replayed. Installs are kept permanently.
          </p>
          <Field>
            <Label htmlFor="replay-preset">Replay from</Label>
            <Select id="replay-preset" value={hours} onChange={(e) => setHours(e.currentTarget.value)}>
              {PRESETS.map(([h, label]) => (
                <option key={h} value={h}>
                  {label}
                </option>
              ))}
            </Select>
          </Field>
          <div style={end}>
            <Button variant="outline" onClick={() => closeDialog(REPLAY_DIALOG)}>
              Cancel
            </Button>
            <Button disabled={busy} onClick={replay}>
              {busy ? 'Rewinding…' : 'Start Replay'}
            </Button>
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
