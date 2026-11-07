// vitest `vi` subset on bun:test. stubGlobal has no bun equivalent.
import { jest, spyOn } from 'bun:test';

const saved = new Map<string, PropertyDescriptor | undefined>();

export const vi = {
  fn: jest.fn,
  spyOn,
  useFakeTimers: () => jest.useFakeTimers(),
  useRealTimers: () => jest.useRealTimers(),
  advanceTimersByTime: (ms: number) => jest.advanceTimersByTime(ms),
  setSystemTime: (t: number | Date) => jest.setSystemTime(t),
  stubGlobal(name: string, value: unknown) {
    if (!saved.has(name)) saved.set(name, Object.getOwnPropertyDescriptor(globalThis, name));
    Object.defineProperty(globalThis, name, { value, configurable: true, writable: true });
  },
  unstubAllGlobals() {
    for (const [name, d] of saved) d ? Object.defineProperty(globalThis, name, d) : delete (globalThis as any)[name];
    saved.clear();
  },
};
