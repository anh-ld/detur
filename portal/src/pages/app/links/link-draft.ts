import type { Link } from '../../../lib/types';

const BLANK = {
  key: '',
  url: '',
  title: '',
  tags: [] as string[],
  ios: '',
  android: '',
  fallbackUrl: '',
  ogTitle: '',
  ogDescription: '',
  ogImage: '',
  expiresAt: '',
  expiredUrl: '',
};
export type LinkDraft = typeof BLANK;
const FIELDS = Object.keys(BLANK) as (keyof LinkDraft)[];

export const OG_TITLE_MAX = 100;
export const OG_DESC_MAX = 300;
export const httpsUrl = (s: string) => /^https:\/\/[^/\s]+/i.test(s);

export const toLocalInput = (iso?: string) => (iso ? iso.slice(0, 16) : '');
export const fromLocalInput = (v: string) => (v ? `${v}:00Z` : '');

export const emptyDraft = (): LinkDraft => ({ ...BLANK, tags: [] });

export const draftFrom = (l: Link): LinkDraft => {
  const d = emptyDraft() as Record<string, unknown>;
  for (const k of FIELDS) d[k] = (l as unknown as Record<string, unknown>)[k] ?? d[k];
  return { ...(d as LinkDraft), expiresAt: toLocalInput(l.expiresAt) };
};

export const linkPayload = (draft: LinkDraft) => {
  const { key: _key, ...rest } = draft;
  return { ...rest, url: draft.url.trim(), title: draft.title.trim(), expiresAt: fromLocalInput(draft.expiresAt) };
};

export const ogImageBad = (d: LinkDraft) => !!d.ogImage.trim() && !httpsUrl(d.ogImage.trim());

export const draftError = (d: LinkDraft, mode: 'create' | 'edit') => {
  if (mode === 'create' && (!d.key.trim() || !d.url.trim())) return 'Key and destination are required';
  if (mode === 'edit' && !d.url.trim()) return 'Destination is required';
  if (ogImageBad(d)) return 'Social preview image must be an absolute https URL';
  return '';
};

export const draftDirty = (a: LinkDraft, b: LinkDraft) =>
  FIELDS.some((k) => (k === 'tags' ? a.tags.length !== b.tags.length || a.tags.some((t, i) => t !== b.tags[i]) : a[k] !== b[k]));
