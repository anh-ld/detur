import { useEffect, useRef } from 'preact/hooks';

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
