import { Field, Label, Textarea } from 'kinu';
import { App, SocialPreset } from '../../../lib/types';
import { PresetForm, TextField } from './preset-form';

export function SocialPresetsForm({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  return (
    <PresetForm<SocialPreset>
      app={app}
      onSaved={onSaved}
      intro="Save the chat and social preview text you reuse. A link copies a preset when picked; editing it here later does not change links already created."
      noun="social preset"
      read={(a) => a.socialPresets}
      patch={(socialPresets) => ({ socialPresets })}
      blank={{ name: '', title: '', description: '', image: '' }}
      fields={(p, set, i) => (
        <>
          <TextField
            id={`social-name-${i}`}
            label="Social preset name"
            value={p.name}
            placeholder="Launch"
            onInput={(e) => set({ name: e.currentTarget.value })}
          />
          <TextField
            id={`social-title-${i}`}
            label="Preview title"
            value={p.title ?? ''}
            maxLength={100}
            placeholder="Sam invited you"
            onInput={(e) => set({ title: e.currentTarget.value })}
          />
          <TextField
            id={`social-image-${i}`}
            label="Preview image URL"
            value={p.image ?? ''}
            placeholder="https://example.com/card.png"
            onInput={(e) => set({ image: e.currentTarget.value })}
          />
          <Field style={{ gridColumn: '1 / -1' }}>
            <Label htmlFor={`social-desc-${i}`}>Preview description</Label>
            <Textarea
              id={`social-desc-${i}`}
              value={p.description ?? ''}
              maxLength={300}
              rows={2}
              onInput={(e) => set({ description: e.currentTarget.value })}
            />
          </Field>
        </>
      )}
    />
  );
}
