import type { LinkRule, RuleTargets, SplitVariant } from '../../../lib/types';
import { muted } from '../../../lib/styles';
import { toLocalInput } from './link-draft';

const VARIANT_COLORS = ['205 87% 59%', '205 90% 34%', '187 72% 42%', '35 92% 55%', '262 55% 62%', '340 70% 60%'];
export const variantColor = (i: number) => `hsl(${VARIANT_COLORS[i % VARIANT_COLORS.length]})`;

const PLATFORM_LABEL: Record<string, string> = { ios: 'iOS', android: 'Android', desktop: 'Desktop' };
const fmtUtc = (iso: string) => toLocalInput(iso).replace('T', ' ');

export function condLabels(c: LinkRule['cond']): string[] {
  const out: string[] = [];
  if (c.platform) out.push(PLATFORM_LABEL[c.platform] ?? c.platform);
  if (c.lang) out.push(`language ${c.lang}*`);
  if (c.param_key) out.push(c.param_val ? `?${c.param_key}=${c.param_val}` : `has ?${c.param_key}`);
  if (c.from && c.until) out.push(`${fmtUtc(c.from)} → ${fmtUtc(c.until)} UTC`);
  else if (c.from) out.push(`from ${fmtUtc(c.from)} UTC`);
  else if (c.until) out.push(`until ${fmtUtc(c.until)} UTC`);
  return out;
}

// What a direct rule overrides; nothing set means the link default.
export function TargetSummary({ t }: { t?: RuleTargets }) {
  const parts = [
    t?.destination && <code class="target">{t.destination}</code>,
    t?.ios && <span class="cond">iOS store</span>,
    t?.android && <span class="cond">Android store</span>,
    t?.fallback_url && <span class="cond">desktop page</span>,
  ].filter(Boolean);
  return parts.length ? <>{parts}</> : <span style={muted}>link default</span>;
}

// Stacked weight bar and legend; compact (list row) drops the bar's room.
export function SplitBar({ variants, compact }: { variants: SplitVariant[]; compact?: boolean }) {
  return (
    <div class={compact ? 'split split-compact' : 'split'}>
      <div class="split-bar" aria-hidden="true">
        {variants.map((v, i) => (
          <span key={i} style={{ flexGrow: Math.max(0, Number(v.weight) || 0), background: variantColor(i) }} />
        ))}
      </div>
      <div class="split-legend">
        {variants.map((v, i) => (
          <span key={i}>
            <i style={{ background: variantColor(i) }} />
            {v.name || 'Unnamed'} <b>{Number(v.weight) || 0}%</b>
          </span>
        ))}
      </div>
    </div>
  );
}
