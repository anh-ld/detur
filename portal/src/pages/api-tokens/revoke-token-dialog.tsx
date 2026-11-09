import { Button, Dialog } from 'kinu';
import { ApiToken } from '../../lib/types';
import { closeDialog } from '../../lib/dialog';
import { muted, stack, end } from '../../lib/styles';

export const REVOKE_TOKEN_DIALOG = 'dlg-revoke-token';

export function RevokeTokenDialog({ token, onRevoke }: { token: ApiToken | null; onRevoke: (t: ApiToken) => void }) {
  return (
    <Dialog id={REVOKE_TOKEN_DIALOG}>
      <Dialog.Content>
        <div style={stack}>
          <h2 style={{ margin: 0 }}>Revoke token</h2>
          <p style={muted}>
            Revoke <strong>{token?.name}</strong>? Anything using it stops working immediately.
          </p>
          <div style={end}>
            <Button variant="outline" onClick={() => closeDialog(REVOKE_TOKEN_DIALOG)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                closeDialog(REVOKE_TOKEN_DIALOG);
                if (token) onRevoke(token);
              }}
            >
              Revoke
            </Button>
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
