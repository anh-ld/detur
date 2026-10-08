import { ComponentChildren } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { Alert, Button, Dialog, Field, Input, Label, Spinner } from 'kinu';
import { api, HttpError } from './api';

// Shared layout bits: kinu token colors, no layout primitives.
export const muted = { color: 'hsl(var(--k-muted-foreground))', fontSize: 14, margin: 0 };
export const mono = { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 13 };
export const row = { display: 'flex', gap: 8, alignItems: 'center' };

export const copyText = async (v: string) => {
  await navigator.clipboard.writeText(v);
};

// Ghost copy button: flashes "Copied" 1.5s after copy.
export function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(t);
  }, [copied]);
  return (
    <Button
      size="sm"
      variant="outline"
      onClick={async () => {
        await copyText(value);
        setCopied(true);
      }}
    >
      {copied ? 'Copied' : label}
    </Button>
  );
}

// Backdrop press closes any open dialog, like Escape (kinu wires this only for Dialog.Trigger;
// the portal opens dialogs by id). The press must start and end outside the panel, so a text
// selection dragged out of a field never closes it.
const onBackdrop = (e: MouseEvent) => {
  const d = e.target as HTMLElement;
  if (d.localName !== 'dialog') return false;
  const r = d.getBoundingClientRect();
  return e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom;
};
let pressedBackdrop = false;
addEventListener('pointerdown', (e) => (pressedBackdrop = onBackdrop(e)));
addEventListener('click', (e) => {
  if (pressedBackdrop && onBackdrop(e)) (e.target as HTMLDialogElement).close();
  pressedBackdrop = false;
});

export function openDialog(id: string) {
  (document.getElementById(id) as HTMLDialogElement | null)?.showModal();
}

export function closeDialog(id: string) {
  (document.getElementById(id) as HTMLDialogElement | null)?.close();
}

// --- Admin elevation: sudo-style password session. The server enforces the gate;
// this store mirrors GET /api/admin/session so surfaces follow it, and adminSet
// mirrors config.adminSet (unset password = no gating). ---
let adminSet = false;
let elevated = false;
let until = ''; // expiresAt ISO; '' = not lifted
const adminListeners = new Set<() => void>();

export const getAdminSet = () => adminSet;
export const getAdmin = () => elevated;
export const getAdminUntil = () => until;

export function setAdminAvailable(v: boolean) {
  if (v === adminSet) return;
  adminSet = v;
  adminListeners.forEach((l) => l());
}
export function setElevated(v: boolean, expiresAt = '') {
  if (v === elevated && expiresAt === until) return;
  elevated = v;
  until = expiresAt;
  adminListeners.forEach((l) => l());
}
export function subscribeAdmin(cb: () => void): () => void {
  adminListeners.add(cb);
  return () => {
    adminListeners.delete(cb);
  };
}

// Re-read the session from the server; the 403 handler also does this first.
export async function refreshAdminSession() {
  try {
    const s = await api.getAdminSession();
    setElevated(s.admin, s.expiresAt ?? '');
  } catch {
    setElevated(false);
  }
}

let pendingAdmin: Promise<boolean> | null = null;
let settleAdmin: ((ok: boolean) => void) | null = null;

// The password prompt's dialog id, shared by the gate (open) and the dialog (close).
const ADMIN_PW_DIALOG = 'dlg-admin-password';

// On-demand gate: no elevation exists (no password set) or already lifted → pass; otherwise the
// password prompt opens and resolves when it closes. false = canceled. Concurrent
// gates coalesce onto one pending prompt so no waiter is orphaned.
export function ensureAdmin(): Promise<boolean> {
  if (!adminSet || elevated) return Promise.resolve(true);
  if (pendingAdmin) return pendingAdmin;
  pendingAdmin = new Promise((resolve) => {
    settleAdmin = resolve;
    openDialog(ADMIN_PW_DIALOG);
  });
  return pendingAdmin;
}
export function settleAdminGate(ok: boolean) {
  settleAdmin?.(ok);
  settleAdmin = null;
  pendingAdmin = null;
}

// Pin the gate at a dialog trigger: open synchronously when the gate passes (no password set or lifted), else prompt first.
export function gateOpen(dialogId: string) {
  if (adminSet && !elevated) {
    ensureAdmin().then((ok) => ok && openDialog(dialogId));
  } else {
    openDialog(dialogId);
  }
}

// Locked-section notice: the one look every admin-gated panel shows before elevation.
// Elevating notifies subscribeAdmin, so panels reload themselves.
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

// The prompt was canceled: the gated action must not run (callers return quietly).
export class AdminGateAborted extends Error {}

// Run one gated API call: prompt first if needed; a 403 mid-use means the session
// died — a gated 403 also proves the server gates (self-heals a failed config fetch),
// so refresh state (the chip clears) and re-prompt once before retrying.
export async function adminCall<T>(fn: () => Promise<T>): Promise<T> {
  if (!(await ensureAdmin())) throw new AdminGateAborted();
  try {
    return await fn();
  } catch (e) {
    if (!(e instanceof HttpError) || e.status !== 403) throw e;
    setAdminAvailable(true);
    await refreshAdminSession();
    if (await ensureAdmin()) return await fn();
    throw new AdminGateAborted();
  }
}

// Password prompt for on-demand elevation. One instance, mounted in the App shell;
// ensureAdmin() opens it. The server's only 401 is a wrong password (1s delay).
export function AdminPasswordDialog() {
  const [pw, setPw] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const el = document.getElementById(ADMIN_PW_DIALOG) as HTMLDialogElement | null;
    if (!el) return;
    // Escape or any close aborts a still-pending gate (success already settled it) and
    // clears the form, so the next prompt never opens prefilled or with a stale error.
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
      settleAdminGate(true); // resolve before closing: the close event must not abort
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
            <Button type="button" variant="outline" onClick={() => { settleAdminGate(false); closeDialog(ADMIN_PW_DIALOG); }}>
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

// Page/section loading state: kinu's Spinner is inline-block, so center it.
export const Loading = () => (
  <div style={{ display: 'grid', placeItems: 'center', padding: '48px 0' }}>
    <Spinner />
  </div>
);

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: ComponentChildren;
  description?: ComponentChildren;
  actions?: ComponentChildren;
}) {
  return (
    <div style={{ ...row, justifyContent: 'space-between', margin: '32px 0 24px', flexWrap: 'wrap', gap: 16 }}>
      <div style={{ display: 'grid', gap: 6 }}>
        <h1 style={{ margin: 0, fontSize: 28 }}>{title}</h1>
        {description && <p style={muted}>{description}</p>}
      </div>
      {actions && <div style={row}>{actions}</div>}
    </div>
  );
}

// Delete button: in-page confirm step (replaces window.confirm). requireAdmin pins
// the gate at the trigger: the password prompt opens first, and only a successful
// elevation opens this same confirmation dialog. Opened and closed imperatively by
// id (kinu's Dialog.Close relies on the Dialog.Trigger polyfill, absent here).
export function ConfirmDelete({
  title,
  body,
  onConfirm,
  id,
  requireAdmin,
}: {
  title: string;
  body: ComponentChildren;
  onConfirm: () => void;
  id: string;
  requireAdmin?: boolean;
}) {
  return (
    <Dialog id={id}>
      <Button size="sm" variant="destructive" onClick={() => (requireAdmin ? gateOpen(id) : openDialog(id))}>
        Delete
      </Button>
      <Dialog.Content>
        <div style={{ display: 'grid', gap: 16 }}>
          <h2 style={{ margin: 0 }}>{title}</h2>
          <p style={muted}>{body}</p>
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Button variant="outline" onClick={() => closeDialog(id)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={() => { onConfirm(); closeDialog(id); }}>
              Delete
            </Button>
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
