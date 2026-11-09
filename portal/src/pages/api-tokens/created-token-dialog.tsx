import { Button, Dialog } from 'kinu';
import { CreatedToken } from '../../lib/types';
import { SecretField } from '../../components/secret-field';
import { closeDialog } from '../../lib/dialog';
import { muted, stack, end } from '../../lib/styles';
import { mcpJson, mcpUrl } from './mcp';
import { Snippet } from './snippet';

export const CREATED_TOKEN_DIALOG = 'dlg-created-token';

export function CreatedTokenDialog({ created, base, onDone }: { created: CreatedToken | null; base: string; onDone: () => void }) {
  return (
    <Dialog id={CREATED_TOKEN_DIALOG}>
      <Dialog.Content>
        <div style={stack}>
          <h2 style={{ margin: 0 }}>Token created</h2>
          <p style={muted}>This token won't be shown again. Copy it now.</p>
          {created && (
            <>
              <SecretField label="Token" value={created.token} copyLabel="Copy token" />
              <Snippet label="MCP config" value={mcpJson(mcpUrl(base), created.token)} copyLabel="Copy MCP config" />
            </>
          )}
          <div style={end}>
            <Button
              onClick={() => {
                closeDialog(CREATED_TOKEN_DIALOG);
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
