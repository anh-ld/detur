import { JSX } from 'preact';
import { Toggle, ToggleGroup } from 'kinu';
import { FraudMode, FraudSettings } from '../../../lib/types';
import { FraudNumKey, SIGNALS } from '../../../lib/matching';
import { fmt } from '../../../lib/format';
import { muted } from '../../../lib/styles';
import { Threshold } from './threshold';

// thresholds = one sentence w/ inline number fields; threshold-less signals explain in the meaning line
const RULES: Record<string, (n: (k: FraudNumKey) => JSX.Element) => JSX.Element> = {
  velocity: (n) => (
    <>
      Flag at {n('velocityIpMax')} clicks from one IP or {n('velocityLinkMax')} on one link, within {n('velocityWindowMinutes')} minutes.
    </>
  ),
  timing: (n) => (
    <>
      Flag opens under {n('timingShortSeconds')} seconds after the click, or over {n('timingLongHours')} hours after it (click ID matches
      only).
    </>
  ),
  fingerprint: (n) => (
    <>
      Flag at {n('fingerprintMax')} installs from one device profile on one link, within {n('fingerprintWindowDays')} days.
    </>
  ),
};

export function SignalRow({
  signal: s,
  view,
  onSave,
}: {
  signal: (typeof SIGNALS)[number];
  view: FraudSettings;
  onSave: (what: string, p: Partial<FraudSettings>) => void;
}) {
  const mode: FraudMode | null = s.mode ? view[s.mode] : null;
  const rule = RULES[s.key];
  const num = (k: FraudNumKey) => {
    const t = s.thresholds.find((x) => x.key === k)!;
    return (
      <Threshold
        k={k}
        label={t.label}
        saved={view[k]}
        onSave={(v) => onSave(`${s.label}: ${t.label.toLowerCase()} set to ${fmt(v)}`, { [k]: v })}
      />
    );
  };
  return (
    <li class="signal-row" aria-label={s.label}>
      <div class="signal-text">
        <h3>{s.label}</h3>
        <p style={muted}>{s.meaning}</p>
        {rule && <p class="signal-rule">{rule(num)}</p>}
        {s.key === 'ip' && <p style={muted}>Installs opened from a datacenter IP are labeled too, but never excluded.</p>}
      </div>
      {s.mode ? (
        <ToggleGroup role="group" aria-label={`${s.label} mode`} class="mode-toggle">
          {(['tagged', 'active'] as const).map((m) => (
            <Toggle
              key={m}
              type="button"
              size="sm"
              data-mode={m}
              pressed={mode === m}
              // state-controlled: drop kinu's DOM flip (un-presses current mode on re-press)
              onClickCapture={() => {}}
              onClick={() => mode !== m && onSave(`${s.label}: ${m}`, { [s.mode!]: m })}
            >
              {m === 'tagged' ? 'Tagged' : 'Active'}
            </Toggle>
          ))}
        </ToggleGroup>
      ) : (
        <span class="tag-only">Tag only</span>
      )}
    </li>
  );
}
