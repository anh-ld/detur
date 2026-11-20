import type {
  Analytics,
  ApiToken,
  App,
  AppPage,
  DayStat,
  Fraud,
  FraudSettings,
  HealthCheck,
  Link,
  LinkRule,
  RedirectProfile,
  MatchQuality,
  VariantStat,
  Webhook,
} from "../lib/types";
import { isWindow, type WindowId } from "../lib/range";

let seed = 0;

const seededRandom = () => {
  seed = (seed + 0x6d2b79f5) | 0;
  let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
  t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;

  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
};

const int = (lo: number, hi: number) =>
  lo + Math.floor(seededRandom() * (hi - lo + 1));
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();
const DAY = 86_400_000;

const app = (
  id: string,
  name: string,
  bundle: string,
): App & { profile: Required<RedirectProfile> } => {
  const profile = {
    name: "Stores",
    ios: "https://apps.apple.com/app/id1234567890",
    android: `https://play.google.com/store/apps/details?id=${bundle}`,
    web: "https://example.com",
  };

  return {
    id,
    name,
    apiKeyHash: "demo",
    iosAppId: `ABCDE12345.${bundle}`,
    androidPackage: bundle,
    androidCertFingerprint: Array.from({ length: 32 }, (_, i) =>
      (i * 7 + 16).toString(16).toUpperCase().slice(-2),
    ).join(":"),
    iosScheme: bundle.split(".").pop()!,
    matchThreshold: 850,
    matchWindowMinutes: 15,
    tagLinks: true,
    redirectProfiles: [profile],
    socialPresets: [
      {
        name: "Default card",
        title: `${name} — get the app`,
        description: "Open in the app for the best experience.",
        image: "https://picsum.photos/seed/detur/1200/630",
      },
    ],
    profile,
  };
};

const APP_SEEDS = [
  app("demo-shop", "Acme Shop", "com.acme.shop"),
  app("demo-news", "Acme News", "com.acme.news"),
];
const APPS: App[] = APP_SEEDS.map(({ profile, ...a }) => a);

const LINK_SEEDS: [string, string, string, string[]][] = [
  ["summer", "/sale/summer", "Summer sale", ["campaign", "tiktok"]],
  ["invite", "/invite", "Referral invite", ["referral"]],
  ["p42", "/product/42", "Sneaker drop", ["product"]],
  ["news", "/newsletter", "Newsletter footer", ["email"]],
  ["ig-bio", "/home", "Instagram bio", ["social"]],
  ["bf24", "/sale/black-friday", "Black Friday", ["campaign"]],
  ["qr-store", "/stores", "In-store QR", ["offline"]],
  ["zalo", "/promo/zalo", "Zalo OA post", ["social", "zalo"]],
];

const LINKS: Link[] = APP_SEEDS.flatMap((a, ai) =>
  LINK_SEEDS.slice(ai * 2).map(([key, url, title, tags], i) => ({
    id: `${a.id}-l${i}`,
    appId: a.id,
    key: ai ? `${key}-n` : key,
    url,
    title,
    ios: a.profile.ios,
    android: a.profile.android,
    fallbackUrl: "https://example.com" + url,
    ogTitle: i === 0 ? title : "",
    ogDescription: i === 0 ? "Up to 50% off, app only." : "",
    ogImage: i === 0 ? "https://picsum.photos/seed/detur/1200/630" : "",
    expiresAt: "",
    expiredUrl: "",
    tags,
    ruleSummary: i === 0 ? "2 rules · 70/30 split" : "",
    hasSplit: i === 0,
  })),
);

const RULES: LinkRule[] = [
  {
    id: "r1",
    name: "TikTok traffic",
    cond: { param_key: "ref", param_val: "tiktok" },
    action: { targets: { destination: "/sale/summer?src=tiktok" } },
  },
  {
    id: "r2",
    name: "Landing A/B",
    cond: {},
    action: {
      split: [
        { name: "A", weight: 70, targets: {} },
        { name: "B", weight: 30, targets: { destination: "/sale/summer-v2" } },
      ],
    },
  },
];

const day = (d: Date) => d.toISOString().slice(0, 10);

const point = (key: string, scale: number): DayStat => {
  const clicks = Math.round(int(80, 160) * scale);
  const nonOrganic = Math.round(clicks * (0.25 + seededRandom() * 0.15));

  return {
    day: key,
    clicks,
    webFallbacks: Math.round(clicks * 0.12),
    opens: Math.round(clicks * 0.3),
    organic: Math.round(nonOrganic * (0.6 + seededRandom() * 0.4)),
    nonOrganic,
  };
};

const WINDOW_BUCKETS: Record<WindowId, [number, number]> = {
  "15m": [15, 1],
  "1h": [12, 5],
  "3h": [18, 10],
  "12h": [24, 30],
};

function analytics(appId: string, q: URLSearchParams): Analytics {
  const linkId = q.get("link_id");
  const from = q.get("from");
  const to = q.get("to");
  seed = appId.length * 1000 + (linkId?.length ?? 0);
  const scale =
    (appId === "demo-shop" ? 1 : 0.4) *
    (linkId ? 0.2 : 1) *
    (q.get("platform") ? 0.5 : 1);
  const win = q.get("window");
  let days: DayStat[];

  if (isWindow(win)) {
    const [n, mins] = WINDOW_BUCKETS[win];
    const step = mins * 60_000;
    const end = Math.floor(Date.now() / step) * step;

    days = Array.from({ length: n }, (_, i) =>
      point(
        new Date(end - (n - 1 - i) * step).toISOString().replace(".000", ""),
        (scale * mins) / 60,
      ),
    );
  } else {
    const end = to ? new Date(to + "T00:00:00Z") : new Date();

    const n = from
      ? Math.round(
          (end.getTime() - new Date(from + "T00:00:00Z").getTime()) / DAY,
        ) + 1
      : Number(q.get("days") ?? 7);

    days = Array.from({ length: n }, (_, i) =>
      point(
        day(new Date(end.getTime() - (n - 1 - i) * DAY)),
        scale * (1 + Math.sin(i / 3) * 0.3),
      ),
    );
  }

  const links = LINKS.filter(
    (l) => l.appId === appId && (!linkId || l.id === linkId),
  );
  const total = days.reduce((s, d) => s + d.clicks, 0);
  const mark = (devices: number, r: number) => ({
    devices,
    returned: Math.round(devices * r),
  });
  const top = win ? [] : links.slice(0, 3);

  return {
    days,
    links: links.map((l, i) => {
      const clicks = Math.round(total / (i + 1.5));

      return {
        linkId: l.id,
        key: l.key,
        title: l.title,
        clicks,
        matches: Math.round(clicks * 0.31),
      };
    }),
    events:
      win && linkId
        ? []
        : [
            { event: "purchase", count: Math.round(total * 0.08) },
            { event: "signup", count: Math.round(total * 0.15) },
            { event: "add_to_cart", count: Math.round(total * 0.22) },
          ],
    sources: [
      { source: "TikTok", count: Math.round(total * 0.34) },
      { source: "Instagram", count: Math.round(total * 0.21) },
      { source: "Zalo", count: Math.round(total * 0.12) },
      { source: "Messenger", count: Math.round(total * 0.08) },
    ],
    conversions: top.map((l, i) => ({
      linkId: l.id,
      key: l.key,
      event: "purchase",
      count: Math.round((total * 0.05) / (i + 1)),
    })),
    retention: top.map((l, i) => {
      const devices = 400 - i * 90;

      return {
        linkId: l.id,
        key: l.key,
        devices,
        d1: mark(devices, 0.42),
        d7: mark(devices, 0.21),
        d30: mark(devices, 0.09),
      };
    }),
  };
}

const fraudSettings: FraudSettings = {
  velocityMode: "tagged",
  timingMode: "tagged",
  userAgentMode: "active",
  ipMode: "tagged",
  velocityIpMax: 20,
  velocityLinkMax: 500,
  velocityWindowMinutes: 10,
  timingShortSeconds: 10,
  timingLongHours: 72,
  fingerprintMax: 3,
  fingerprintWindowDays: 7,
};

const fraud = (): Fraud => ({
  signals: {
    velocity: 37,
    timing: 12,
    user_agent: 58,
    ip: 21,
    install_ip: 4,
    fingerprint: 9,
  },
  installs: Array.from({ length: 8 }, (_, i) => ({
    id: `f${i}`,
    createdAt: ago(i * 5 * 3_600_000),
    attribution: i % 3 ? "non_organic" : "organic",
    method: i % 3 ? "probabilistic" : "organic",
    linkKey: i % 3 ? LINK_SEEDS[i % 4][0] : "",
    platform: i % 2 ? "android" : "ios",
    fraud: [["velocity"], ["timing"], ["ip", "velocity"], ["user_agent"]][
      i % 4
    ],
    fraudAction: (["", "reattributed", "excluded"] as const)[i % 3],
    fraudLinkKey: i % 3 === 1 ? LINK_SEEDS[i % 4][0] : "",
  })),
});

const matchQuality: MatchQuality = {
  methods: {
    click_id: 412,
    pasteboard: 238,
    probabilistic: 655,
    prior: 31,
    organic: 1890,
  },
  buckets: Array.from({ length: 19 }, (_, i) => {
    const from = 500 + i * 50;

    return {
      from,
      matched: from >= 850 ? 40 + ((i * 37) % 90) : 0,
      organic: from < 850 ? 30 + ((i * 53) % 70) : 0,
    };
  }),
};

const health = (a: App): HealthCheck[] => [
  {
    check: "SDK",
    status: "ok",
    detail: `react-native 2.3.1, last call ${ago(120_000).slice(0, 16).replace("T", " ")} UTC`,
  },
  { check: "iOS app ID", status: "ok", detail: a.iosAppId },
  { check: "iOS URL scheme", status: "ok", detail: a.iosScheme + "://" },
  { check: "Android", status: "ok", detail: a.androidPackage },
  {
    check: "AASA refresh",
    status: "warn",
    detail:
      "1 link(s) from the last 48h may open the browser until Apple refreshes: /summer",
  },
  {
    check: "Proxy",
    status: "ok",
    detail: "Cloudflare → detur, forwarded IPs trusted.",
  },
];

const WEBHOOK_SEEDS: [
  id: string,
  url: string,
  types: Webhook["types"],
  fails: number,
][] = [
  ["w1", "https://hooks.example.com/detur", ["installs", "events"], 0],
  ["w2", "https://crm.example.com/webhooks/installs", ["installs"], 3],
];

const webhooks = (appId: string): Webhook[] =>
  WEBHOOK_SEEDS.map(([id, url, types, fails]) => ({
    id: `${appId}-${id}`,
    appId,
    url,
    secret: "whsec_demo",
    types,
    cursorInstalls: 18_420,
    cursorClicks: 0,
    cursorEvents: 9_311,
    failsInstalls: fails,
    failsClicks: 0,
    failsEvents: 0,
    backoffInstalls: fails ? ago(-300_000) : undefined,
    replayUntilInstalls: 0,
    replayUntilClicks: 0,
    replayUntilEvents: 0,
    enabled: true,
    createdAt: ago(30 * DAY),
  }));

const tokens: ApiToken[] = [
  {
    id: "t1",
    name: "Claude analytics",
    prefix: "dtr_a1b2",
    appIds: APPS.map((a) => a.id),
    createdAt: ago(12 * DAY),
    lastUsedAt: ago(3_600_000),
  },
];

const variants: VariantStat[] = [
  { variant: "A", clicks: 1840, installs: 571, conversion_rate: 0.31 },
  { variant: "B", clicks: 790, installs: 284, conversion_rate: 0.36 },
];

const PAGE_HTML = `<main style="font-family:system-ui;text-align:center;padding:48px 24px">
  <img src="{{icon_url}}" width="96" height="96" style="border-radius:22px" alt="">
  <h1>{{app_name}}</h1>
  <p>Tap to continue in the app.</p>
  {{cta}}
</main>`;

const page: AppPage = {
  html: "",
  defaultHtml: PAGE_HTML,
  placeholders: [
    { name: "cta", desc: "Get-the-app button (required)" },
    { name: "icon_url", desc: "Store icon" },
    { name: "app_name", desc: "App name" },
    { name: "web_link", desc: "Continue on web URL" },
  ],
};

type Handler = (m: RegExpMatchArray, q: URLSearchParams) => unknown;

const GET: [RegExp, Handler][] = [
  [
    /^\/api\/config$/,
    () => ({
      logoutUrl: "",
      adminSet: true,
      linkBase: "https://go.acme.example",
      version: "demo",
    }),
  ],
  [
    /^\/api\/admin\/session$/,
    () => ({ admin: true, expiresAt: ago(-12 * 3_600_000) }),
  ],
  [/^\/api\/apps$/, () => APPS],
  [/^\/api\/apps\/([^/]+)$/, (m) => APPS.find((a) => a.id === m[1])],
  [
    /^\/api\/apps\/([^/]+)\/links$/,
    (m) => LINKS.filter((l) => l.appId === m[1]),
  ],
  [
    /^\/api\/apps\/[^/]+\/links\/([^/]+)\/rules$/,
    (m) => (m[1].endsWith("-l0") ? RULES : []),
  ],
  [
    /^\/api\/apps\/[^/]+\/links\/([^/]+)\/variants$/,
    (m) => (m[1].endsWith("-l0") ? variants : []),
  ],
  [/^\/api\/apps\/([^/]+)\/analytics$/, (m, q) => analytics(m[1], q)],
  [/^\/api\/apps\/[^/]+\/fraud$/, () => fraud()],
  [/^\/api\/apps\/[^/]+\/fraud\/settings$/, () => fraudSettings],
  [/^\/api\/apps\/[^/]+\/match-quality$/, () => matchQuality],
  [
    /^\/api\/apps\/([^/]+)\/health$/,
    (m) => health(APPS.find((a) => a.id === m[1]) ?? APPS[0]),
  ],
  [/^\/api\/apps\/[^/]+\/page$/, () => page],
  [/^\/api\/apps\/([^/]+)\/webhooks$/, (m) => webhooks(m[1])],
  [/^\/api\/tokens$/, () => tokens],
];

const READ_ONLY = "Demo: changes are not saved";

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });

const realFetch = window.fetch.bind(window);

window.fetch = async (input, init) => {
  const url = new URL(
    typeof input === "string"
      ? input
      : input instanceof URL
        ? input.href
        : input.url,
    location.href,
  );
  if (!url.pathname.startsWith("/api/")) return realFetch(input, init);
  const method = (init?.method ?? "GET").toUpperCase();

  if (url.pathname.endsWith("/page/preview")) {
    return new Response(
      PAGE_HTML.replace("{{cta}}", "<button>Get the app</button>")
        .replace("{{app_name}}", "Acme Shop")
        .replace("{{icon_url}}", ""),
      { headers: { "Content-Type": "text/html" } },
    );
  }

  if (url.pathname === "/api/admin/session" && method === "DELETE")
    return json({ admin: false });
  if (method !== "GET") return new Response(READ_ONLY, { status: 400 });

  for (const [re, h] of GET) {
    const m = url.pathname.match(re);

    if (m) {
      const body = h(m, url.searchParams);

      return body === undefined
        ? new Response("Not found", { status: 404 })
        : json(body);
    }
  }

  return new Response("Not found", { status: 404 });
};

const bar = document.createElement("div");
bar.textContent = "Demo · all data is fake, nothing is saved";
bar.setAttribute("role", "status");

bar.style.cssText =
  "position:sticky;top:0;z-index:1000;padding:6px 16px;text-align:center;font:500 13px system-ui,sans-serif;background:hsl(var(--k-accent));color:hsl(var(--k-accent-foreground));border-bottom:1px solid hsl(var(--k-border))";

document.body.prepend(bar);
