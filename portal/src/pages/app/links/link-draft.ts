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
  expiresAt: '', // datetime-local value, read as UTC
  expiredUrl: '',
};
export type LinkDraft = typeof BLANK;
const FIELDS = Object.keys(BLANK) as (keyof LinkDraft)[];

// social preview limits: match the server's
export const OG_TITLE_MAX = 100;
export const OG_DESC_MAX = 300;
export const httpsUrl = (s: string) => /^https:\/\/[^/\s]+/i.test(s);

// RFC3339 UTC <-> datetime-local (UTC wall time, no zone shift)
export const toLocalInput = (iso?: string) => (iso ? iso.slice(0, 16) : '');
export const fromLocalInput = (v: string) => (v ? `${v}:00Z` : '');

export const emptyDraft = (): LinkDraft => ({ ...BLANK, tags: [] });

// link values over the defaults; only expiresAt changes shape
export const draftFrom = (l: Link): LinkDraft => {
  const d = emptyDraft() as Record<string, unknown>;
  for (const k of FIELDS) d[k] = (l as unknown as Record<string, unknown>)[k] ?? d[k];
  return { ...(d as LinkDraft), expiresAt: toLocalInput(l.expiresAt) };
};

// wire payload shared by create and edit (key is create-only: create adds the trimmed key)
export const linkPayload = (draft: LinkDraft) => {
  const { key: _key, ...rest } = draft;
  return { ...rest, url: draft.url.trim(), title: draft.title.trim(), expiresAt: fromLocalInput(draft.expiresAt) };
};

// first problem with the draft, or '' when it can be saved
export const draftError = (d: LinkDraft, mode: 'create' | 'edit') => {
  if (mode === 'create' && (!d.key.trim() || !d.url.trim())) return 'Key and destination are required';
  if (mode === 'edit' && !d.url.trim()) return 'Destination is required';
  if (d.ogImage.trim() && !httpsUrl(d.ogImage.trim())) return 'Social preview image must be an absolute https URL';
  return '';
};

export const draftDirty = (a: LinkDraft, b: LinkDraft) =>
  FIELDS.some((k) => (k === 'tags' ? a.tags.length !== b.tags.length || a.tags.some((t, i) => t !== b.tags[i]) : a[k] !== b[k]));
