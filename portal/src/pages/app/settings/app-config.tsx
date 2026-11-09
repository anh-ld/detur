import { JSX } from 'preact';
import { useState } from 'preact/hooks';
import { Button, Input, Label, Table } from 'kinu';
import { api } from '../../../lib/api';
import { adminCall } from '../../../lib/admin';
import { mono, muted, stack, end } from '../../../lib/styles';
import { App } from '../../../lib/types';
import { useSave } from './save';

const pick = (a: App) => ({
  iosAppId: a.iosAppId,
  androidPackage: a.androidPackage,
  androidCertFingerprint: a.androidCertFingerprint,
  iosScheme: a.iosScheme,
});

export function AppConfigTable({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  const [draft, setDraft] = useState(() => pick(app));
  const { onSubmit, status } = useSave(async () => {
    const a = await adminCall(() => api.updateApp(app.id, draft));
    setDraft(pick(a));
    onSaved(a);
  });

  const field = (id: string, key: keyof typeof draft, placeholder: string, style?: JSX.CSSProperties) => (
    <Input id={id} value={draft[key]} placeholder={placeholder} style={style} onInput={(e) => setDraft({ ...draft, [key]: e.currentTarget.value })} />
  );
  const line = (id: string, label: string, input: JSX.Element, format: string) => (
    <tr>
      <td>
        <Label htmlFor={id}>{label}</Label>
      </td>
      <td>{input}</td>
      <td data-label="Format" style={muted}>
        {format}
      </td>
    </tr>
  );

  return (
    <form style={stack} onSubmit={onSubmit}>
      <Table>
        <thead>
          <tr>
            <th>Setting</th>
            <th>Value</th>
            <th>Format</th>
          </tr>
        </thead>
        <tbody>
          {line('cfg-ios', 'iOS App ID', field('cfg-ios', 'iosAppId', 'ABCDE12345.com.example.app'), 'TEAMID.BUNDLEID')}
          {line('cfg-scheme', 'iOS URL scheme', field('cfg-scheme', 'iosScheme', 'myapp'), 'Optional. In-app browsers open the installed app first')}
          {line('cfg-android', 'Android package', field('cfg-android', 'androidPackage', 'com.example.app'), 'Application ID')}
          {line('cfg-cert', 'Android cert fingerprint', field('cfg-cert', 'androidCertFingerprint', 'AA:BB:CC:…', mono), 'SHA-256 of the signing certificate')}
        </tbody>
      </Table>
      {status}
      <div style={end}>
        <Button type="submit">Save</Button>
      </div>
    </form>
  );
}
