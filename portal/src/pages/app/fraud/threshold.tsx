import { useEffect, useState } from 'preact/hooks';
import { Input } from 'kinu';
import { FRAUD_RANGES, FraudNumKey } from '../../../lib/matching';

// saves on blur/Enter, only if changed
export function Threshold({ k, label, saved, onSave }: { k: FraudNumKey; label: string; saved: number; onSave: (n: number) => void }) {
  const [v, setV] = useState(String(saved));
  useEffect(() => setV(String(saved)), [saved]);
  const r = FRAUD_RANGES[k];
  const commit = () => {
    if (v.trim() === '' || Number(v) === saved) return setV(String(saved));
    onSave(Number(v));
  };
  return (
    <Input
      id={`fraud-${k}`}
      class="inline-num"
      type="number"
      inputMode="numeric"
      aria-label={label}
      min={r.min}
      max={r.max}
      value={v}
      title={`${label}: ${r.min}–${r.max}`}
      onInput={(e) => setV(e.currentTarget.value)}
      onBlur={commit}
      onKeyDown={(e) => e.key === 'Enter' && e.currentTarget.blur()}
    />
  );
}
