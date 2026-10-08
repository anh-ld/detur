// Ranges mirrored from server/internal/match/match.go (ValidateSettings).
export const THRESHOLD = { min: 700, max: 1200 };
export const WINDOW = { min: 5, max: 180 };

export function inRange(v: number, range: { min: number; max: number }): boolean {
  return v >= range.min && v <= range.max;
}

// Fraud threshold ranges mirrored from server/internal/fraud/fraud.go (Settings.Validate).
export const FRAUD_RANGES = {
  velocityIpMax: { min: 2, max: 10000 },
  velocityLinkMax: { min: 2, max: 100000 },
  velocityWindowMinutes: { min: 5, max: 1440 },
  timingShortSeconds: { min: 1, max: 300 },
  timingLongHours: { min: 1, max: 720 },
  fingerprintMax: { min: 2, max: 1000 },
  fingerprintWindowDays: { min: 1, max: 30 },
};

export type FraudNumKey = keyof typeof FRAUD_RANGES;
export type FraudModeKey = 'velocityMode' | 'timingMode' | 'userAgentMode' | 'ipMode';

// Fraud signals in server label order: install label key, plain label, one-line meaning, mode field (none = tag only), thresholds.
export const SIGNALS: {
  key: string;
  label: string;
  meaning: string;
  mode?: FraudModeKey;
  thresholds: { key: FraudNumKey; label: string }[];
}[] = [
  {
    key: 'velocity',
    label: 'Click flooding',
    meaning: 'A burst of clicks from one IP address or on one link.',
    mode: 'velocityMode',
    thresholds: [
      { key: 'velocityIpMax', label: 'Max clicks per IP' },
      { key: 'velocityLinkMax', label: 'Max clicks per link' },
      { key: 'velocityWindowMinutes', label: 'Window (minutes)' },
    ],
  },
  {
    key: 'timing',
    label: 'Suspicious timing',
    meaning: 'The app opened implausibly fast after the click (injection), or very late (flooding).',
    mode: 'timingMode',
    thresholds: [
      { key: 'timingShortSeconds', label: 'Too soon (seconds)' },
      { key: 'timingLongHours', label: 'Too late (hours)' },
    ],
  },
  {
    key: 'user_agent',
    label: 'Bot traffic',
    meaning: 'The click came from a known bot, script or headless browser.',
    mode: 'userAgentMode',
    thresholds: [],
  },
  {
    key: 'ip',
    label: 'Datacenter IP',
    meaning: 'The click came from a hosting or proxy network, not a home or mobile one.',
    mode: 'ipMode',
    thresholds: [],
  },
  {
    key: 'install_ip',
    label: 'Datacenter install IP',
    meaning: 'The app opened from a hosting or proxy network. Never changes attribution.',
    thresholds: [],
  },
  {
    key: 'fingerprint',
    label: 'Repeated device',
    meaning: 'Many installs from the same device profile on one link.',
    thresholds: [
      { key: 'fingerprintMax', label: 'Max installs' },
      { key: 'fingerprintWindowDays', label: 'Window (days)' },
    ],
  },
];
