import { Button, Dialog } from 'kinu';
import { SecretField } from '../../../components/secret-field';
import { closeDialog } from '../../../lib/dialog';
import { muted, row } from '../../../lib/styles';

export function CreatedSecretDialog({ secret }: { secret: string | null }) {
  return (
    <Dialog id="dlg-created-secret">
      <Dialog.Content>
        <div style={{ display: 'grid', gap: 16 }}>
          <h2 style={{ margin: 0 }}>Webhook Created</h2>
          <p style={muted}>
            Your endpoint has been configured. Save this signing secret now to verify incoming <code>Detur-Signature</code> headers on your
            destination server.
          </p>
          {secret && <SecretField label="Signing Secret" value={secret} copyLabel="Copy secret" />}
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Button onClick={() => closeDialog('dlg-created-secret')}>Done</Button>
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
