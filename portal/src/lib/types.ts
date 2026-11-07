// shapes of the Go API's JSON

export interface App {
  id: string;
  name: string;
  apiKeyHash: string;
  iosAppId: string;
  androidPackage: string;
  androidCertFingerprint: string;
  // iOS tap page tries <scheme>://<route> before the App Store
  iosScheme: string;
  matchThreshold: number;
  matchWindowMinutes: number;
  // SDK destinations get detur_link=<key> so the app can tag its device
  tagLinks: boolean;
  // reusable store/web redirect sets; a link copies one when picked
  redirectProfiles?: RedirectProfile[];
  // reusable social preview sets (title/description/image)
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
  expiresAt: string; // RFC3339 UTC; "" = never
  expiredUrl: string;
  tags?: string[]; // portal labels; server always sends []
  ruleSummary?: string; // list view: rule name + split weights, "" = no rules
  hasSplit?: boolean; // any rule uses weighted variants
}

// one UTC day. clicks = browser clicks; webFallbacks ⊂ clicks; opens = SDK opens of installed app
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
  // browser clicks per in-app source (messenger, zalo, …, unknown-inapp), all platforms
  sources: { source: string; count: number }[];
  // per link, from app-tagged devices, all platforms. Older servers omit both.
  conversions?: { linkId: string; key: string; event: string; count: number }[];
  retention?: LinkRetention[];
}

// retention mark: of devices (cohorts w/ mark day in range), returned = opened app exactly N days after tag
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

// Installs per match method ('' = pre-receipt) + 50-pt score buckets.
export interface MatchQuality {
  methods: Record<string, number>;
  buckets: { from: number; matched: number; organic: number }[];
}

// GET /api/apps/{id}/health row.
export interface HealthCheck {
  check: string;
  status: 'ok' | 'warn' | 'fail';
  detail: string;
}

export type FraudMode = 'tagged' | 'active';

// per-app fraud config (server fraud.Settings). Fingerprint: no mode, tag only.
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

// Flagged install. linkKey/fraudLinkKey '' = organic or link deleted; fraudAction '' = tagged only.
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

// Per-signal counts + latest 100 flagged installs, newest first.
export interface Fraud {
  signals: Record<string, number>;
  installs: FlaggedInstall[];
}

export type Platform = '' | 'ios' | 'android' | 'desktop';

export interface CreatedApp {
  id: string;
  name: string;
  apiKey: string;
  apiKeyHash: string;
}

// GET /api/admin/session; expiresAt only when admin
export interface AdminSession {
  admin: boolean;
  expiresAt?: string;
}

// GET /api/apps/{id}/page; html '' = default page in use
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

// data-API / MCP bearer token. prefix only after creation; lastUsedAt null = never used
export interface ApiToken {
  id: string;
  name: string;
  prefix: string;
  appIds: string[];
  createdAt: string;
  lastUsedAt: string | null;
}

// POST reply: plaintext token, shown once
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
