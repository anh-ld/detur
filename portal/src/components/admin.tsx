import { ComponentChildren, ComponentProps } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { Alert, Button, Dialog, Field, Input, Label } from 'kinu';
import { ICONS, Icon } from './icons';
import { api, HttpError } from '../lib/api';
import { ADMIN_PW_DIALOG, ensureAdmin, setElevated, settleAdminGate, useAdmin } from '../lib/admin';
import { closeDialog } from '../lib/dialog';
import { muted, row, stack, end } from '../lib/styles';

export const LOCKED_HINT = 'Admin required: use Enter admin in the sidebar';

export const lockedProps = (locked: boolean) => (locked ? { title: LOCKED_HINT, onClick: () => void ensureAdmin() } : {});

export function AdminButton({ children, ...rest }: ComponentProps<typeof Button>) {
  const { locked } = useAdmin();
  return (
    <Button {...rest} {...lockedProps(locked)}>
      {locked && <Icon d={ICONS.lock} />}
      {children}
    </Button>
  );
}

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

export function AdminPasswordDialog() {
  const [pw, setPw] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const el = document.getElementById(ADMIN_PW_DIALOG) as HTMLDialogElement | null;
    if (!el) return;
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
      settleAdminGate(true);
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
          style={stack}
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
          <div style={end}>
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
