import { useRef, useState } from 'preact/hooks';
import { Alert, Button, Dialog, Field, Input, Label } from 'kinu';
import { api } from '../../../lib/api';
import { adminCall, AdminGateAborted } from '../../../lib/admin';
import { CopyButton } from '../../../components/copy-button';
import { AdminButton } from '../../../components/admin';
import { closeDialog, openDialog } from '../../../lib/dialog';
import { mono, muted, row } from '../../../lib/styles';
import { App, CreatedApp } from '../../../lib/types';

const stack = { display: 'grid', gap: 16 };
const actions = { ...row, justifyContent: 'flex-end' };

// mints a new key; the old one dies now. Plaintext is shown once, like create.
export function RotateKeyDialog({ app, onChanged }: { app: App; onChanged: () => void }) {
  const [rotated, setRotated] = useState<CreatedApp | null>(null);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  // ref guard: a second rotate would kill the key this dialog is about to show
  const rotating = useRef(false);
  const id = `dlg-rotate-${app.id}`;

  const rotate = async () => {
    if (rotating.current) return;
    rotating.current = true;
    setBusy(true);
    setErr('');
    try {
      setRotated(await adminCall(() => api.rotateKey(app.id)));
      onChanged();
    } catch (e) {
      if (e instanceof AdminGateAborted) return;
      setErr(String(e));
    } finally {
      rotating.current = false;
      setBusy(false);
    }
  };

  return (
    <Dialog id={id}>
      <AdminButton size="sm" variant="outline" onClick={() => openDialog(id)}>
        Rotate
      </AdminButton>
      <Dialog.Content>
        {rotated ? (
          <div style={stack}>
            <h2 style={{ margin: 0 }}>Key rotated</h2>
            <Alert variant="warning">Old key stopped working. New key shown once.</Alert>
            <Field>
              <Label htmlFor={`${id}-key`}>API key</Label>
              <Input id={`${id}-key`} readOnly value={rotated.apiKey} style={mono} />
              <Field.Description>EXPO_PUBLIC_DETOUR_API_KEY in the SDK config.</Field.Description>
            </Field>
            <div style={actions}>
              <Button
                variant="outline"
                onClick={() => {
                  setRotated(null);
                  closeDialog(id);
                }}
              >
                Done
              </Button>
              <CopyButton value={rotated.apiKey} label="Copy API key" />
            </div>
          </div>
        ) : (
          <form
            style={stack}
            onSubmit={(e) => {
              e.preventDefault();
              rotate();
            }}
          >
            <h2 style={{ margin: 0 }}>Rotate API key</h2>
            <p style={muted}>A new key is generated; the current one stops working immediately.</p>
            {err && <Alert variant="destructive">{err}</Alert>}
            <div style={actions}>
              <Button type="button" variant="outline" onClick={() => closeDialog(id)}>
                Cancel
              </Button>
              <Button type="submit" disabled={busy} aria-busy={busy}>
                Rotate key
              </Button>
            </div>
          </form>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
