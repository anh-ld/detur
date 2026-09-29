import { useEffect, useState } from 'preact/hooks';
import { Alert, Button, Card, Field, Input, Label } from 'kinu';
import { PageHeader, row } from './ui';
import { api } from './api';
import { inRange, THRESHOLD, WINDOW } from './matching';

export function SettingsPage() {
  const [threshold, setThreshold] = useState('');
  const [windowMinutes, setWindowMinutes] = useState('');
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    api
      .getSettings()
      .then((s) => {
        setThreshold(String(s.threshold));
        setWindowMinutes(String(s.windowMinutes));
      })
      .catch((e) => setError(String(e)));
  }, []);

  const save = async () => {
    setError('');
    setSaved(false);
    if (threshold.trim() === '' || windowMinutes.trim() === '') {
      setError('threshold and window are required');
      return;
    }
    const th = Number(threshold);
    const win = Number(windowMinutes);
    if (!Number.isFinite(th) || !Number.isFinite(win)) {
      setError('threshold and window must be numbers');
      return;
    }
    if (!inRange(th, THRESHOLD) || !inRange(win, WINDOW)) {
      setError(`threshold must be ${THRESHOLD.min}–${THRESHOLD.max}, window ${WINDOW.min}–${WINDOW.max} minutes`);
      return;
    }
    try {
      const s = await api.saveSettings({ threshold: th, windowMinutes: win });
      setThreshold(String(s.threshold));
      setWindowMinutes(String(s.windowMinutes));
      setSaved(true);
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <div>
      <PageHeader title="Settings" description="Default matching for every link. Each link can override these." />
      <Card style={{ maxWidth: 480 }}>
        <form
          style={{ display: 'grid', gap: 16 }}
          onSubmit={(e) => {
            e.preventDefault();
            save();
          }}
        >
          <Field>
            <Label htmlFor="set-th">Match threshold</Label>
            <Input id="set-th" type="number" value={threshold} onInput={(e) => setThreshold(e.currentTarget.value)} />
            <Field.Description>700–1200. Higher means stricter install matching.</Field.Description>
          </Field>
          <Field>
            <Label htmlFor="set-win">Match window (minutes)</Label>
            <Input
              id="set-win"
              type="number"
              value={windowMinutes}
              onInput={(e) => setWindowMinutes(e.currentTarget.value)}
            />
            <Field.Description>5–180. How long after a click an install can still match it.</Field.Description>
          </Field>
          {error && <Alert variant="destructive">{error}</Alert>}
          {saved && <Alert variant="success">Saved.</Alert>}
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Button type="submit">Save</Button>
          </div>
        </form>
      </Card>
    </div>
  );
}
