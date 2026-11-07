import { DayStat } from '../../../lib/types';

export type Metric = keyof Omit<DayStat, 'day'>;
export type Series = { key: Metric; label: string; color: string }[];

// blue ramp (theme --chart-*) + one warm gray
const BLUE = 'hsl(var(--chart-1))';

export const LINES: Series = [
  { key: 'clicks', label: 'Clicks', color: BLUE },
  { key: 'nonOrganic', label: 'Installs via link', color: 'hsl(var(--chart-2))' },
  { key: 'webFallbacks', label: 'Web fallbacks', color: 'hsl(var(--chart-3))' },
  { key: 'opens', label: 'Already installed opens', color: 'hsl(240 4% 62%)' },
];

export const BARS: Series = [
  { key: 'nonOrganic', label: 'Non-organic', color: BLUE },
  { key: 'organic', label: 'Organic', color: 'hsl(var(--chart-wash))' },
];

// server ua.InApp ids → display names
export const SOURCE_NAMES: Record<string, string> = {
  messenger: 'Messenger',
  facebook: 'Facebook',
  instagram: 'Instagram',
  threads: 'Threads',
  zalo: 'Zalo',
  tiktok: 'TikTok',
  linkedin: 'LinkedIn',
  snapchat: 'Snapchat',
  line: 'LINE',
  x: 'X',
  telegram: 'Telegram',
  wechat: 'WeChat',
  'unknown-inapp': 'Unknown in-app',
};

// Lucide icon paths (ISC), 24px grid
export const ICONS: Partial<Record<Metric, string>> = {
  clicks:
    'M14 4.1 12 6M5.1 8l-2.9-.8M6 12l-1.9 2M7.2 2.2 8 5.1M9.037 9.69a.498.498 0 0 1 .653-.653l11 4.5a.5.5 0 0 1-.074.949l-4.349 1.041a1 1 0 0 0-.74.739l-1.04 4.35a.5.5 0 0 1-.95.074z',
  opens: 'M7 2h10a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2zM12 18h.01',
  nonOrganic: 'M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3',
  webFallbacks: 'M12 2a10 10 0 1 0 0 20a10 10 0 1 0 0-20M12 2a14.5 14.5 0 0 0 0 20a14.5 14.5 0 0 0 0-20M2 12h20',
};
