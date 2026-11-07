import { useState } from 'preact/hooks';
import { Accordion, Field, Input, InputGroup, Label, Select, Separator } from 'kinu';
import { RedirectProfile, SocialPreset } from '../../../lib/types';
import { mono, muted } from '../../../lib/styles';
import type { LinkDraft } from './link-draft';
import { SocialPreview } from './social-preview';
import { TagInput } from './tag-input';

// Where the link sends people; rules sit below this in the same editor.
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
  const [profileName, setProfileName] = useState('');
  // profiles are templates: picking one copies its URLs into this link
  const applyProfile = (name: string) => {
    setProfileName(name);
    const p = profiles.find((x) => x.name === name);
    if (p) setDraft({ ...draft, ios: p.ios ?? '', android: p.android ?? '', fallbackUrl: p.web ?? '' });
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
      <TagInput id={id} tags={draft.tags} existing={existingTags} onChange={(tags) => setDraft({ ...draft, tags })} />

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

      <Accordion open={!!(draft.expiresAt || draft.expiredUrl)}>
        <summary>
          <span>
            Expiry <span style={muted}>optional</span>
          </span>
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
      </Accordion>

      <SocialPreview id={id} draft={draft} setDraft={setDraft} presets={socialPresets} linkBase={linkBase} />
    </>
  );
}
