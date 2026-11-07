import { useEffect, useRef } from 'preact/hooks';

// latest-wins guard: begin() returns a check that stays true until the next begin(), cancel(), or unmount.
// Callers keep their own loading/error state; a superseded response just skips its setState.
export function useLatest() {
  const seq = useRef(0);
  const guard = useRef({
    begin: () => {
      const n = ++seq.current;
      return () => seq.current === n;
    },
    cancel: () => {
      seq.current++;
    },
  });
  useEffect(() => guard.current.cancel, []);
  return guard.current;
}
