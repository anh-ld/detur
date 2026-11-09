import { Button, Checkbox, Label, Popover, PopoverContent, PopoverTrigger } from 'kinu';
import { muted } from '../../lib/styles';

export function TagFilter({ tags, selected, onChange }: { tags: string[]; selected: string[]; onChange: (t: string[]) => void }) {
  const toggle = (t: string) => onChange(selected.includes(t) ? selected.filter((x) => x !== t) : [...selected, t]);
  return (
    <Popover>
      <PopoverTrigger>
        <Button variant="outline">Tags{selected.length ? ` · ${selected.length}` : ''}</Button>
      </PopoverTrigger>
      <PopoverContent>
        <div class="tag-filter-panel">
          {tags.length === 0 && <p style={muted}>No tags yet.</p>}
          {tags.map((t) => (
            <Label class="check-option" key={t}>
              <Checkbox checked={selected.includes(t)} onChange={() => toggle(t)} />
              {t}
            </Label>
          ))}
          {selected.length > 0 && (
            <Button size="sm" variant="outline" onClick={() => onChange([])}>
              Clear
            </Button>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
