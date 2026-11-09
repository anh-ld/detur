import { Button, Dialog } from 'kinu';
import { SecretField } from '../../../components/secret-field';
import { closeDialog } from '../../../lib/dialog';
import { muted, stack, end } from '../../../lib/styles';

export const CREATED_SECRET_DIALOG = 'dlg-created-secret';

export function CreatedSecretDialog({ secret }: { secret: string | null }) {
  return (
    <Dialog id={CREATED_SECRET_DIALOG}>
      <Dialog.Content>
        <div style={stack}>
          <h2 style={{ margin: 0 }}>Webhook Created</h2>
          <p style={muted}>
            Your endpoint has been configured. Save this signing secret now to verify incoming <code>Detur-Signature</code> headers on your
            destination server.
          </p>
          {secret && <SecretField label="Signing Secret" value={secret} copyLabel="Copy secret" />}
          <div style={end}>
            <Button onClick={() => closeDialog(CREATED_SECRET_DIALOG)}>Done</Button>
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
