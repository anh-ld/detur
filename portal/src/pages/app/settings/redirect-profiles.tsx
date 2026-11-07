import { App, RedirectProfile } from '../../../lib/types';
import { PresetForm, TextField } from './preset-form';

export function RedirectProfilesForm({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  return (
    <PresetForm<RedirectProfile>
      app={app}
      onSaved={onSaved}
      intro="Save the store and web URLs you reuse. A link copies a preset when picked; editing it here later does not change links already created."
      noun="redirect preset"
      read={(a) => a.redirectProfiles}
      patch={(redirectProfiles) => ({ redirectProfiles })}
      blank={{ name: '', ios: '', android: '', web: '' }}
      fields={(p, set, i) => (
        <>
          <TextField
            id={`prof-name-${i}`}
            label="Redirect preset name"
            value={p.name}
            placeholder="Production"
            onInput={(e) => set({ name: e.currentTarget.value })}
          />
          <TextField
            id={`prof-ios-${i}`}
            label="Preset iOS store"
            value={p.ios ?? ''}
            placeholder="https://apps.apple.com/app/id123"
            onInput={(e) => set({ ios: e.currentTarget.value })}
          />
          <TextField
            id={`prof-android-${i}`}
            label="Preset Android store"
            value={p.android ?? ''}
            placeholder="https://play.google.com/store/apps/details?id=com.example"
            onInput={(e) => set({ android: e.currentTarget.value })}
          />
          <TextField
            id={`prof-web-${i}`}
            label="Preset web fallback"
            value={p.web ?? ''}
            placeholder="https://example.com"
            onInput={(e) => set({ web: e.currentTarget.value })}
          />
        </>
      )}
    />
  );
}
