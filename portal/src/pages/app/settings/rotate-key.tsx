import { useRef, useState } from 'preact/hooks';
import { Alert, Button, Dialog, Field, Input, Label } from 'kinu';
import { api } from '../../../lib/api';
import { adminCall } from '../../../lib/admin';
import { actionErrors } from '../../../components/action-errors';
import { CopyButton } from '../../../components/copy-button';
import { AdminButton } from '../../../components/admin';
import { closeDialog, openDialog } from '../../../lib/dialog';
import { mono, muted, stack, end } from '../../../lib/styles';
import { App, CreatedApp } from '../../../lib/types';


export function RotateKeyDialog({ app, onChanged }: { app: App; onChanged: () => void }) {
  const [rotated, setRotated] = useState<CreatedApp | null>(null);
  const [err, setErr] = useState('');
  const errors = actionErrors(setErr);
  const [busy, setBusy] = useState(false);
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
      errors.fail(e);
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
            <div style={end}>
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
            <div style={end}>
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
