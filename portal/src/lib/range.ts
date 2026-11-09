export const WINDOWS = ['15m', '1h', '3h', '12h'] as const;
export type WindowId = (typeof WINDOWS)[number];
export const isWindow = (v: unknown): v is WindowId => WINDOWS.includes(v as WindowId);

export type DateRangeFilter = {
  days?: number;
  window?: WindowId;
  linkId?: string;
  from?: string;
  to?: string;
};

export const dateRangeQuery = (r: DateRangeFilter | number): string => {
  if (typeof r === 'number') return `days=${r}`;
  const parts: string[] = [];
  if (r.window) parts.push(`window=${r.window}`);
  else if (r.from && r.to) parts.push(`from=${r.from}&to=${r.to}`);
  else parts.push(`days=${r.days ?? 7}`);
  if (r.linkId) parts.push(`link_id=${encodeURIComponent(r.linkId)}`);
  return parts.join('&');
};
