import { useState } from 'preact/hooks';
import { Label, Switch, toast } from 'kinu';
import { api } from '../../../lib/api';
import { adminCall, isGateAbort } from '../../../lib/admin';
import { CopyButton } from '../../../components/copy-button';
import { muted, row } from '../../../lib/styles';
import { App } from '../../../lib/types';

const SNIPPET = "DetourAnalytics.logEvent('detur_link', { link: link.params.detur_link })";

export function TagLinksRow({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  const [on, setOn] = useState(app.tagLinks);
  const [saving, setSaving] = useState(false);
  const save = async (next: boolean) => {
    setOn(next);
    setSaving(true);
    try {
      const updated = await adminCall(() => api.saveTagging(app.id, next));
      setOn(updated.tagLinks);
      onSaved(updated);
      toast.show(`Tag deferred links: ${updated.tagLinks ? 'on' : 'off'}`);
    } catch (e) {
      setOn(!next);
      if (isGateAbort(e)) return;
      toast.show(String(e), { title: 'Not saved' });
    } finally {
      setSaving(false);
    }
  };
  return (
    <div style={{ ...row, justifyContent: 'space-between', alignItems: 'flex-start', gap: 16, marginTop: 24 }}>
      <div style={{ display: 'grid', gap: 4 }}>
        <Label htmlFor="set-tag">Tag deferred links</Label>
        <p style={muted}>
          Adds <code>detur_link=&lt;key&gt;</code> to links the SDK hands your app. Send it back once for per-link conversions and
          retention:
        </p>
        <div class="code-snippet">
          <code>{SNIPPET}</code>
          <CopyButton icon value={SNIPPET} label="Copy snippet" />
        </div>
      </div>
      <Switch id="set-tag" checked={on} disabled={saving} onChange={(e) => save(e.currentTarget.checked)} />
    </div>
  );
}
