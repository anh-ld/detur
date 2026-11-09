import { Button, Empty } from 'kinu';
import type { Link, LinkRule } from '../../../lib/types';
import { IconButton, ICONS } from '../../../components/icons';
import { muted } from '../../../lib/styles';
import { condLabels, SplitBar, TargetSummary } from './rule-summary';

export function RuleList({
  rules,
  link,
  busy,
  canAdd,
  onAdd,
  onEdit,
  onMove,
  onDelete,
}: {
  rules: LinkRule[];
  link: Link;
  busy: boolean;
  canAdd: boolean;
  onAdd: () => void;
  onEdit: (idx: number) => void;
  onMove: (idx: number, dir: -1 | 1) => void;
  onDelete: (idx: number) => void;
}) {
  if (rules.length === 0) {
    return (
      <Empty>
        <h3>No rules yet</h3>
        <p style={muted}>
          Every visitor gets the link's own targets. Add a rule to route by platform, language, query or date, or to run an A/B split.
        </p>
        <Button variant="outline" disabled={!canAdd} onClick={onAdd}>
          Add rule
        </Button>
      </Empty>
    );
  }
  return (
    <div style={{ display: 'grid', gap: 8 }}>
      <span style={muted}>Checked top to bottom. The first rule that matches wins.</span>
      <ol class="rule-list">
        {rules.map((r, idx) => {
          const conds = condLabels(r.cond);
          return (
            <li key={r.id || idx} class="rule-row">
              <span class="rule-num">{idx + 1}</span>
              <div class="rule-body">
                <strong>{r.name}</strong>
                <div class="rule-line">
                  <span class="rule-kw">If</span>
                  {conds.length ? (
                    conds.map((c) => (
                      <span key={c} class="cond">
                        {c}
                      </span>
                    ))
                  ) : (
                    <span style={muted}>any visitor</span>
                  )}
                </div>
                <div class="rule-line">
                  <span class="rule-kw">Then</span>
                  {r.action.split?.length ? <SplitBar variants={r.action.split} compact /> : <TargetSummary t={r.action.targets} />}
                </div>
              </div>
              <div class="rule-actions">
                <IconButton icon={ICONS.up} label="Move up" disabled={busy || idx === 0} onClick={() => onMove(idx, -1)} />
                <IconButton icon={ICONS.down} label="Move down" disabled={busy || idx === rules.length - 1} onClick={() => onMove(idx, 1)} />
                <IconButton icon={ICONS.edit} label="Edit" disabled={busy} onClick={() => onEdit(idx)} />
                <IconButton icon={ICONS.trash} label="Delete" danger disabled={busy} onClick={() => onDelete(idx)} />
              </div>
            </li>
          );
        })}
        <li class="rule-row rule-default">
          <span class="rule-num" aria-hidden="true" />
          <div class="rule-body">
            <div class="rule-line">
              <span class="rule-kw">Else</span>
              <span style={muted}>link default</span>
              <code class="target">{link.url}</code>
            </div>
          </div>
        </li>
      </ol>
    </div>
  );
}
