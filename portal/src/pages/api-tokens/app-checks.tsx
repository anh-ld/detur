import { Checkbox, Label } from 'kinu';
import { App } from '../../lib/types';
import { muted } from '../../lib/styles';

// app checkboxes shared by create form and edit dialog
export function AppChecks({ apps, value, onChange }: { apps: App[]; value: string[]; onChange: (ids: string[]) => void }) {
  if (apps.length === 0) return <p style={muted}>No apps yet. Create an app first.</p>;
  const toggle = (id: string) => onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id]);
  return (
    <div style={{ display: 'grid', gap: 10, marginTop: 4 }}>
      {apps.map((a) => (
        <Label key={a.id} class="check-option">
          <Checkbox checked={value.includes(a.id)} onChange={() => toggle(a.id)} />
          <span class="check-option-title">{a.name}</span>
        </Label>
      ))}
    </div>
  );
}
