import { useState } from 'preact/hooks';
import { Accordion, Field, Input, Label, Select, Textarea } from 'kinu';
import type { SocialPreset } from '../../../lib/types';
import { muted } from '../../../lib/styles';
import { httpsUrl, OG_DESC_MAX, OG_TITLE_MAX, type LinkDraft } from './link-draft';

export function SocialPreview({
  id,
  draft,
  setDraft,
  presets,
  linkBase,
}: {
  id: string;
  draft: LinkDraft;
  setDraft: (d: LinkDraft) => void;
  presets: SocialPreset[];
  linkBase: string;
}) {
  const [presetName, setPresetName] = useState('');
  const applyPreset = (name: string) => {
    setPresetName(name);
    const p = presets.find((x) => x.name === name);
    if (p) setDraft({ ...draft, ogTitle: p.title ?? '', ogDescription: p.description ?? '', ogImage: p.image ?? '' });
  };
  const imageBad = !!draft.ogImage.trim() && !httpsUrl(draft.ogImage.trim());
  const filled = !!(draft.ogTitle || draft.ogDescription || draft.ogImage);

  return (
    <Accordion open={filled}>
      <summary>
        <span>
          Social preview <span style={muted}>optional</span>
        </span>
      </summary>
      <div style={{ display: 'grid', gap: 16, marginTop: 12 }}>
        {presets.length > 0 && (
          <Field>
            <Label htmlFor={`${id}-social`}>Social preview preset</Label>
            <Select id={`${id}-social`} value={presetName} onChange={(e) => applyPreset(e.currentTarget.value)}>
              <option value="">Custom</option>
              {presets.map((p) => (
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
            aria-invalid={imageBad}
            onInput={(e) => setDraft({ ...draft, ogImage: e.currentTarget.value })}
            placeholder="https://example.com/card.png"
          />
          <Field.Description>
            {imageBad ? 'Must be an absolute https URL' : 'Shown when the link is shared in chats and social apps. 1200×630 works best.'}
          </Field.Description>
        </Field>
        {filled && (
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
    </Accordion>
  );
}
