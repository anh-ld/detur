import { ComponentChildren } from 'preact';
import { Alert, toast } from 'kinu';
import { api } from '../../../lib/api';
import { FraudSettings } from '../../../lib/types';
import { adminCall, isGateAbort, useGatedLoad } from '../../../lib/admin';
import { SIGNALS } from '../../../lib/matching';
import { muted, stack } from '../../../lib/styles';
import { AdminRequired } from '../../../components/admin';
import { Loading } from '../../../components/page';
import { SignalRow } from './signal-row';

const settingsOf = (v: FraudSettings): FraudSettings => ({
  velocityMode: v.velocityMode,
  timingMode: v.timingMode,
  userAgentMode: v.userAgentMode,
  ipMode: v.ipMode,
  velocityIpMax: v.velocityIpMax,
  velocityLinkMax: v.velocityLinkMax,
  velocityWindowMinutes: v.velocityWindowMinutes,
  timingShortSeconds: v.timingShortSeconds,
  timingLongHours: v.timingLongHours,
  fingerprintMax: v.fingerprintMax,
  fingerprintWindowDays: v.fingerprintWindowDays,
});

const Heading = ({ children }: { children?: ComponentChildren }) => (
  <div style={{ display: 'grid', gap: 4 }}>
    <h2 style={{ margin: 0 }}>Signals</h2>
    {children}
  </div>
);

export function Signals({ appId, tick }: { appId: string; tick: number }) {
  const {
    data: view,
    setData: setView,
    locked,
    error: loadError,
  } = useGatedLoad<FraudSettings>({ load: () => api.getFraudSettings(appId), deps: [appId, tick], mode: 'when-set' });

  if (!view) {
    if (locked) {
      return (
        <section aria-label="Fraud signals" style={stack}>
          <Heading />
          <AdminRequired>Enter your admin password to configure signals.</AdminRequired>
        </section>
      );
    }
    return loadError ? <Alert variant="destructive">{loadError}</Alert> : <Loading />;
  }

  const save = async (what: string, patch: Partial<FraudSettings>) => {
    const before = view;
    setView({ ...view, ...patch });
    try {
      setView(await adminCall(() => api.saveFraudSettings(appId, { ...settingsOf(before), ...patch })));
      toast.show(what);
    } catch (e) {
      setView(before);
      if (isGateAbort(e)) return;
      toast.show(String(e), { title: 'Not saved' });
    }
  };

  return (
    <section aria-label="Fraud signals" style={stack}>
      <Heading>
        <p style={muted}>Tagged: label only. Active: flagged clicks get no credit. Applies from the next install.</p>
      </Heading>
      <ul class="signal-list">
        {SIGNALS.filter((s) => s.key !== 'install_ip').map((s) => (
          <SignalRow key={s.key} signal={s} view={view} onSave={save} />
        ))}
      </ul>
    </section>
  );
}
