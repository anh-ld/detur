import { useEffect, useState } from 'preact/hooks';
import { Button, Dialog, Field, Input, Label } from 'kinu';
import { api } from '../../lib/api';
import { ApiToken, App } from '../../lib/types';
import { adminCall } from '../../lib/admin';
import { ActionErrors } from '../../components/action-errors';
import { closeDialog } from '../../lib/dialog';
import { row } from '../../lib/styles';
import { AppChecks } from './app-checks';

// fields re-prefill when the target token changes
export function EditTokenDialog({
  token,
  apps,
  errors,
  onSaved,
}: {
  token: ApiToken | null;
  apps: App[];
  errors: ActionErrors;
  onSaved: (t: ApiToken) => void;
}) {
  const [name, setName] = useState(token?.name ?? '');
  const [ids, setIds] = useState(token?.appIds ?? []);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    setName(token?.name ?? '');
    setIds(token?.appIds ?? []);
  }, [token]);

  const save = async (e: Event) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true);
    try {
      onSaved(await adminCall(() => api.updateToken(token.id, { name: name.trim(), appIds: ids })));
      closeDialog('dlg-edit-token');
    } catch (e) {
      errors.fail(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog id="dlg-edit-token">
      <Dialog.Content>
        <form style={{ display: 'grid', gap: 16 }} onSubmit={save}>
          <h2 style={{ margin: 0 }}>Edit token</h2>
          <Field>
            <Label htmlFor="tok-edit-name">Name</Label>
            <Input id="tok-edit-name" value={name} onInput={(e) => setName(e.currentTarget.value)} />
          </Field>
          <Field>
            <Label>Allowed apps</Label>
            <AppChecks apps={apps} value={ids} onChange={setIds} />
          </Field>
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Button type="button" variant="outline" onClick={() => closeDialog('dlg-edit-token')}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !name.trim() || ids.length === 0}>
              {busy ? 'Saving…' : 'Save'}
            </Button>
          </div>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}
