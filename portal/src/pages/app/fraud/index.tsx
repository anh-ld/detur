import { Flagged } from './flagged';
import { Signals } from './signals';

// AppPage remounts on app change -> no state crosses apps
export function FraudPanel({ appId, tick }: { appId: string; tick: number }) {
  return (
    <div style={{ display: 'grid', gap: 40 }}>
      <Signals appId={appId} tick={tick} />
      <Flagged appId={appId} tick={tick} />
    </div>
  );
}
