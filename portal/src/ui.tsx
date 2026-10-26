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

// 1.5px outline icon paths (24px grid); decorative, the button label carries the name
export const ICONS = {
  copy: 'M9 9h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H9a1 1 0 0 1-1-1V10a1 1 0 0 1 1-1zM16 9V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v10a1 1 0 0 0 1 1h3',
  check: 'M20 6 9 17l-5-5',
  open: 'M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6',
  up: 'M12 19V5M5 12l7-7 7 7',
  down: 'M12 5v14M19 12l-7 7-7-7',
  edit: 'M17 3a2.85 2.85 0 0 1 4 4L7.5 20.5 2 22l1.5-5.5Z',
  trash: 'M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6',
  close: 'M18 6 6 18M6 6l12 12',
  plus: 'M12 5v14M5 12h14',
};

export const Icon = ({ d, spin, size = 16 }: { d: string; spin?: boolean; size?: number }) => (
  <svg class={spin ? 'spin' : undefined} width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor"
    stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path d={d} />
  </svg>
);

// square ghost button, icon only: label -> aria-label + hover tooltip
export function IconButton({ icon, label, ...rest }: { icon: string; label: string } & Record<string, unknown>) {
  return (
    <Button type="button" size="sm" variant="ghost" class="icon-btn" aria-label={label} title={label} {...rest}>
      <Icon d={icon} />
    </Button>
  );
}

// copy button: "Copied" (or a check) for 1.5s. icon = compact ghost version for inline values
export function CopyButton({ value, label, icon }: { value: string; label: string; icon?: boolean }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(t);
  }, [copied]);
  const onClick = async () => {
    await copyText(value);
    setCopied(true);
  };
  if (icon) return <IconButton icon={copied ? ICONS.check : ICONS.copy} label={copied ? 'Copied' : label} onClick={onClick} />;
  return (
    <Button size="sm" variant="outline" onClick={onClick}>
      {copied ? 'Copied' : label}
    </Button>
  );
}

// no-track flag (server paramNoTrack): portal previews never count as clicks
export const previewUrl = (url: string) => `${url}?detur-no-track=1`;

// full short link: host muted, key strong; copy + open (untracked) beside it
export function ShortLink({ base, linkKey }: { base: string; linkKey: string }) {
  const url = `${base}/${linkKey}`;
  return (
    <span class="short-link">
      <span class="short-link-text" title={url}>
        <span class="short-link-host">{base.replace(/^https?:\/\//, '')}/</span>
        <strong>{linkKey}</strong>
      </span>
      {base && (
        <>
          <CopyButton icon value={url} label="Copy link" />
          <IconButton icon={ICONS.open} label="Open link (not counted as a click)" href={previewUrl(url)} target="_blank" rel="noreferrer" />
        </>
      )}
    </span>
  );
}

// backdrop press closes any open dialog like Escape (kinu does it only for Dialog.Trigger;
// portal opens dialogs by id). Press must start + end outside the panel: drag-select out of a field won't close.
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

// --- Admin elevation: sudo-style password session. Server enforces; this store mirrors
// GET /api/admin/session; adminSet mirrors config.adminSet (unset password = no gating). ---
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

// re-read session from server; 403 handler calls this first
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

// prompt dialog id: gate opens, dialog closes
const ADMIN_PW_DIALOG = 'dlg-admin-password';

// on-demand gate: no password set or lifted → pass; else prompt, resolves on close (false = canceled).
// concurrent gates share one pending prompt: no orphaned waiter.
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

// gate at a dialog trigger: open sync if gate passes (no password or lifted), else prompt first
export function gateOpen(dialogId: string) {
  if (adminSet && !elevated) {
    ensureAdmin().then((ok) => ok && openDialog(dialogId));
  } else {
    openDialog(dialogId);
  }
}

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

// prompt canceled: skip gated action (callers return quietly)
export class AdminGateAborted extends Error {}

// one gated API call: prompt if needed. Mid-use 403 = session died (also proves server gates,
// self-heals failed config fetch) -> refresh state (chip clears), re-prompt once, retry.
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

// kinu Spinner is inline-block -> center it
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

// in-page confirm (replaces window.confirm). requireAdmin gates the trigger: prompt first,
// confirm opens only on successful elevation. Open/close by id: kinu Dialog.Close needs the
// Dialog.Trigger polyfill, absent here.
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
