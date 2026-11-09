import { useEffect, useState } from 'preact/hooks';
import { Button, Dialog } from 'kinu';
import { api } from '../../../lib/api';
import { Webhook } from '../../../lib/types';
import { adminCall } from '../../../lib/admin';
import { SecretField } from '../../../components/secret-field';
import { closeDialog } from '../../../lib/dialog';
import { muted, stack, end } from '../../../lib/styles';
import { ActionErrors } from '../../../components/action-errors';

export const ROTATE_SECRET_DIALOG = 'dlg-rotate-secret';

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
    <Dialog id={ROTATE_SECRET_DIALOG}>
      <Dialog.Content>
        <div style={stack}>
          <h2 style={{ margin: 0 }}>Rotate Signing Secret</h2>
          <p style={muted}>
            Rotating the secret invalidates the old secret immediately. Existing signature verification on your receiver will fail until
            updated with the new secret.
          </p>
          {secret && <SecretField label="New Signing Secret" value={secret} copyLabel="Copy secret" />}
          <div style={end}>
            <Button variant="outline" onClick={() => closeDialog(ROTATE_SECRET_DIALOG)}>
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
