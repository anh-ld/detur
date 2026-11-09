import { useEffect, useRef, useState } from 'preact/hooks';
import { api, HttpError } from './api';
import { openDialog } from './dialog';
import { useLatest } from './use-latest';

let adminSet = false;
let elevated = false;
let adminVersion = 0;
let until = '';
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

export const ADMIN_PW_DIALOG = 'dlg-admin-password';

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

export function useAdmin() {
  const [, bump] = useState(0);
  const snap = { adminSet, admin: elevated, until, locked: adminSet && !elevated };
  useEffect(() => {
    const off = subscribeAdmin(() => bump((n) => n + 1));
    if (adminSet !== snap.adminSet || elevated !== snap.admin || until !== snap.until) bump((n) => n + 1);
    return off;
  }, []);
  return snap;
}
export class AdminGateAborted extends Error {}
export const isGateAbort = (e: unknown) => e instanceof AdminGateAborted;

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

async function sessionDied() {
  setAdminAvailable(true);
  await refreshAdminSession();
}

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
