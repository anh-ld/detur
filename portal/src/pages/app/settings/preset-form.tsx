import { ComponentChildren, JSX } from 'preact';
import { useState } from 'preact/hooks';
import { Button, Field, Input, Label } from 'kinu';
import { api } from '../../../lib/api';
import { adminCall } from '../../../lib/admin';
import { muted, row } from '../../../lib/styles';
import { App } from '../../../lib/types';
import { ICONS, Icon, IconButton } from '../../../components/icons';
import { formGrid, useSave } from './save';

const MAX = 10;

type TextFieldProps = {
  id: string;
  label: string;
  value: string;
  placeholder?: string;
  maxLength?: number;
  onInput: (e: JSX.TargetedEvent<HTMLInputElement>) => void;
};

export function TextField({ id, label, ...input }: TextFieldProps) {
  return (
    <Field>
      <Label htmlFor={id}>{label}</Label>
      <Input id={id} {...input} />
    </Field>
  );
}

// Editable list of presets: a link copies one when picked, so edits never touch links already created.
export function PresetForm<T>({
  app,
  onSaved,
  intro,
  noun,
  read,
  patch,
  blank,
  fields,
}: {
  app: App;
  onSaved: (a: App) => void;
  intro: string;
  noun: string;
  read: (a: App) => T[] | undefined;
  patch: (items: T[]) => Parameters<typeof api.updateApp>[1];
  blank: T;
  fields: (item: T, set: (p: Partial<T>) => void, i: number) => ComponentChildren;
}) {
  const [items, setItems] = useState<T[]>(() => read(app) ?? []);
  const { onSubmit, status } = useSave(async () => {
    const a = await adminCall(() => api.updateApp(app.id, patch(items)));
    setItems(read(a) ?? []);
    onSaved(a);
  });

  return (
    <form style={formGrid} onSubmit={onSubmit}>
      <p style={muted}>{intro}</p>
      {items.map((p, i) => (
        <div class="preset-item" key={i}>
          <div class="form-grid">
            {fields(p, (change) => setItems(items.map((x, j) => (j === i ? { ...x, ...change } : x))), i)}
          </div>
          <IconButton
            icon={ICONS.trash}
            label={`Remove ${noun} ${i + 1}`}
            danger
            onClick={() => setItems(items.filter((_, j) => j !== i))}
          />
        </div>
      ))}
      <div style={{ ...row, justifyContent: 'space-between' }}>
        <Button type="button" variant="ghost" disabled={items.length >= MAX} onClick={() => setItems([...items, blank])}>
          <Icon d={ICONS.plus} />
          Add {noun}
        </Button>
        <Button type="submit">
          Save
        </Button>
      </div>
      {status}
    </form>
  );
}
