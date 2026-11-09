import type { LinkRule, RuleTargets } from '../../../lib/types';

export type ActionKind = 'direct' | 'split';

export const blankRule = (): LinkRule => ({ name: '', cond: {}, action: { targets: {} } });

export const actionKind = (r: LinkRule): ActionKind => (r.action.split?.length ? 'split' : 'direct');

const cleanTargets = (t?: RuleTargets): RuleTargets => ({
  destination: t?.destination?.trim() || undefined,
  ios: t?.ios?.trim() || undefined,
  android: t?.android?.trim() || undefined,
  fallback_url: t?.fallback_url?.trim() || undefined,
});

function cleanRule(draft: LinkRule, kind: ActionKind): { rule: LinkRule } | { error: string } {
  const name = draft.name.trim();
  if (!name) return { error: 'Rule name is required' };

  const { platform, from, until } = draft.cond;
  if (from && until && new Date(until) < new Date(from)) return { error: 'Active window end must be after start' };
  const lang = draft.cond.lang?.trim();
  const paramKey = draft.cond.param_key?.trim();
  const paramVal = paramKey ? draft.cond.param_val?.trim() : undefined;
  const cond: LinkRule['cond'] = {};
  if (platform) cond.platform = platform;
  if (from) cond.from = from;
  if (until) cond.until = until;
  if (lang) cond.lang = lang;
  if (paramKey) cond.param_key = paramKey;
  if (paramVal) cond.param_val = paramVal;

  const rule: LinkRule = { ...draft, name, cond, action: {} };
  if (kind === 'direct') {
    rule.action.targets = cleanTargets(draft.action.targets);
    return { rule };
  }

  const variants = draft.action.split || [];
  if (variants.length < 2) return { error: 'A/B Split requires at least 2 variants' };
  let sum = 0;
  for (const v of variants) {
    if (!v.name.trim()) return { error: 'All variants must have a name' };
    const w = Number(v.weight);
    if (!Number.isInteger(w) || w < 1 || w > 100) return { error: `Variant "${v.name.trim()}" weight must be 1–100%` };
    sum += w;
  }
  if (sum !== 100) return { error: `Variant weights must sum to 100% (currently ${sum}%)` };
  rule.action.split = variants.map((v) => ({ name: v.name.trim(), weight: Number(v.weight), targets: cleanTargets(v.targets) }));
  return { rule };
}

export function commitRule(rules: LinkRule[], index: number, draft: LinkRule, kind: ActionKind): { rules: LinkRule[] } | { error: string } {
  const cleaned = cleanRule(draft, kind);
  if ('error' in cleaned) return cleaned;
  const next = [...rules];
  if (index === -1) next.push(cleaned.rule);
  else next[index] = cleaned.rule;
  const labels = next.flatMap((r) => (r.action.split?.length ? r.action.split.map((v) => v.name) : [r.name]));
  const dup = labels.find((l, i) => labels.indexOf(l) !== i);
  if (dup) return { error: `"${dup}" is already used by another rule or variant on this link` };
  return { rules: next };
}
