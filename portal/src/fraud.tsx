import { JSX } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { Alert, Badge, Card, Empty, Input, Select, Table, toast, Toggle, ToggleGroup } from 'kinu';
import { api, FlaggedInstall, Fraud, FraudMode, FraudSettings } from './api';
import { AdminGateAborted, adminCall, AdminRequired, getAdmin, Loading, mono, muted, row, subscribeAdmin } from './ui';
import { FRAUD_RANGES, FraudNumKey, SIGNALS } from './matching';
import { fmt, Skeleton } from './analytics';

type Signal = (typeof SIGNALS)[number];

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

// Fraud tab: signal configuration, then the installs those signals flagged. AppPage remounts it on app change, so state never crosses apps.
export function FraudPanel({ appId, tick }: { appId: string; tick: number }) {
  return (
    <div style={{ display: 'grid', gap: 40 }}>
      <Signals appId={appId} tick={tick} />
      <Flagged appId={appId} tick={tick} />
    </div>
  );
}

// Every change saves on its own: a mode on press, a threshold on blur or Enter. Each save toasts; a rejected one reverts and toasts why.
// Admin-gated config surface: a viewer's load 403s — the gate routes to the password prompt (elevate in place) instead of a raw error.
function Signals({ appId, tick }: { appId: string; tick: number }) {
  const [view, setView] = useState<FraudSettings | null>(null);
  const [loadError, setLoadError] = useState('');
  const [locked, setLocked] = useState(false);
  // The viewer canceled this section's prompt: Refresh (tick) stays locked instead of
  // prompting again. Survives effect re-runs; FraudPanel remounts on app change.
  const canceled = useRef(false);

  useEffect(() => {
    let stale = false;
    const load = () => {
      if (canceled.current && !getAdmin()) return;
      canceled.current = false;
      setLoadError('');
      setLocked(false);
      adminCall(() => api.getFraudSettings(appId)).then(
        (v) => !stale && setView(v),
        (e) => {
          if (e instanceof AdminGateAborted) {
            canceled.current = true;
            !stale && setLocked(true);
          } else if (!stale) setLoadError(String(e));
        },
      );
    };
    load();
    // Elevating from the top bar while locked re-loads the section (recovery). Only
    // when locked: an in-flight load's own prompt already retries after elevation.
    const unsub = subscribeAdmin(() => {
      if (canceled.current && getAdmin() && !stale) load();
    });
    return () => {
      stale = true;
      unsub();
    };
  }, [appId, tick]);

  if (!view) {
    if (locked) {
      return (
        <section aria-label="Fraud signals" style={{ display: 'grid', gap: 16 }}>
          <div style={{ display: 'grid', gap: 4 }}>
            <h2 style={{ margin: 0 }}>Signals</h2>
          </div>
          <AdminRequired>Enter your admin password to configure signals.</AdminRequired>
        </section>
      );
    }
    return loadError ? <Alert variant="destructive">{loadError}</Alert> : <Loading />;
  }

  // Patch one field on top of the saved settings; unsaved edits elsewhere never ride along.
  const save = async (what: string, patch: Partial<FraudSettings>) => {
    const before = view;
    setView({ ...view, ...patch });
    try {
      setView(await adminCall(() => api.saveFraudSettings(appId, { ...settingsOf(before), ...patch })));
      toast.show(what);
    } catch (e) {
      setView(before);
      if (e instanceof AdminGateAborted) return;
      toast.show(String(e), { title: 'Not saved' });
    }
  };

  return (
    <section aria-label="Fraud signals" style={{ display: 'grid', gap: 16 }}>
      <div style={{ display: 'grid', gap: 4 }}>
        <h2 style={{ margin: 0 }}>Signals</h2>
        <p style={muted}>
          Tagged: label only. Active: flagged clicks get no credit. Applies from the next install.
        </p>
      </div>
      <ul class="signal-list">
        {SIGNALS.filter((s) => s.key !== 'install_ip').map((s) => (
          <SignalRow key={s.key} signal={s} view={view} onSave={save} />
        ))}
      </ul>
    </section>
  );
}

// Thresholds as one sentence with inline number fields; signals without thresholds explain themselves in the meaning line.
const RULES: Record<string, (n: (k: FraudNumKey) => JSX.Element) => JSX.Element> = {
  velocity: (n) => (
    <>
      Flag at {n('velocityIpMax')} clicks from one IP or {n('velocityLinkMax')} on one link, within {n('velocityWindowMinutes')}{' '}
      minutes.
    </>
  ),
  timing: (n) => (
    <>
      Flag opens under {n('timingShortSeconds')} seconds after the click, or over {n('timingLongHours')} hours after it (click
      ID matches only).
    </>
  ),
  fingerprint: (n) => (
    <>
      Flag at {n('fingerprintMax')} installs from one device profile on one link, within {n('fingerprintWindowDays')} days.
    </>
  ),
};

function SignalRow({
  signal: s,
  view,
  onSave,
}: {
  signal: Signal;
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
              // Controlled by state: drop kinu's own DOM flip, which un-presses the current mode on a re-press.
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

// Number input that saves when it loses focus or on Enter, only if the value changed.
function Threshold({
  k,
  label,
  saved,
  onSave,
}: {
  k: FraudNumKey;
  label: string;
  saved: number;
  onSave: (n: number) => void;
}) {
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

const signalLabel = (k: string) => SIGNALS.find((s) => s.key === k)?.label ?? k;

const fmtTime = (t: string) =>
  new Date(t).toLocaleString('en', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });

// Outcome in plain words; the flagged click's link when credit moved or was dropped.
function Outcome({ i }: { i: FlaggedInstall }) {
  const text =
    i.fraudAction === 'reattributed'
      ? 'Credit moved to another click'
      : i.fraudAction === 'excluded'
        ? 'Kept as organic'
        : i.attribution === 'non_organic'
          ? 'Attributed, tagged only'
          : 'Organic, tagged only';
  const from = i.fraudLinkKey && `Flagged click on ${i.fraudLinkKey}`; // set only with an action
  return (
    <div style={{ display: 'grid', gap: 2 }}>
      {text}
      {from && <span style={{ ...muted, fontSize: 12 }}>{from}</span>}
    </div>
  );
}

// Flagged installs for the chosen range; a superseded response is dropped (stale flag).
function Flagged({ appId, tick }: { appId: string; tick: number }) {
  const [days, setDays] = useState(7);
  const [data, setData] = useState<Fraud | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let stale = false;
    setError('');
    api.getFraud(appId, days).then(
      (f) => !stale && setData(f),
      (e) => !stale && setError(String(e)),
    );
    return () => {
      stale = true;
    };
  }, [appId, days, tick]);

  return (
    <section aria-label="Flagged installs" style={{ display: 'grid', gap: 16 }}>
      <div style={{ ...row, justifyContent: 'space-between', flexWrap: 'wrap', gap: 12 }}>
        <div style={{ display: 'grid', gap: 4 }}>
          <h2 style={{ margin: 0 }}>Flagged installs</h2>
          <p style={muted}>Installs at least one signal flagged, newest first.</p>
        </div>
        <Select aria-label="Flagged range" value={String(days)} onChange={(e) => setDays(Number(e.currentTarget.value))}>
          <option value="7">Last 7 days</option>
          <option value="30">Last 30 days</option>
          <option value="90">Last 90 days</option>
        </Select>
      </div>
      {error ? (
        <Alert variant="destructive">{error}</Alert>
      ) : !data ? (
        <Skeleton h={120} />
      ) : data.installs.length === 0 ? (
        <Card>
          <Empty>Nothing flagged in this range.</Empty>
        </Card>
      ) : (
        <>
          <dl class="signal-counts" aria-label="Flagged installs per signal">
            {SIGNALS.map((s) => (
              <div key={s.key}>
                <dt>{s.label}</dt>
                <dd>{fmt(data.signals[s.key] ?? 0)}</dd>
              </div>
            ))}
          </dl>
          <Table>
            <thead>
              <tr>
                <th>When</th>
                <th>Link</th>
                <th>Signals</th>
                <th>Outcome</th>
              </tr>
            </thead>
            <tbody>
              {data.installs.map((i) => (
                <tr key={i.id}>
                  <td style={{ whiteSpace: 'nowrap' }}>{fmtTime(i.createdAt)}</td>
                  <td data-label="Link" style={mono}>
                    {i.linkKey || '—'}
                  </td>
                  <td data-label="Signals">
                    <div style={{ ...row, flexWrap: 'wrap', gap: 4 }}>
                      {i.fraud.map((f) => (
                        <Badge key={f} variant="outline" style={{ letterSpacing: 'normal' }}>
                          {signalLabel(f)}
                        </Badge>
                      ))}
                    </div>
                  </td>
                  <td data-label="Outcome">
                    <Outcome i={i} />
                  </td>
                </tr>
              ))}
            </tbody>
          </Table>
          {data.installs.length >= 100 && <p style={muted}>Showing the latest 100 flagged installs.</p>}
        </>
      )}
    </section>
  );
}
