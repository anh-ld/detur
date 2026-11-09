import { useEffect, useMemo, useState } from 'preact/hooks';
import { Alert, Button, Label, Toggle, ToggleGroup, toast } from 'kinu';
import { api } from '../../../lib/api';
import { adminCall, useGatedLoad } from '../../../lib/admin';
import { actionErrors } from '../../../components/action-errors';
import { AdminRequired } from '../../../components/admin';
import { CodeEditor } from '../../../components/code-editor';
import { Loading } from '../../../components/page';
import { fmt } from '../../../lib/format';
import { muted, row, stack } from '../../../lib/styles';
import { App, AppPage, PagePreview } from '../../../lib/types';
import { useLatest } from '../../../lib/use-latest';

const PAGE_MAX = 51200;
const BOX_HEIGHT = 560;
const PREVIEWS: { label: string; p: Pick<PagePreview, 'page' | 'platform'> }[] = [
  { label: 'iOS copy', p: { page: 'copy', platform: 'ios' } },
  { label: 'iOS in-app', p: { page: 'tap', platform: 'ios' } },
  { label: 'Android in-app', p: { page: 'tap', platform: 'android' } },
];

const codeSpans = (s: string) => s.split('`').map((t, i) => (i % 2 ? <code key={i}>{t}</code> : t));

export function PageEditor({ app }: { app: App }) {
  const [text, setText] = useState('');
  const [view, setView] = useState(0);
  const [preview, setPreview] = useState('');
  const [error, setError] = useState('');
  const errors = actionErrors(setError);
  const previewLoad = useLatest();
  const {
    data,
    locked,
    error: loadError,
  } = useGatedLoad<AppPage>({
    load: () => api.getPage(app.id),
    deps: [app.id],
    mode: 'when-set',
    onLoaded: (d) => setText(d.html || d.defaultHtml),
  });

  const bytes = useMemo(() => new TextEncoder().encode(text).length, [text]);
  const html = !data || text === data.defaultHtml ? '' : text;
  useEffect(() => {
    if (!data) return;
    const live = previewLoad.begin();
    const t = setTimeout(() => {
      api.previewPage(app.id, { html, ...PREVIEWS[view].p }).then(
        (h) => live() && (setPreview(h), setError('')),
        (e) => live() && setError(String(e)),
      );
    }, 400);
    return () => {
      previewLoad.cancel();
      clearTimeout(t);
    };
  }, [data, html, view, app.id]);

  const save = async () => {
    setError('');
    try {
      await adminCall(() => api.savePage(app.id, html));
      toast.show(html ? 'Custom page saved' : 'Default page saved');
    } catch (e) {
      errors.fail(e);
    }
  };

  if (!data) {
    if (locked) return <AdminRequired>Enter your admin password to edit the page.</AdminRequired>;
    return error || loadError ? <Alert variant="destructive">{error || loadError}</Alert> : <Loading />;
  }
  return (
    <div style={stack}>
      <div style={{ ...row, justifyContent: 'flex-end', gap: 12 }}>
        <Button variant="outline" disabled={!html} onClick={() => setText(data.defaultHtml)}>
          Reset to default
        </Button>
        <Button onClick={save}>Save</Button>
      </div>
      {error && <Alert variant="destructive">{error}</Alert>}
      <div class="page-editor">
        <div class="page-box">
          <div class="page-box-head" style={{ justifyContent: 'space-between' }}>
            <Label htmlFor="page-html" style={{ margin: 0 }}>
              HTML
            </Label>
            <small style={{ ...muted, ...(bytes > PAGE_MAX ? { color: 'hsl(var(--k-destructive))' } : {}) }}>
              {html ? 'Custom' : 'Default'} page · {fmt(bytes)} / {fmt(PAGE_MAX)} bytes
            </small>
          </div>
          <CodeEditor id="page-html" value={text} onInput={setText} height={BOX_HEIGHT} />
        </div>
        <div class="page-box">
          <div class="page-box-head" style={{ justifyContent: 'center' }}>
            <ToggleGroup role="group" aria-label="Preview" class="seg">
              {PREVIEWS.map((v, i) => (
                <Toggle key={v.label} type="button" size="sm" pressed={view === i} onClickCapture={() => {}} onClick={() => setView(i)}>
                  {v.label}
                </Toggle>
              ))}
            </ToggleGroup>
          </div>
          <iframe
            title="Page preview"
            sandbox=""
            srcdoc={preview}
            style={{ display: 'block', width: '100%', height: BOX_HEIGHT, border: 0, background: 'hsl(var(--k-card))' }}
          />
        </div>
      </div>
      <Placeholders placeholders={data.placeholders} />
    </div>
  );
}

function Placeholders({ placeholders }: { placeholders: AppPage['placeholders'] }) {
  return (
    <div style={muted}>
      <ul style={{ margin: 0, paddingLeft: 18 }}>
        {placeholders.map((p) => (
          <li key={p.name}>
            <code class="hl-ph">{`{{${p.name}}}`}</code> {codeSpans(p.desc)}
          </li>
        ))}
      </ul>
      <p style={{ marginTop: 8 }}>No scripts or on* handlers. Images and fonts: https only.</p>
    </div>
  );
}
