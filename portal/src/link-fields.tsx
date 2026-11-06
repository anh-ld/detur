import { useState } from 'preact/hooks';
import { Combobox, ComboboxInput, ComboboxList, ComboboxOption, Field, Input, InputGroup, Label, Select, Separator, Textarea } from 'kinu';
import { Link, RedirectProfile, SocialPreset } from './api';
import { mono, muted } from './ui';

export interface LinkDraft {
  key: string;
  url: string;
  title: string;
  tags: string[];
  ios: string;
  android: string;
  fallbackUrl: string;
  ogTitle: string;
  ogDescription: string;
  ogImage: string;
  expiresAt: string; // datetime-local value, read as UTC
  expiredUrl: string;
}

// social preview limits: match the server's
export const OG_TITLE_MAX = 100;
export const OG_DESC_MAX = 300;
export const httpsUrl = (s: string) => /^https:\/\/[^/\s]+/i.test(s);

// RFC3339 UTC <-> datetime-local (UTC wall time, no zone shift)
export const toLocalInput = (iso?: string) => (iso ? iso.slice(0, 16) : '');
export const fromLocalInput = (v: string) => (v ? `${v}:00Z` : '');

export const emptyDraft = (): LinkDraft => ({
  key: '',
  url: '',
  title: '',
  tags: [],
  ios: '',
  android: '',
  fallbackUrl: '',
  ogTitle: '',
  ogDescription: '',
  ogImage: '',
  expiresAt: '',
  expiredUrl: '',
});

export const draftFrom = (l: Link): LinkDraft => ({
  key: l.key,
  url: l.url,
  title: l.title ?? '',
  tags: l.tags ?? [],
  ios: l.ios,
  android: l.android,
  fallbackUrl: l.fallbackUrl,
  ogTitle: l.ogTitle ?? '',
  ogDescription: l.ogDescription ?? '',
  ogImage: l.ogImage ?? '',
  expiresAt: toLocalInput(l.expiresAt ?? ''),
  expiredUrl: l.expiredUrl ?? '',
});

// wire payload shared by create and edit (key is create-only)
export const linkPayload = (draft: LinkDraft) => ({
  url: draft.url.trim(),
  title: draft.title.trim(),
  tags: draft.tags,
  ios: draft.ios,
  android: draft.android,
  fallbackUrl: draft.fallbackUrl,
  ogTitle: draft.ogTitle,
  ogDescription: draft.ogDescription,
  ogImage: draft.ogImage,
  expiresAt: fromLocalInput(draft.expiresAt),
  expiredUrl: draft.expiredUrl,
});

// Everything about where the link sends people; rules sit below this in the same editor.
export function LinkFields({
  id,
  draft,
  setDraft,
  existingTags,
  profiles = [],
  socialPresets = [],
  linkBase,
  showKey,
}: {
  id: string;
  draft: LinkDraft;
  setDraft: (d: LinkDraft) => void;
  existingTags: string[];
  profiles?: RedirectProfile[];
  socialPresets?: SocialPreset[];
  linkBase: string;
  showKey?: boolean;
}) {
  const [tagText, setTagText] = useState('');
  const [profileName, setProfileName] = useState('');
  const [socialName, setSocialName] = useState('');
  const applySocial = (name: string) => {
    setSocialName(name);
    const p = socialPresets.find((x) => x.name === name);
    if (p) setDraft({ ...draft, ogTitle: p.title ?? '', ogDescription: p.description ?? '', ogImage: p.image ?? '' });
  };
  // profiles are templates: picking one copies its URLs into this link
  const applyProfile = (name: string) => {
    setProfileName(name);
    const p = profiles.find((x) => x.name === name);
    if (p) setDraft({ ...draft, ios: p.ios ?? '', android: p.android ?? '', fallbackUrl: p.web ?? '' });
  };
  const ogImageBad = !!draft.ogImage.trim() && !httpsUrl(draft.ogImage.trim());
  const addTag = () => {
    const t = tagText.trim();
    if (!t) return;
    if (!draft.tags.some((x) => x.toLowerCase() === t.toLowerCase())) setDraft({ ...draft, tags: [...draft.tags, t] });
    setTagText('');
  };

  return (
    <>
      {showKey && (
        <Field>
          <Label htmlFor={`${id}-key`}>Short link</Label>
          <InputGroup class="prefix-group">
            {linkBase && <legend>{linkBase.replace(/^https?:\/\//, '')}/</legend>}
            <Input
              id={`${id}-key`}
              style={mono}
              value={draft.key}
              onInput={(e) => setDraft({ ...draft, key: e.currentTarget.value })}
              placeholder="summer-sale"
            />
          </InputGroup>
          <Field.Description>The path people tap. Can't be changed later.</Field.Description>
        </Field>
      )}
      <Field>
        <Label htmlFor={`${id}-url`}>Destination</Label>
        <Input
          id={`${id}-url`}
          value={draft.url}
          onInput={(e) => setDraft({ ...draft, url: e.currentTarget.value })}
          placeholder="/product/42?ref=promo"
        />
        <Field.Description>App route the app opens, e.g. /product/42. Web visitors use the web fallback.</Field.Description>
      </Field>
      <Field>
        <Label htmlFor={`${id}-title`}>
          Title / Note <span style={muted}>optional</span>
        </Label>
        <Input
          id={`${id}-title`}
          value={draft.title}
          maxLength={100}
          onInput={(e) => setDraft({ ...draft, title: e.currentTarget.value })}
          placeholder="e.g. John Doe promotional link"
        />
        <Field.Description>Optional note to identify which member or campaign this link belongs to.</Field.Description>
      </Field>
      <Field>
        <Label htmlFor={`${id}-tags`}>
          Tags <span style={muted}>optional</span>
        </Label>
        <div class="tag-box">
          {draft.tags.map((t) => (
            <span class="tag-pill" key={t}>
              {t}
              <button
                type="button"
                aria-label={`Remove ${t}`}
                onClick={() => setDraft({ ...draft, tags: draft.tags.filter((x) => x !== t) })}
              >
                ×
              </button>
            </span>
          ))}
          <Combobox>
            <ComboboxInput
              id={`${id}-tags`}
              value={tagText}
              onInput={(e) => setTagText(e.currentTarget.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault(); // don't submit the form
                  addTag();
                } else if (e.key === 'Backspace' && !tagText && draft.tags.length) {
                  setDraft({ ...draft, tags: draft.tags.slice(0, -1) });
                }
              }}
              placeholder={draft.tags.length ? '' : 'Type or pick a tag, press Enter'}
            />
            <ComboboxList>
              {existingTags
                .filter((t) => !draft.tags.some((x) => x.toLowerCase() === t.toLowerCase()))
                .map((t) => (
                  <ComboboxOption
                    key={t}
                    onClick={(e) => {
                      e.preventDefault();
                      e.stopPropagation(); // add straight away instead of refilling the input
                      setDraft({ ...draft, tags: [...draft.tags, t] });
                      setTagText('');
                      (e.currentTarget.closest('[k="combobox-list"]') as HTMLDialogElement | null)?.close();
                    }}
                  >
                    {t}
                  </ComboboxOption>
                ))}
            </ComboboxList>
          </Combobox>
        </div>
        <Field.Description>Press Enter to add. The Links list filters by tag.</Field.Description>
      </Field>

      <Separator />
      <div class="form-section">
        <h3>Store and web redirects</h3>
        <p style={muted}>Optional. Where people without the app land.</p>
      </div>
      {profiles.length > 0 && (
        <Field>
          <Label htmlFor={`${id}-profile`}>Store and web redirect preset</Label>
          <Select id={`${id}-profile`} value={profileName} onChange={(e) => applyProfile(e.currentTarget.value)}>
            <option value="">Custom</option>
            {profiles.map((p) => (
              <option key={p.name} value={p.name}>
                {p.name}
              </option>
            ))}
          </Select>
          <Field.Description>Copies a preset from Settings. Edit any field below to keep it custom.</Field.Description>
        </Field>
      )}
      <Field>
        <Label htmlFor={`${id}-ios`}>iOS store page</Label>
        <Input
          id={`${id}-ios`}
          value={draft.ios}
          onInput={(e) => setDraft({ ...draft, ios: e.currentTarget.value })}
          placeholder="https://apps.apple.com/app/id123"
        />
      </Field>
      <Field>
        <Label htmlFor={`${id}-android`}>Android store page</Label>
        <Input
          id={`${id}-android`}
          value={draft.android}
          onInput={(e) => setDraft({ ...draft, android: e.currentTarget.value })}
          placeholder="https://play.google.com/store/apps/details?id=com.example"
        />
      </Field>
      <Field>
        <Label htmlFor={`${id}-fb`}>Web fallback</Label>
        <Input
          id={`${id}-fb`}
          value={draft.fallbackUrl}
          onInput={(e) => setDraft({ ...draft, fallbackUrl: e.currentTarget.value })}
          placeholder="https://example.com"
        />
        <Field.Description>Desktop visitors, and phones with no store page above for their platform.</Field.Description>
      </Field>

      <details open={!!(draft.expiresAt || draft.expiredUrl)}>
        <summary>
          Expiry <span style={muted}>optional</span>
        </summary>
        <div style={{ display: 'grid', gap: 16, marginTop: 12 }}>
          <Field>
            <Label htmlFor={`${id}-exp`}>Expires at (UTC)</Label>
            <Input
              id={`${id}-exp`}
              type="datetime-local"
              value={draft.expiresAt}
              onInput={(e) => setDraft({ ...draft, expiresAt: e.currentTarget.value })}
            />
            <Field.Description>After this, the link goes to the expired URL, or shows "link expired".</Field.Description>
          </Field>
          <Field>
            <Label htmlFor={`${id}-expurl`}>Expired URL</Label>
            <Input
              id={`${id}-expurl`}
              value={draft.expiredUrl}
              onInput={(e) => setDraft({ ...draft, expiredUrl: e.currentTarget.value })}
              placeholder="https://example.com/offer-ended"
            />
          </Field>
        </div>
      </details>

      <details open={!!(draft.ogTitle || draft.ogDescription || draft.ogImage)}>
        <summary>
          Social preview <span style={muted}>optional</span>
        </summary>
        <div style={{ display: 'grid', gap: 16, marginTop: 12 }}>
          {socialPresets.length > 0 && (
            <Field>
              <Label htmlFor={`${id}-social`}>Social preview preset</Label>
              <Select id={`${id}-social`} value={socialName} onChange={(e) => applySocial(e.currentTarget.value)}>
                <option value="">Custom</option>
                {socialPresets.map((p) => (
                  <option key={p.name} value={p.name}>
                    {p.name}
                  </option>
                ))}
              </Select>
              <Field.Description>Copies a preset from Settings. Edit any field below to keep it custom.</Field.Description>
            </Field>
          )}
          <Field>
            <Label htmlFor={`${id}-ogt`}>Title</Label>
            <Input
              id={`${id}-ogt`}
              value={draft.ogTitle}
              maxLength={OG_TITLE_MAX}
              onInput={(e) => setDraft({ ...draft, ogTitle: e.currentTarget.value })}
              placeholder="Sam invited you"
            />
            <Field.Description>
              {draft.ogTitle.length}/{OG_TITLE_MAX}
            </Field.Description>
          </Field>
          <Field>
            <Label htmlFor={`${id}-ogd`}>Description</Label>
            <Textarea
              id={`${id}-ogd`}
              value={draft.ogDescription}
              maxLength={OG_DESC_MAX}
              rows={2}
              onInput={(e) => setDraft({ ...draft, ogDescription: e.currentTarget.value })}
            />
            <Field.Description>
              {draft.ogDescription.length}/{OG_DESC_MAX}
            </Field.Description>
          </Field>
          <Field>
            <Label htmlFor={`${id}-ogi`}>Image URL</Label>
            <Input
              id={`${id}-ogi`}
              value={draft.ogImage}
              aria-invalid={ogImageBad}
              onInput={(e) => setDraft({ ...draft, ogImage: e.currentTarget.value })}
              placeholder="https://example.com/card.png"
            />
            <Field.Description>
              {ogImageBad
                ? 'Must be an absolute https URL'
                : 'Shown when the link is shared in chats and social apps. 1200×630 works best.'}
            </Field.Description>
          </Field>
          {(draft.ogTitle || draft.ogDescription || draft.ogImage) && (
            <figure class="og-preview">
              <figcaption style={muted}>Preview</figcaption>
              <div class="og-card">
                {httpsUrl(draft.ogImage.trim()) && <img src={draft.ogImage.trim()} alt="" />}
                <div>
                  <small>{linkBase.replace(/^https?:\/\//, '') || 'detur'}</small>
                  <strong>{draft.ogTitle || draft.key || 'Untitled'}</strong>
                  {draft.ogDescription && <p>{draft.ogDescription}</p>}
                </div>
              </div>
            </figure>
          )}
        </div>
      </details>
    </>
  );
}
