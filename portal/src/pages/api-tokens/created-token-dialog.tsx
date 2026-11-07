import { Button, Dialog } from 'kinu';
import { CreatedToken } from '../../lib/types';
import { SecretField } from '../../components/secret-field';
import { closeDialog } from '../../lib/dialog';
import { muted, row } from '../../lib/styles';
import { mcpJson, mcpUrl } from './mcp';
import { Snippet } from './snippet';

// plaintext token lives in the parent's state only until Done clears `created`
export function CreatedTokenDialog({ created, base, onDone }: { created: CreatedToken | null; base: string; onDone: () => void }) {
  return (
    <Dialog id="dlg-created-token">
      <Dialog.Content>
        <div style={{ display: 'grid', gap: 16 }}>
          <h2 style={{ margin: 0 }}>Token created</h2>
          <p style={muted}>This token won't be shown again. Copy it now.</p>
          {created && (
            <>
              <SecretField label="Token" value={created.token} copyLabel="Copy token" />
              <Snippet label="MCP config" value={mcpJson(mcpUrl(base), created.token)} copyLabel="Copy MCP config" />
            </>
          )}
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Button
              onClick={() => {
                closeDialog('dlg-created-token');
                onDone();
              }}
            >
              Done
            </Button>
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
