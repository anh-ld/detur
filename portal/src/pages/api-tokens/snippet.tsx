import { Field, Label } from 'kinu';
import { CopyButton } from '../../components/copy-button';
import { mono, row } from '../../lib/styles';

const codeBlock = {
  ...mono,
  margin: 0,
  padding: 12,
  whiteSpace: 'pre-wrap',
  wordBreak: 'break-all',
  background: 'hsl(var(--k-muted))',
  border: '1px solid hsl(var(--k-border))',
  borderRadius: 'var(--k-radius)',
} as const;

export function Snippet({ label, value, copyLabel, icon }: { label: string; value: string; copyLabel: string; icon?: boolean }) {
  return (
    <Field>
      <div style={{ ...row, justifyContent: 'space-between' }}>
        <Label>{label}</Label>
        <CopyButton icon={icon} value={value} label={copyLabel} />
      </div>
      <pre style={codeBlock}>{value}</pre>
    </Field>
  );
}
