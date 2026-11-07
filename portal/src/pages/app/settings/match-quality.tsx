import { useEffect, useState } from 'preact/hooks';
import { Badge, Card, Field, Label, Slider } from 'kinu';
import { api } from '../../../lib/api';
import { Loading } from '../../../components/page';
import { THRESHOLD } from '../../../lib/matching';
import { muted, row } from '../../../lib/styles';
import { App, MatchQuality } from '../../../lib/types';

const METHODS: [string, string][] = [
  ['click_id', 'Click ID'],
  ['pasteboard', 'Clipboard'],
  ['probabilistic', 'Fingerprint'],
  ['prior', 'Retry'],
  ['organic', 'Organic'],
  ['', 'Before receipts'],
];
const STEP = 50;

type Bucket = MatchQuality['buckets'][number];

// every bucket across the threshold range + seen scores; empty ones as zero (one bucket alone filled the card)
function fillBuckets(buckets: Bucket[]) {
  const seen = new Map(buckets.map((b) => [b.from, b]));
  const lo = Math.min(THRESHOLD.min, ...buckets.map((b) => b.from));
  const hi = Math.max(THRESHOLD.max, ...buckets.map((b) => b.from));
  const bars: Bucket[] = [];
  for (let f = lo; f <= hi; f += STEP) bars.push(seen.get(f) ?? { from: f, matched: 0, organic: 0 });
  return bars;
}

function ScoreBars({ buckets, at }: { buckets: Bucket[]; at: number }) {
  const top = Math.max(1, ...buckets.map((b) => b.matched + b.organic));
  return (
    <div class="score-bars" role="img" aria-label="Installs per fingerprint score">
      {fillBuckets(buckets).map((b) => (
        <div key={b.from} title={`${b.from}–${b.from + STEP - 1}: ${b.matched} matched, ${b.organic} organic`}>
          <span class="score-bar organic" style={{ height: `${(b.organic / top) * 100}%` }} />
          <span class="score-bar matched" style={{ height: `${(b.matched / top) * 100}%`, opacity: b.from < at ? 0.35 : 1 }} />
          <small>{b.from}</small>
        </div>
      ))}
    </div>
  );
}

export function MatchQualityView({ app }: { app: App }) {
  const [q, setQ] = useState<MatchQuality | null>(null);
  const [at, setAt] = useState(app.matchThreshold);
  useEffect(() => {
    api.getMatchQuality(app.id).then(setQ, () => setQ({ methods: {}, buckets: [] }));
  }, [app.id]);
  useEffect(() => setAt(app.matchThreshold), [app.matchThreshold]);
  if (!q) return <Loading />;

  const lose = q.buckets.filter((b) => b.from < at).reduce((n, b) => n + b.matched, 0);
  const gain = q.buckets.filter((b) => b.from >= at).reduce((n, b) => n + b.organic, 0);
  return (
    <Card style={{ display: 'grid', gap: 16 }}>
      <div style={{ ...row, flexWrap: 'wrap' }}>
        {METHODS.filter(([k]) => q.methods[k]).map(([k, label]) => (
          <Badge key={k} variant="secondary" style={{ letterSpacing: 'normal' }}>
            {label} {q.methods[k]}
          </Badge>
        ))}
        {Object.keys(q.methods).length === 0 && <p style={muted}>No installs yet.</p>}
      </div>
      {q.buckets.length > 0 && (
        <>
          <ScoreBars buckets={q.buckets} at={at} />
          <Field>
            <Label htmlFor="whatif">
              At threshold {at}: lose {lose} matches, gain up to {gain} organic installs
            </Label>
            <Slider
              id="whatif"
              min={THRESHOLD.min}
              max={THRESHOLD.max}
              step={STEP}
              value={at}
              onInput={(e) => setAt(Number(e.currentTarget.value))}
            />
            <Field.Description>Fingerprint matches only. Click ID matches never depend on the threshold.</Field.Description>
          </Field>
        </>
      )}
    </Card>
  );
}
