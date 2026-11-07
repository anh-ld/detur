import { useEffect, useState } from 'preact/hooks';
import { Button, Dialog } from 'kinu';
import { api } from '../../../lib/api';
import { Webhook } from '../../../lib/types';
import { adminCall } from '../../../lib/admin';
import { SecretField } from '../../../components/secret-field';
import { closeDialog } from '../../../lib/dialog';
import { muted, row } from '../../../lib/styles';
import { ActionErrors } from '../../../components/action-errors';

// `open` is a fresh object per click so reopening the same webhook clears the previous secret
export function RotateDialog({ open, errors, onRotated }: { open: { wh: Webhook } | null; errors: ActionErrors; onRotated: () => void }) {
  const [secret, setSecret] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => setSecret(''), [open]);

  const rotate = async () => {
    if (!open) return;
    setBusy(true);
    errors.clear();
    try {
      setSecret((await adminCall(() => api.rotateWebhookSecret(open.wh.id))).secret);
      onRotated();
    } catch (e) {
      errors.fail(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog id="dlg-rotate-secret">
      <Dialog.Content>
        <div style={{ display: 'grid', gap: 16 }}>
          <h2 style={{ margin: 0 }}>Rotate Signing Secret</h2>
          <p style={muted}>
            Rotating the secret invalidates the old secret immediately. Existing signature verification on your receiver will fail until
            updated with the new secret.
          </p>
          {secret && <SecretField label="New Signing Secret" value={secret} copyLabel="Copy secret" />}
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Button variant="outline" onClick={() => closeDialog('dlg-rotate-secret')}>
              {secret ? 'Close' : 'Cancel'}
            </Button>
            {!secret && (
              <Button variant="destructive" disabled={busy} onClick={rotate}>
                {busy ? 'Rotating…' : 'Rotate Secret Now'}
              </Button>
            )}
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
