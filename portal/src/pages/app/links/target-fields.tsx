import { Field, Input, Label } from 'kinu';
import type { Link, RuleTargets } from '../../../lib/types';

// Placeholders show what an empty field falls back to.
export function TargetFields({ idp, t, link, onChange }: { idp: string; t?: RuleTargets; link: Link; onChange: (patch: RuleTargets) => void }) {
  const fields: { k: keyof RuleTargets; label: string; base: string; hint: string }[] = [
    { k: 'destination', label: 'App route', base: link.url, hint: '/promo/special' },
    { k: 'fallback_url', label: 'Web fallback', base: link.fallbackUrl, hint: 'https://example.com/landing' },
    { k: 'ios', label: 'iOS store page', base: link.ios, hint: 'https://apps.apple.com/app/id123' },
    { k: 'android', label: 'Android store page', base: link.android, hint: 'https://play.google.com/store/apps/details?id=…' },
  ];
  return (
    <div class="form-grid inherit-fields">
      {fields.map((f) => (
        <Field key={f.k}>
          <Label htmlFor={`${idp}-${f.k}`}>{f.label}</Label>
          <Input
            id={`${idp}-${f.k}`}
            placeholder={f.base || f.hint}
            value={t?.[f.k] || ''}
            onInput={(e) => onChange({ [f.k]: e.currentTarget.value })}
          />
        </Field>
      ))}
    </div>
  );
}
