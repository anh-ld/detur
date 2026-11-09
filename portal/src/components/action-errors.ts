import { isGateAbort } from '../lib/admin';

export type ActionErrors = { clear: () => void; fail: (e: unknown) => void };

export const actionErrors = (set: (msg: string) => void): ActionErrors => ({
  clear: () => set(''),
  fail: (e) => {
    if (!isGateAbort(e)) set(String(e));
  },
});
