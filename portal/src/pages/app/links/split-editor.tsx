import { Button, Field, Input, Label } from 'kinu';
import type { Link, SplitVariant } from '../../../lib/types';
import { Icon, IconButton, ICONS } from '../../../components/icons';
import { muted } from '../../../lib/styles';
import { SplitBar, variantColor } from './rule-summary';
import { TargetFields } from './target-fields';

export const defaultVariants = (): SplitVariant[] => [
  { name: 'Variant A', weight: 50, targets: {} },
  { name: 'Variant B', weight: 50, targets: {} },
];

function weightMessage(total: number) {
  if (total === 100) return 'Weights add up to 100%';
  return total < 100 ? `${100 - total}% unassigned, weights must add up to 100%` : `${total - 100}% over, weights must add up to 100%`;
}

export function SplitEditor({
  idp,
  link,
  variants,
  onChange,
}: {
  idp: string;
  link: Link;
  variants: SplitVariant[];
  onChange: (variants: SplitVariant[]) => void;
}) {
  const total = variants.reduce((acc, v) => acc + (Number(v.weight) || 0), 0);
  const update = (i: number, patch: Partial<SplitVariant>) => onChange(variants.map((v, j) => (j === i ? { ...v, ...patch } : v)));

  return (
    <div style={{ display: 'grid', gap: 12 }}>
      <SplitBar variants={variants} />
      <span class={total === 100 ? 'weight-ok' : 'weight-bad'} role="status">
        {weightMessage(total)}
      </span>
      <p style={muted}>Each variant's empty fields keep the link's own value, shown faded.</p>
      {variants.map((v, i) => (
        <div key={i} class="variant">
          <div class="variant-head">
            <span class="variant-dot" style={{ background: variantColor(i) }} aria-hidden="true" />
            <Field style={{ flex: 1 }}>
              <Label htmlFor={`${idp}-v-${i}-name`}>Variant name</Label>
              <Input id={`${idp}-v-${i}-name`} value={v.name} onInput={(e) => update(i, { name: e.currentTarget.value })} />
            </Field>
            <Field style={{ width: 110 }}>
              <Label htmlFor={`${idp}-v-${i}-weight`}>Weight</Label>
              <div class="suffix-input">
                <Input
                  id={`${idp}-v-${i}-weight`}
                  type="number"
                  min="1"
                  max="100"
                  value={v.weight}
                  onInput={(e) => update(i, { weight: parseInt(e.currentTarget.value) || 0 })}
                />
                <span aria-hidden="true">%</span>
              </div>
            </Field>
            {variants.length > 2 && (
              <IconButton icon={ICONS.close} label={`Remove ${v.name || 'variant'}`} onClick={() => onChange(variants.filter((_, j) => j !== i))} />
            )}
          </div>
          <TargetFields idp={`${idp}-v-${i}`} t={v.targets} link={link} onChange={(patch) => update(i, { targets: { ...v.targets, ...patch } })} />
        </div>
      ))}
      <Button
        type="button"
        size="sm"
        variant="ghost"
        style={{ justifySelf: 'start' }}
        onClick={() =>
          onChange([
            ...variants,
            { name: `Variant ${String.fromCharCode(65 + variants.length)}`, weight: Math.max(0, 100 - total), targets: {} },
          ])
        }
      >
        <Icon d={ICONS.plus} />
        Add variant
      </Button>
    </div>
  );
}
