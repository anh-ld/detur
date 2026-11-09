export interface App {
  id: string;
  name: string;
  apiKeyHash: string;
  iosAppId: string;
  androidPackage: string;
  androidCertFingerprint: string;
  iosScheme: string;
  matchThreshold: number;
  matchWindowMinutes: number;
  tagLinks: boolean;
  redirectProfiles?: RedirectProfile[];
  socialPresets?: SocialPreset[];
}

export interface Link {
  id: string;
  appId: string;
  key: string;
  url: string;
  title: string;
  ios: string;
  android: string;
  fallbackUrl: string;
  ogTitle: string;
  ogDescription: string;
  ogImage: string;
  expiresAt: string;
  expiredUrl: string;
  tags?: string[];
  ruleSummary?: string;
  hasSplit?: boolean;
}

export interface DayStat {
  day: string;
  clicks: number;
  webFallbacks: number;
  opens: number;
  organic: number;
  nonOrganic: number;
}

export interface Analytics {
  days: DayStat[];
  links: { linkId: string; key: string; title?: string; clicks: number; matches: number }[];
  events: { event: string; count: number }[];
  sources: { source: string; count: number }[];
  conversions?: { linkId: string; key: string; event: string; count: number }[];
  retention?: LinkRetention[];
}

export interface Mark {
  returned: number;
  devices: number;
}

export interface LinkRetention {
  linkId: string;
  key: string;
  devices: number;
  d1: Mark;
  d7: Mark;
  d30: Mark;
}

export interface MatchQuality {
  methods: Record<string, number>;
  buckets: { from: number; matched: number; organic: number }[];
}

export interface HealthCheck {
  check: string;
  status: 'ok' | 'warn' | 'fail';
  detail: string;
}

export type FraudMode = 'tagged' | 'active';

export interface FraudSettings {
  velocityMode: FraudMode;
  timingMode: FraudMode;
  userAgentMode: FraudMode;
  ipMode: FraudMode;
  velocityIpMax: number;
  velocityLinkMax: number;
  velocityWindowMinutes: number;
  timingShortSeconds: number;
  timingLongHours: number;
  fingerprintMax: number;
  fingerprintWindowDays: number;
}

export interface FlaggedInstall {
  id: string;
  createdAt: string;
  attribution: 'organic' | 'non_organic' | 'unknown';
  method: string;
  linkKey: string;
  platform: string;
  fraud: string[];
  fraudAction: '' | 'reattributed' | 'excluded';
  fraudLinkKey: string;
}

export interface Fraud {
  signals: Record<string, number>;
  installs: FlaggedInstall[];
}

export const PLATFORMS = [
  { id: 'ios', label: 'iOS' },
  { id: 'android', label: 'Android' },
  { id: 'desktop', label: 'Desktop' },
] as const;
export type Platform = '' | (typeof PLATFORMS)[number]['id'];

export interface CreatedApp {
  id: string;
  name: string;
  apiKey: string;
  apiKeyHash: string;
}

export interface AdminSession {
  admin: boolean;
  expiresAt?: string;
}

export interface AppPage {
  html: string;
  defaultHtml: string;
  placeholders: { name: string; desc: string }[];
}

export interface PagePreview {
  html: string;
  page: 'copy' | 'tap';
  platform: 'ios' | 'android';
}

export interface ApiToken {
  id: string;
  name: string;
  prefix: string;
  appIds: string[];
  createdAt: string;
  lastUsedAt: string | null;
}

export interface CreatedToken extends ApiToken {
  token: string;
}

export type WebhookEventType = 'installs' | 'events' | 'clicks';

export interface Webhook {
  id: string;
  appId: string;
  url: string;
  secret: string;
  types: WebhookEventType[];
  cursorInstalls: number;
  cursorClicks: number;
  cursorEvents: number;
  failsInstalls: number;
  failsClicks: number;
  failsEvents: number;
  backoffInstalls?: string;
  backoffClicks?: string;
  backoffEvents?: string;
  replayUntilInstalls: number;
  replayUntilClicks: number;
  replayUntilEvents: number;
  enabled: boolean;
  createdAt: string;
}

export interface RuleCond {
  platform?: Platform;
  lang?: string;
  param_key?: string;
  param_val?: string;
  from?: string;
  until?: string;
}

export interface RuleTargets {
  destination?: string;
  ios?: string;
  android?: string;
  fallback_url?: string;
}

export interface SocialPreset {
  name: string;
  title?: string;
  description?: string;
  image?: string;
}

export interface RedirectProfile {
  name: string;
  ios?: string;
  android?: string;
  web?: string;
}

export interface SplitVariant {
  name: string;
  weight: number;
  targets: RuleTargets;
}

export interface RuleAction {
  targets?: RuleTargets;
  split?: SplitVariant[];
}

export interface LinkRule {
  id?: string;
  link_id?: string;
  position?: number;
  name: string;
  cond: RuleCond;
  action: RuleAction;
  created_at?: string;
}

export interface VariantStat {
  variant: string;
  clicks: number;
  installs: number;
  conversion_rate: number;
}
