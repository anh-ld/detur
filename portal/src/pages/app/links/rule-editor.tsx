import { useState } from 'preact/hooks';
import { Button, Field, Input, Label, Select, Toggle, ToggleGroup } from 'kinu';
import { PLATFORMS, type Link, type LinkRule, type Platform } from '../../../lib/types';
import { muted, row } from '../../../lib/styles';
import { fromLocalInput, toLocalInput } from './link-draft';
import { actionKind, commitRule, type ActionKind } from './rule-draft';
import { defaultVariants, SplitEditor } from './split-editor';
import { TargetFields } from './target-fields';

export function RuleEditor({
  idp,
  link,
  rules,
  index,
  initial,
  setErr,
  onDone,
  onCancel,
}: {
  idp: string;
  link: Link;
  rules: LinkRule[];
  index: number;
  initial: LinkRule;
  setErr: (msg: string) => void;
  onDone: (rules: LinkRule[]) => void;
  onCancel: () => void;
}) {
  const [rule, setRule] = useState<LinkRule>(() => structuredClone(initial));
  const [kind, setKind] = useState<ActionKind>(() => actionKind(initial));
  const fid = (s: string) => `${idp}-${s}`;
  const setCond = (patch: Partial<LinkRule['cond']>) => setRule({ ...rule, cond: { ...rule.cond, ...patch } });
  const setAction = (patch: LinkRule['action']) => setRule({ ...rule, action: { ...rule.action, ...patch } });

  const pickSplit = () => {
    setKind('split');
    if (!rule.action.split?.length) setAction({ split: defaultVariants() });
  };
  const done = () => {
    setErr('');
    const res = commitRule(rules, index, rule, kind);
    if ('error' in res) setErr(res.error);
    else onDone(res.rules);
  };

  return (
    <>
      <div class="rule-editor">
        <h3>{index === -1 ? 'New rule' : `Edit rule ${index + 1}`}</h3>
        <Field>
          <Label htmlFor={fid('rule-name')}>Rule name</Label>
          <Input
            id={fid('rule-name')}
            placeholder="e.g. German iOS campaign"
            value={rule.name}
            onInput={(e) => setRule({ ...rule, name: e.currentTarget.value })}
          />
        </Field>

        <section class="form-section">
          <h4>When</h4>
          <p style={muted}>Every filled condition must match. Leave all empty to match everyone.</p>
          <div class="form-grid">
            <Field>
              <Label htmlFor={fid('platform')}>Platform</Label>
              <Select
                id={fid('platform')}
                value={rule.cond.platform || ''}
                onChange={(e) => setCond({ platform: (e.currentTarget.value as Platform) || undefined })}
              >
                <option value="">Any platform</option>
                {PLATFORMS.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.label}
                  </option>
                ))}
              </Select>
            </Field>
            <Field>
              <Label htmlFor={fid('lang')}>Language starts with</Label>
              <Input
                id={fid('lang')}
                placeholder="e.g. de, fr, en-US"
                value={rule.cond.lang || ''}
                onInput={(e) => setCond({ lang: e.currentTarget.value || undefined })}
              />
            </Field>
            <Field>
              <Label htmlFor={fid('param-key')}>Query param</Label>
              <Input
                id={fid('param-key')}
                placeholder="e.g. utm_campaign"
                value={rule.cond.param_key || ''}
                onInput={(e) => setCond({ param_key: e.currentTarget.value || undefined })}
              />
            </Field>
            <Field>
              <Label htmlFor={fid('param-val')}>Equals</Label>
              <Input
                id={fid('param-val')}
                placeholder="Any value"
                disabled={!rule.cond.param_key}
                value={rule.cond.param_val || ''}
                onInput={(e) => setCond({ param_val: e.currentTarget.value || undefined })}
              />
            </Field>
            <Field>
              <Label htmlFor={fid('from')}>Active from (UTC)</Label>
              <Input
                id={fid('from')}
                type="datetime-local"
                value={toLocalInput(rule.cond.from)}
                onInput={(e) => setCond({ from: fromLocalInput(e.currentTarget.value) || undefined })}
              />
            </Field>
            <Field>
              <Label htmlFor={fid('until')}>Active until (UTC)</Label>
              <Input
                id={fid('until')}
                type="datetime-local"
                value={toLocalInput(rule.cond.until)}
                onInput={(e) => setCond({ until: fromLocalInput(e.currentTarget.value) || undefined })}
              />
            </Field>
          </div>
        </section>

        <section class="form-section">
          <div style={{ ...row, justifyContent: 'space-between', flexWrap: 'wrap', gap: 12 }}>
            <h4>Then</h4>
            <ToggleGroup role="group" aria-label="Action" class="seg">
              <Toggle type="button" size="sm" pressed={kind === 'direct'} onClick={() => setKind('direct')}>
                Send to
              </Toggle>
              <Toggle type="button" size="sm" pressed={kind === 'split'} onClick={pickSplit}>
                A/B split
              </Toggle>
            </ToggleGroup>
          </div>
          {kind === 'direct' ? (
            <>
              <p style={muted}>Empty fields keep the link's own value, shown faded.</p>
              <TargetFields
                idp={fid('direct')}
                t={rule.action.targets}
                link={link}
                onChange={(patch) => setAction({ targets: { ...rule.action.targets, ...patch } })}
              />
            </>
          ) : (
            <SplitEditor idp={idp} link={link} variants={rule.action.split || []} onChange={(split) => setAction({ split })} />
          )}
        </section>
      </div>
      <div class="dialog-actions">
        <Button variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button onClick={done}>Done</Button>
      </div>
    </>
  );
}
