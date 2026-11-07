import { Button, Dialog } from 'kinu';
import { ApiToken } from '../../lib/types';
import { closeDialog } from '../../lib/dialog';
import { muted, row } from '../../lib/styles';

export function RevokeTokenDialog({ token, onRevoke }: { token: ApiToken | null; onRevoke: (t: ApiToken) => void }) {
  return (
    <Dialog id="dlg-revoke-token">
      <Dialog.Content>
        <div style={{ display: 'grid', gap: 16 }}>
          <h2 style={{ margin: 0 }}>Revoke token</h2>
          <p style={muted}>
            Revoke <strong>{token?.name}</strong>? Anything using it stops working immediately.
          </p>
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Button variant="outline" onClick={() => closeDialog('dlg-revoke-token')}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                closeDialog('dlg-revoke-token');
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
