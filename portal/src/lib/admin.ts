import { useEffect, useRef, useState } from 'preact/hooks';
import { api, HttpError } from './api';
import { openDialog } from './dialog';
import { useLatest } from './use-latest';

// --- Admin elevation: sudo-style password session. Server enforces; this store mirrors
// GET /api/admin/session; adminSet mirrors config.adminSet (unset password = no gating). ---
let adminSet = false;
let elevated = false;
let adminVersion = 0; // bumped on every state change: a session read started before one is stale
let until = ''; // expiresAt ISO; '' = not lifted
const adminListeners = new Set<() => void>();

export const getAdmin = () => elevated;

export function setAdminAvailable(v: boolean) {
  if (v === adminSet) return;
  adminSet = v;
  adminListeners.forEach((l) => l());
}
export function setElevated(v: boolean, expiresAt = '') {
  adminVersion++;
  if (v === elevated && expiresAt === until) return;
  elevated = v;
  until = expiresAt;
  adminListeners.forEach((l) => l());
}
function subscribeAdmin(cb: () => void): () => void {
  adminListeners.add(cb);
  return () => {
    adminListeners.delete(cb);
  };
}

// re-read session from server; 403 handler calls this first
// a slow read (e.g. the mount check) landing after an unlock or exit would undo it: dropped
export async function refreshAdminSession() {
  const v = adminVersion;
  try {
    const s = await api.getAdminSession();
    if (v === adminVersion) setElevated(s.admin, s.expiresAt ?? '');
  } catch {
    if (v === adminVersion) setElevated(false);
  }
}

let pendingAdmin: Promise<boolean> | null = null;
let settleAdmin: ((ok: boolean) => void) | null = null;

// prompt dialog id: gate opens, dialog closes
export const ADMIN_PW_DIALOG = 'dlg-admin-password';

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

// the one admin snapshot: password set?, elevated?, until; re-renders its component on any change.
// locked = a password is set and the session isn't elevated.
export function useAdmin() {
  const [, bump] = useState(0);
  useEffect(() => {
    const off = subscribeAdmin(() => bump((n) => n + 1));
    bump((n) => n + 1); // a change between render and this effect had no listener: re-read once
    return off;
  }, []);
  return { adminSet, admin: elevated, until, locked: adminSet && !elevated };
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
    await sessionDied();
    if (await ensureAdmin()) return await fn();
    throw new AdminGateAborted();
  }
}

// a 403 from a gated call proves the server gates and the session is gone: mark the password set
// (self-heals a failed config fetch), re-read the session (chip clears). Never prompts.
async function sessionDied() {
  setAdminAvailable(true);
  await refreshAdminSession();
}

// Admin-gated load, one rule for every panel. Never prompts, never asks the server while the gate is closed.
// - 'when-set': open when no password is set; closed = password set and not elevated. Unlock loads only
//   until the first success (edits are never clobbered); deps changes reload.
// - 'needs-password': the server 403s without a password, so closed = not set or not elevated. Lists:
//   elevation is a dep (refetch on every unlock) and a closed gate resets data to `empty` (secrets leave state).
// A load 403 = session died (sessionDied) -> locked. reload() re-runs through the same gate and guard.
export function useGatedLoad<T>(opts: {
  load: () => Promise<T>;
  deps: unknown[];
  mode: 'when-set' | 'needs-password';
  empty?: T;
  onLoaded?: (v: T) => void;
}) {
  const adm = useAdmin();
  const latest = useLatest();
  const loaded = useRef(false);
  const cur = useRef(opts);
  cur.current = opts;
  const [data, setData] = useState<T | null>(null);
  const [locked, setLocked] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const reload = (): Promise<void> => {
    const { load, mode, empty, onLoaded } = cur.current;
    const live = latest.begin();
    setError('');
    const list = mode === 'needs-password';
    if (list ? !adminSet || !elevated : adminSet && !elevated) {
      setLocked(true);
      if (list) setData(empty ?? null);
      setLoading(false);
      return Promise.resolve();
    }
    return load()
      .then(
        (v) => {
          if (!live()) return;
          loaded.current = true;
          setLocked(false);
          setData(v);
          onLoaded?.(v);
        },
        (e) => {
          if (!live()) return;
          if (e instanceof HttpError && e.status === 403) {
            setLocked(true);
            if (list) setData(empty ?? null);
            return sessionDied();
          }
          setError(String(e));
        },
      )
      .finally(() => live() && setLoading(false));
  };

  useEffect(() => {
    loaded.current = false;
    void reload();
    const unsub = opts.mode === 'when-set' ? subscribeAdmin(() => !loaded.current && void reload()) : undefined;
    return () => {
      unsub?.();
      latest.cancel();
    };
  }, [...opts.deps, opts.mode === 'needs-password' ? adm.adminSet : 0, opts.mode === 'needs-password' ? adm.admin : 0]);

  return { data, setData, locked, loading, error, reload };
}
