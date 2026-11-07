import { JSX } from 'preact';
import { useState } from 'preact/hooks';
import { Alert } from 'kinu';
import { AdminGateAborted } from '../../../lib/admin';

export const formGrid = { display: 'grid', gap: 16 };

// fn returns an error message to show, or nothing on success. A closed admin gate stays silent.
export function useSave(fn: () => Promise<string | void>) {
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);
  const onSubmit = async (e: JSX.TargetedEvent<HTMLFormElement>) => {
    e.preventDefault();
    setError('');
    setSaved(false);
    try {
      const problem = await fn();
      if (problem) setError(problem);
      else setSaved(true);
    } catch (err) {
      if (err instanceof AdminGateAborted) return;
      setError(String(err));
    }
  };
  const status = (
    <>
      {error && <Alert variant="destructive">{error}</Alert>}
      {saved && <Alert variant="success">Saved.</Alert>}
    </>
  );
  return { onSubmit, status };
}
