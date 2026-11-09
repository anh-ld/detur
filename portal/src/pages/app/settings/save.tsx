import { JSX } from 'preact';
import { useState } from 'preact/hooks';
import { Alert } from 'kinu';
import { actionErrors } from '../../../components/action-errors';

export function useSave(fn: () => Promise<string | void>) {
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);
  const errors = actionErrors(setError);
  const onSubmit = async (e: JSX.TargetedEvent<HTMLFormElement>) => {
    e.preventDefault();
    setError('');
    setSaved(false);
    try {
      const problem = await fn();
      if (problem) setError(problem);
      else setSaved(true);
    } catch (err) {
      errors.fail(err);
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
