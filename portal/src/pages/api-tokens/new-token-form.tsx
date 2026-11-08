import { useState } from 'preact/hooks';
import { Button, Dialog, Field, Input, Label } from 'kinu';
import { api } from '../../lib/api';
import { App, CreatedToken } from '../../lib/types';
import { adminCall } from '../../lib/admin';
import { ActionErrors } from '../../components/action-errors';
import { closeDialog } from '../../lib/dialog';
import { muted, row } from '../../lib/styles';
import { AppChecks } from './app-checks';

export const NEW_TOKEN_DIALOG = 'dlg-new-token';

// drawer opened by the page's "New token" action (same pattern as Create app)
export function NewTokenDialog({ apps, errors, onCreated }: { apps: App[]; errors: ActionErrors; onCreated: (t: CreatedToken) => void }) {
  const [name, setName] = useState('');
  const [picked, setPicked] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const invalid = !name.trim() || picked.length === 0;

  const create = async (e: Event) => {
    e.preventDefault();
    if (busy || invalid) return;
    setBusy(true);
    errors.clear();
    try {
      const t = await adminCall(() => api.createToken({ name: name.trim(), appIds: picked }));
      setName('');
      setPicked([]);
      closeDialog(NEW_TOKEN_DIALOG);
      onCreated(t);
    } catch (e) {
      errors.fail(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog id={NEW_TOKEN_DIALOG}>
      <Dialog.Content>
        <form style={{ display: 'grid', gap: 16 }} onSubmit={create}>
          <div style={{ display: 'grid', gap: 4 }}>
            <h2 style={{ margin: 0 }}>New token</h2>
            <p style={muted}>Read-only access to the apps you pick. Shown once after creating.</p>
          </div>
          <Field>
            <Label htmlFor="tok-name">Name</Label>
            <Input id="tok-name" autocomplete="off" placeholder="Claude Code" value={name} onInput={(e) => setName(e.currentTarget.value)} />
          </Field>
          <Field>
            <Label>Allowed apps</Label>
            <AppChecks apps={apps} value={picked} onChange={setPicked} />
          </Field>
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Button type="button" variant="outline" onClick={() => closeDialog(NEW_TOKEN_DIALOG)}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || invalid}>
              {busy ? 'Creating…' : 'Create token'}
            </Button>
          </div>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}
