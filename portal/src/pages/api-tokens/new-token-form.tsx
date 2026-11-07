import { useState } from 'preact/hooks';
import { Button, Card, Field, Input, Label } from 'kinu';
import { api } from '../../lib/api';
import { App, CreatedToken } from '../../lib/types';
import { adminCall } from '../../lib/admin';
import { ActionErrors } from '../../components/action-errors';
import { row } from '../../lib/styles';
import { AppChecks } from './app-checks';

export function NewTokenForm({ apps, errors, onCreated }: { apps: App[]; errors: ActionErrors; onCreated: (t: CreatedToken) => void }) {
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
      onCreated(await adminCall(() => api.createToken({ name: name.trim(), appIds: picked })));
      setName('');
      setPicked([]);
    } catch (e) {
      errors.fail(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card>
      <form style={{ display: 'grid', gap: 16, padding: 16 }} onSubmit={create}>
        <h2 style={{ margin: 0, fontSize: 20 }}>New token</h2>
        <Field>
          <Label htmlFor="tok-name">Name</Label>
          <Input id="tok-name" placeholder="Claude Code" value={name} onInput={(e) => setName(e.currentTarget.value)} />
        </Field>
        <Field>
          <Label>Allowed apps</Label>
          <AppChecks apps={apps} value={picked} onChange={setPicked} />
        </Field>
        <div style={{ ...row, justifyContent: 'flex-end' }}>
          <Button type="submit" disabled={busy || invalid}>
            {busy ? 'Creating…' : 'Create token'}
          </Button>
        </div>
      </form>
    </Card>
  );
}
