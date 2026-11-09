import { useState } from 'preact/hooks';
import { Button, Input, Label, Table } from 'kinu';
import { api } from '../../../lib/api';
import { adminCall } from '../../../lib/admin';
import { inRange, THRESHOLD, WINDOW } from '../../../lib/matching';
import { muted, stack, end } from '../../../lib/styles';
import { App } from '../../../lib/types';
import { useSave } from './save';

export function MatchingTable({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  const [threshold, setThreshold] = useState(String(app.matchThreshold));
  const [windowMinutes, setWindowMinutes] = useState(String(app.matchWindowMinutes));
  const { onSubmit, status } = useSave(async () => {
    if (threshold.trim() === '' || windowMinutes.trim() === '') return 'threshold and window are required';
    const th = Number(threshold);
    const win = Number(windowMinutes);
    if (!Number.isFinite(th) || !Number.isFinite(win)) return 'threshold and window must be numbers';
    if (!inRange(th, THRESHOLD) || !inRange(win, WINDOW)) {
      return `threshold must be ${THRESHOLD.min}–${THRESHOLD.max}, window ${WINDOW.min}–${WINDOW.max} minutes`;
    }
    const updated = await adminCall(() => api.saveMatching(app.id, { threshold: th, windowMinutes: win }));
    setThreshold(String(updated.matchThreshold));
    setWindowMinutes(String(updated.matchWindowMinutes));
    onSaved(updated);
  });

  const line = (id: string, label: string, value: string, set: (v: string) => void, range: { min: number; max: number }, effect: string) => (
    <tr>
      <td>
        <Label htmlFor={id}>{label}</Label>
      </td>
      <td>
        <Input id={id} type="number" value={value} onInput={(e) => set(e.currentTarget.value)} style={{ maxWidth: 120 }} />
      </td>
      <td data-label="Range" style={muted}>
        {range.min}–{range.max}
      </td>
      <td style={muted}>{effect}</td>
    </tr>
  );

  return (
    <form style={stack} onSubmit={onSubmit}>
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
          {line('set-th', 'Match threshold', threshold, setThreshold, THRESHOLD, 'Higher means stricter install matching.')}
          {line('set-win', 'Match window (minutes)', windowMinutes, setWindowMinutes, WINDOW, 'How long after a click an install can still match it.')}
        </tbody>
      </Table>
      {status}
      <div style={end}>
        <Button type="submit">Save</Button>
      </div>
    </form>
  );
}
