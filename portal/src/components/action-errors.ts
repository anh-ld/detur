import { AdminGateAborted } from '../lib/admin';

// what dialogs and rows use to report into the panel's error alert
export type ActionErrors = { clear: () => void; fail: (e: unknown) => void };

export const actionErrors = (set: (msg: string) => void): ActionErrors => ({
  clear: () => set(''),
  fail: (e) => {
    if (!(e instanceof AdminGateAborted)) set(String(e));
  },
});
