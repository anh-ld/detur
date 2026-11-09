import { useState } from 'preact/hooks';
import { Chip, Combobox, ComboboxInput, ComboboxList, ComboboxOption, Field, Label } from 'kinu';
import { muted } from '../../../lib/styles';

const has = (tags: string[], t: string) => tags.some((x) => x.toLowerCase() === t.toLowerCase());

export function TagInput({ id, tags, existing, onChange }: { id: string; tags: string[]; existing: string[]; onChange: (tags: string[]) => void }) {
  const [text, setText] = useState('');
  const add = () => {
    const t = text.trim();
    if (!t) return;
    if (!has(tags, t)) onChange([...tags, t]);
    setText('');
  };

  return (
    <Field>
      <Label htmlFor={`${id}-tags`}>
        Tags <span style={muted}>optional</span>
      </Label>
      <div class="tag-box">
        {tags.map((t) => (
          <Chip key={t}>
            {t}
            <Chip.Button aria-label={`Remove ${t}`} onClick={() => onChange(tags.filter((x) => x !== t))}>
              ×
            </Chip.Button>
          </Chip>
        ))}
        <Combobox>
          <ComboboxInput
            id={`${id}-tags`}
            value={text}
            onInput={(e) => setText(e.currentTarget.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                add();
              } else if (e.key === 'Backspace' && !text && tags.length) {
                onChange(tags.slice(0, -1));
              }
            }}
            placeholder={tags.length ? '' : 'Type or pick a tag, press Enter'}
          />
          <ComboboxList>
            {existing
              .filter((t) => !has(tags, t))
              .map((t) => (
                <ComboboxOption
                  key={t}
                  onClick={(e: MouseEvent) => {
                    e.preventDefault();
                    e.stopPropagation();
                    onChange([...tags, t]);
                    setText('');
                    ((e.currentTarget as HTMLElement).closest('[k="combobox-list"]') as HTMLDialogElement | null)?.close();
                  }}
                >
                  {t}
                </ComboboxOption>
              ))}
          </ComboboxList>
        </Combobox>
      </div>
      <Field.Description>Press Enter to add. The Links list filters by tag.</Field.Description>
    </Field>
  );
}
