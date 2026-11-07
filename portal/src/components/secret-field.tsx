import { Field, Input, Label } from 'kinu';
import { mono, row } from '../lib/styles';
import { CopyButton } from './copy-button';

export function SecretField({ label, value, copyLabel }: { label: string; value: string; copyLabel: string }) {
  return (
    <Field>
      <Label>{label}</Label>
      <div style={row}>
        <Input readOnly value={value} style={mono} />
        <CopyButton value={value} label={copyLabel} />
      </div>
    </Field>
  );
}
