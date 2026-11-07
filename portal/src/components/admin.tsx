import { ComponentChildren } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { Alert, Button, Dialog, Field, Input, Label } from 'kinu';
import { api, HttpError } from '../lib/api';
import { ADMIN_PW_DIALOG, ensureAdmin, setElevated, settleAdminGate } from '../lib/admin';
import { closeDialog } from '../lib/dialog';
import { muted, row } from '../lib/styles';

// locked-section notice: shared look for admin-gated panels before elevation.
// elevating notifies subscribeAdmin -> panels reload.
export function AdminRequired({ children }: { children: ComponentChildren }) {
  return (
    <div
      style={{
        ...row,
        justifyContent: 'space-between',
        gap: 16,
        padding: 16,
        background: 'hsl(var(--k-card))',
        border: '1px solid hsl(var(--k-border))',
        borderRadius: 'var(--k-radius)',
      }}
    >
      <div>
        <strong>Admin required</strong>
        <p style={{ ...muted, marginTop: 4 }}>{children}</p>
      </div>
      <Button variant="outline" onClick={() => ensureAdmin()}>
        Unlock admin
      </Button>
    </div>
  );
}

// one instance in App shell, opened by ensureAdmin(). Server's only 401 = wrong password (1s delay).
export function AdminPasswordDialog() {
  const [pw, setPw] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const el = document.getElementById(ADMIN_PW_DIALOG) as HTMLDialogElement | null;
    if (!el) return;
    // any close aborts a pending gate (success already settled it) + clears the form:
    // next prompt never opens prefilled or with a stale error.
    const abort = () => {
      setPw('');
      setErr('');
      settleAdminGate(false);
    };
    el.addEventListener('cancel', abort);
    el.addEventListener('close', abort);
    return () => {
      el.removeEventListener('cancel', abort);
      el.removeEventListener('close', abort);
    };
  }, []);

  const submit = async () => {
    setErr('');
    setBusy(true);
    try {
      const { expiresAt } = await api.enterAdminMode(pw);
      setElevated(true, expiresAt);
      settleAdminGate(true); // resolve before close, else close event aborts
      closeDialog(ADMIN_PW_DIALOG);
    } catch (e) {
      setErr(e instanceof HttpError && e.status === 401 ? 'Wrong password' : String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog id={ADMIN_PW_DIALOG}>
      <Dialog.Content>
        <form
          style={{ display: 'grid', gap: 16 }}
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <div style={{ display: 'grid', gap: 4 }}>
            <h2 style={{ margin: 0 }}>Enter admin</h2>
            <p style={muted}>A master password unlocks app management for this browser session.</p>
          </div>
          <Field>
            <Label htmlFor="admin-pw">Password</Label>
            <Input id="admin-pw" type="password" value={pw} onInput={(e) => setPw(e.currentTarget.value)} />
          </Field>
          {err && (
            <Alert variant="destructive" role="alert">
              {err}
            </Alert>
          )}
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                settleAdminGate(false);
                closeDialog(ADMIN_PW_DIALOG);
              }}
            >
              Cancel
            </Button>
            <Button type="submit" disabled={busy} aria-busy={busy}>
              Unlock
            </Button>
          </div>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}
