import { useState } from 'preact/hooks';
import { Alert, Button, Input, Label, Table } from 'kinu';
import { muted, row } from './ui';
import { api, App } from './api';
import { inRange, THRESHOLD, WINDOW } from './matching';

export function MatchingTable({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  const [threshold, setThreshold] = useState(String(app.matchThreshold));
  const [windowMinutes, setWindowMinutes] = useState(String(app.matchWindowMinutes));
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

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
      const updated = await api.saveMatching(app.id, { threshold: th, windowMinutes: win });
      setThreshold(String(updated.matchThreshold));
      setWindowMinutes(String(updated.matchWindowMinutes));
      setSaved(true);
      onSaved(updated);
    } catch (e) {
      setError(String(e));
    }
  };

  const field = (id: string, value: string, set: (v: string) => void) => (
    <Input id={id} type="number" value={value} onInput={(e) => set(e.currentTarget.value)} style={{ maxWidth: 120 }} />
  );

  return (
    <form
      style={{ display: 'grid', gap: 16 }}
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <Table>
        <thead>
          <tr>
            <th>Setting</th>
            <th>Value</th>
            <th>Range</th>
            <th>Effect</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <Label htmlFor="set-th">Match threshold</Label>
            </td>
            <td>{field('set-th', threshold, setThreshold)}</td>
            <td data-label="Range" style={muted}>
              {THRESHOLD.min}–{THRESHOLD.max}
            </td>
            <td style={muted}>Higher means stricter install matching.</td>
          </tr>
          <tr>
            <td>
              <Label htmlFor="set-win">Match window (minutes)</Label>
            </td>
            <td>{field('set-win', windowMinutes, setWindowMinutes)}</td>
            <td data-label="Range" style={muted}>
              {WINDOW.min}–{WINDOW.max}
            </td>
            <td style={muted}>How long after a click an install can still match it.</td>
          </tr>
        </tbody>
      </Table>
      {error && <Alert variant="destructive">{error}</Alert>}
      {saved && <Alert variant="success">Saved.</Alert>}
      <div style={{ ...row, justifyContent: 'flex-end' }}>
        <Button type="submit">Save</Button>
      </div>
    </form>
  );
}
