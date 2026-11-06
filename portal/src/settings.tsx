import { JSX } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import {
  Alert,
  Badge,
  Button,
  Card,
  Dialog,
  Field,
  Input,
  Label,
  Switch,
  Tab,
  TabList,
  Table,
  Textarea,
  toast,
  Toggle,
  ToggleGroup,
} from 'kinu';
import { api, App, AppPage, CreatedApp, HealthCheck, HttpError, MatchQuality, PagePreview, RedirectProfile, SocialPreset } from './api';
import {
  AdminGateAborted,
  adminCall,
  AdminRequired,
  closeDialog,
  ConfirmDelete,
  CopyButton,
  gateOpen,
  getAdmin,
  getAdminSet,
  Loading,
  mono,
  muted,
  refreshAdminSession,
  row,
  subscribeAdmin,
  useAdminLocked,
} from './ui';
import { inRange, THRESHOLD, WINDOW } from './matching';
import { CodeEditor } from './code';

const sectionTitle = { margin: '40px 0 16px', fontSize: 20 };

// Settings tab: App (health, config, key), Page, Matching (rules + quality).
// admin only: not elevated -> locked notice, nothing else. Session dying after the panel rendered
// hides it instead (drafts + an in-flight save's retry survive the re-unlock).
export function SettingsPanel({ app, onApp, onReload }: { app: App; onApp: (a: App) => void; onReload: () => void }) {
  const [tab, setTab] = useState<'app' | 'page' | 'matching'>('app');
  const [error, setError] = useState('');
  const locked = useAdminLocked();
  // tabs hide the admin-only page load, so probe once: a dead session locks the panel on arrival
  useEffect(() => {
    void refreshAdminSession();
  }, []);
  const shown = useRef(false);
  const notice = <AdminRequired>Enter your admin password to view and change settings.</AdminRequired>;
  if (locked && !shown.current) return notice;
  shown.current = true;

  return (
    <div>
      {locked && notice}
      <div hidden={locked}>
        {error && <Alert variant="destructive">{error}</Alert>}
        <TabList class="settings-tabs">
          <Tab role="tab" aria-selected={tab === 'app'} onClick={() => setTab('app')}>
            App
          </Tab>
          <Tab role="tab" aria-selected={tab === 'page'} onClick={() => setTab('page')}>
            Page
          </Tab>
          <Tab role="tab" aria-selected={tab === 'matching'} onClick={() => setTab('matching')}>
            Matching
          </Tab>
        </TabList>

        {tab === 'app' && (
          <>
            <h2 style={{ ...sectionTitle, marginTop: 0 }}>Health</h2>
            <p style={{ ...muted, marginBottom: 16 }}>Setup checks. Refreshes after you save.</p>
            <HealthTable app={app} />

            <h2 style={sectionTitle}>App config</h2>
            <p style={{ ...muted, marginBottom: 16 }}>Used to serve the iOS and Android well-known files.</p>
            <AppConfigTable app={app} onSaved={onApp} />

            <h2 style={sectionTitle}>Store and web redirect presets</h2>
            <RedirectProfilesForm app={app} onSaved={onApp} />

            <h2 style={sectionTitle}>Social preview presets</h2>
            <SocialPresetsForm app={app} onSaved={onApp} />

            <h2 style={sectionTitle}>API key</h2>
            <Table>
              <thead>
                <tr>
                  <th>Credential</th>
                  <th>Value</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                <tr>
                  <td>App ID</td>
                  <td style={mono}>{app.id}</td>
                  <td>
                    <div style={{ ...row, justifyContent: 'flex-end' }}>
                      <CopyButton value={app.id} label="Copy ID" />
                    </div>
                  </td>
                </tr>
                <tr>
                  <td>API key</td>
                  <td>
                    {app.apiKeyHash ? (
                      <span style={row}>
                        <span style={{ ...mono, letterSpacing: 2 }} aria-label="Hidden">
                          ••••••••••••
                        </span>
                        <span style={{ ...muted, fontSize: 13 }}>Shown once when created or rotated.</span>
                      </span>
                    ) : (
                      <span style={{ ...muted, color: 'hsl(var(--k-destructive))' }}>Revoked. SDK calls are rejected.</span>
                    )}
                  </td>
                  <td>
                    <div style={{ ...row, justifyContent: 'flex-end' }}>
                      <RotateKeyDialog app={app} onChanged={onReload} />
                      <ConfirmDelete
                        id={`dlg-revoke-${app.id}`}
                        requireAdmin
                        title={`Remove the API key for ${app.name}?`}
                        body="The SDK stops accepting it. Rotate a new key to get access back."
                        onConfirm={async () => {
                          setError('');
                          try {
                            await adminCall(() => api.revokeKey(app.id));
                            onReload();
                          } catch (e) {
                            if (e instanceof AdminGateAborted) return;
                            setError(String(e));
                          }
                        }}
                      />
                    </div>
                  </td>
                </tr>
              </tbody>
            </Table>
          </>
        )}

        {tab === 'page' && (
          <>
            <p style={{ ...muted, marginTop: 0, marginBottom: 16 }}>
              What visitors see before the store: iOS browsers (copy page) and in-app browsers (tap page). Default, or your own HTML.
            </p>
            <PageEditor app={app} />
          </>
        )}

        {tab === 'matching' && (
          <>
            <p style={{ ...muted, marginTop: 0, marginBottom: 16 }}>Applies to every link of this app.</p>
            <MatchingTable app={app} onSaved={onApp} />
            <TagLinksRow app={app} onSaved={onApp} />

            <h2 style={sectionTitle}>Match quality</h2>
            <p style={{ ...muted, marginBottom: 16 }}>Installs from the last 30 days: how each was attributed, and the score it got.</p>
            <MatchQualityView app={app} />
          </>
        )}
      </div>
    </div>
  );
}

function AppConfigTable({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  const [draft, setDraft] = useState({
    iosAppId: app.iosAppId,
    androidPackage: app.androidPackage,
    androidCertFingerprint: app.androidCertFingerprint,
    iosScheme: app.iosScheme,
  });
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  const save = async () => {
    setError('');
    setSaved(false);
    try {
      const a = await adminCall(() => api.updateApp(app.id, draft));
      // server trims + strips "://" from the scheme: show what was stored
      setDraft({
        iosAppId: a.iosAppId,
        androidPackage: a.androidPackage,
        androidCertFingerprint: a.androidCertFingerprint,
        iosScheme: a.iosScheme,
      });
      onSaved(a);
      setSaved(true);
    } catch (e) {
      if (e instanceof AdminGateAborted) return;
      setError(String(e));
    }
  };

  const field = (id: string, key: keyof typeof draft, placeholder: string, style?: JSX.CSSProperties) => (
    <Input
      id={id}
      value={draft[key]}
      placeholder={placeholder}
      style={style}
      onInput={(e) => setDraft({ ...draft, [key]: e.currentTarget.value })}
    />
  );

  return (
    <form
      style={{ display: 'grid', gap: 16 }}
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <Table>
        <thead>
          <tr>
            <th>Setting</th>
            <th>Value</th>
            <th>Format</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <Label htmlFor="cfg-ios">iOS App ID</Label>
            </td>
            <td>{field('cfg-ios', 'iosAppId', 'ABCDE12345.com.example.app')}</td>
            <td data-label="Format" style={muted}>
              TEAMID.BUNDLEID
            </td>
          </tr>
          <tr>
            <td>
              <Label htmlFor="cfg-scheme">iOS URL scheme</Label>
            </td>
            <td>{field('cfg-scheme', 'iosScheme', 'myapp')}</td>
            <td data-label="Format" style={muted}>
              Optional. In-app browsers open the installed app first
            </td>
          </tr>
          <tr>
            <td>
              <Label htmlFor="cfg-android">Android package</Label>
            </td>
            <td>{field('cfg-android', 'androidPackage', 'com.example.app')}</td>
            <td data-label="Format" style={muted}>
              Application ID
            </td>
          </tr>
          <tr>
            <td>
              <Label htmlFor="cfg-cert">Android cert fingerprint</Label>
            </td>
            <td>{field('cfg-cert', 'androidCertFingerprint', 'AA:BB:CC:…', mono)}</td>
            <td data-label="Format" style={muted}>
              SHA-256 of the signing certificate
            </td>
          </tr>
        </tbody>
      </Table>
      {error && <Alert variant="destructive">{error}</Alert>}
      {saved && <Alert variant="success">Saved.</Alert>}
      <div style={{ ...row, justifyContent: 'flex-end' }}>
        <Button type="submit">Save</Button>
      </div>
    </form>
  );
}

const PAGE_MAX = 51200;
// placeholder help: `backticks` -> <code>
const codeSpans = (s: string) => s.split('`').map((t, i) => (i % 2 ? <code key={i}>{t}</code> : t));
const PREVIEWS: { label: string; p: Pick<PagePreview, 'page' | 'platform'> }[] = [
  { label: 'iOS copy', p: { page: 'copy', platform: 'ios' } },
  { label: 'iOS in-app', p: { page: 'tap', platform: 'ios' } },
  { label: 'Android in-app', p: { page: 'tap', platform: 'android' } },
];

// loads on mount (admin-gated GET, 50 KB kept out of the app list), never prompts: viewer -> Unlock admin notice, reloads on elevation.
// One HTML field (starts as the default), preview, save.
// Text == default -> saved as "" (stays on the built-in page, picks up its future changes).
function PageEditor({ app }: { app: App }) {
  const [data, setData] = useState<AppPage | null>(null);
  const [text, setText] = useState('');
  const [view, setView] = useState(0);
  const [preview, setPreview] = useState('');
  const [error, setError] = useState('');
  const [locked, setLocked] = useState(false);
  const loaded = useRef(false); // unlock reloads only until the page is in (never clobbers edits)

  useEffect(() => {
    let stale = false;
    const load = () => {
      if (getAdminSet() && !getAdmin()) return setLocked(true);
      api.getPage(app.id).then(
        (d) => !stale && ((loaded.current = true), setLocked(false), setData(d), setText(d.html || d.defaultHtml)),
        (e) => !stale && (e instanceof HttpError && e.status === 403 ? (setLocked(true), refreshAdminSession()) : setError(String(e))),
      );
    };
    load();
    const unsub = subscribeAdmin(() => !stale && !loaded.current && load());
    return () => {
      stale = true;
      unsub();
    };
  }, [app.id]);

  const html = !data || text === data.defaultHtml ? '' : text;
  // debounced: real renderer on the server; sandboxed frame (no scripts)
  useEffect(() => {
    if (!data) return;
    let stale = false;
    const t = setTimeout(() => {
      api.previewPage(app.id, { html, ...PREVIEWS[view].p }).then(
        (h) => !stale && (setPreview(h), setError('')),
        (e) => !stale && setError(String(e)),
      );
    }, 400);
    return () => {
      stale = true;
      clearTimeout(t);
    };
  }, [data, html, view, app.id]);

  const save = async () => {
    setError('');
    try {
      await adminCall(() => api.savePage(app.id, html));
      toast.show(html ? 'Custom page saved' : 'Default page saved');
    } catch (e) {
      if (e instanceof AdminGateAborted) return;
      setError(String(e));
    }
  };

  if (!data) {
    if (locked) return <AdminRequired>Enter your admin password to edit the page.</AdminRequired>;
    return error ? <Alert variant="destructive">{error}</Alert> : <Loading />;
  }
  const bytes = new TextEncoder().encode(text).length;
  return (
    <div style={{ display: 'grid', gap: 16 }}>
      {/* one toolbar line: reset, save */}
      <div style={{ ...row, justifyContent: 'flex-end', gap: 12 }}>
        <Button variant="outline" disabled={!html} onClick={() => setText(data.defaultHtml)}>
          Reset to default
        </Button>
        <Button onClick={save}>Save</Button>
      </div>
      {error && <Alert variant="destructive">{error}</Alert>}
      {/* HTML 2 cols, preview 1 col; stacks below ~720px. Twin boxes: same header bar + 560px body -> equal height */}
      <div class="page-editor">
        <div class="page-box">
          <div class="page-box-head" style={{ justifyContent: 'space-between' }}>
            <Label htmlFor="page-html" style={{ margin: 0 }}>
              HTML
            </Label>
            <small style={{ ...muted, ...(bytes > PAGE_MAX ? { color: 'hsl(var(--k-destructive))' } : {}) }}>
              {html ? 'Custom' : 'Default'} page · {bytes.toLocaleString()} / {PAGE_MAX.toLocaleString()} bytes
            </small>
          </div>
          <CodeEditor id="page-html" value={text} onInput={setText} height={560} />
        </div>
        {/* preview frame: its own target switch on top */}
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
            style={{ display: 'block', width: '100%', height: 560, border: 0, background: '#fff' }}
          />
        </div>
      </div>
      <div style={muted}>
        <ul style={{ margin: 0, paddingLeft: 18 }}>
          {data.placeholders.map((p) => (
            <li key={p.name}>
              <code class="hl-ph">{`{{${p.name}}}`}</code> {codeSpans(p.desc)}
            </li>
          ))}
        </ul>
        <p style={{ marginTop: 8 }}>No scripts or on* handlers. Images and fonts: https only.</p>
      </div>
    </div>
  );
}

export // Reusable store/web URL sets: a link copies one when picked (presets, not inheritance).
function RedirectProfilesForm({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  const [profiles, setProfiles] = useState<RedirectProfile[]>(() => app.redirectProfiles ?? []);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  const save = async () => {
    setError('');
    setSaved(false);
    try {
      const a = await adminCall(() =>
        api.updateApp(app.id, {
          iosAppId: app.iosAppId,
          androidPackage: app.androidPackage,
          androidCertFingerprint: app.androidCertFingerprint,
          iosScheme: app.iosScheme,
          redirectProfiles: profiles,
        }),
      );
      setProfiles(a.redirectProfiles ?? []);
      onSaved(a);
      setSaved(true);
    } catch (e) {
      if (e instanceof AdminGateAborted) return;
      setError(String(e));
    }
  };

  return (
    <form
      style={{ display: 'grid', gap: 16 }}
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <p style={{ ...muted, marginTop: 0, marginBottom: 0 }}>
        Save the store and web URLs you reuse. A link copies a preset when picked; editing it here later does not change links already
        created.
      </p>
      {profiles.map((p, i) => {
        const set = (patch: Partial<RedirectProfile>) => setProfiles(profiles.map((x, j) => (j === i ? { ...x, ...patch } : x)));
        return (
          <div style={{ ...row, alignItems: 'flex-end', gap: 8 }} key={i}>
            <div class="form-grid" style={{ flex: 1 }}>
              <Field>
                <Label htmlFor={`prof-name-${i}`}>Redirect preset name</Label>
                <Input
                  id={`prof-name-${i}`}
                  value={p.name}
                  onInput={(e) => set({ name: e.currentTarget.value })}
                  placeholder="Production"
                />
              </Field>
              <Field>
                <Label htmlFor={`prof-ios-${i}`}>Preset iOS store</Label>
                <Input
                  id={`prof-ios-${i}`}
                  value={p.ios ?? ''}
                  onInput={(e) => set({ ios: e.currentTarget.value })}
                  placeholder="https://apps.apple.com/app/id123"
                />
              </Field>
              <Field>
                <Label htmlFor={`prof-android-${i}`}>Preset Android store</Label>
                <Input
                  id={`prof-android-${i}`}
                  value={p.android ?? ''}
                  onInput={(e) => set({ android: e.currentTarget.value })}
                  placeholder="https://play.google.com/store/apps/details?id=com.example"
                />
              </Field>
              <Field>
                <Label htmlFor={`prof-web-${i}`}>Preset web fallback</Label>
                <Input
                  id={`prof-web-${i}`}
                  value={p.web ?? ''}
                  onInput={(e) => set({ web: e.currentTarget.value })}
                  placeholder="https://example.com"
                />
              </Field>
            </div>
            <Button
              type="button"
              size="sm"
              variant="outline"
              aria-label={`Remove redirect preset ${i + 1}`}
              onClick={() => setProfiles(profiles.filter((_, j) => j !== i))}
            >
              Remove
            </Button>
          </div>
        );
      })}
      <div style={{ ...row, justifyContent: 'space-between' }}>
        <Button
          type="button"
          variant="outline"
          disabled={profiles.length >= 10}
          onClick={() => setProfiles([...profiles, { name: '', ios: '', android: '', web: '' }])}
        >
          Add redirect preset
        </Button>
        <Button type="submit">Save</Button>
      </div>
      {error && <Alert variant="destructive">{error}</Alert>}
      {saved && <Alert variant="success">Saved.</Alert>}
    </form>
  );
}

// Reusable social preview sets: a link copies one when picked.
function SocialPresetsForm({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  const [presets, setPresets] = useState<SocialPreset[]>(() => app.socialPresets ?? []);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  const save = async () => {
    setError('');
    setSaved(false);
    try {
      const a = await adminCall(() =>
        api.updateApp(app.id, {
          iosAppId: app.iosAppId,
          androidPackage: app.androidPackage,
          androidCertFingerprint: app.androidCertFingerprint,
          iosScheme: app.iosScheme,
          socialPresets: presets,
        }),
      );
      setPresets(a.socialPresets ?? []);
      onSaved(a);
      setSaved(true);
    } catch (e) {
      if (e instanceof AdminGateAborted) return;
      setError(String(e));
    }
  };

  return (
    <form
      style={{ display: 'grid', gap: 16 }}
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <p style={{ ...muted, marginTop: 0, marginBottom: 0 }}>
        Save the chat and social preview text you reuse. A link copies a preset when picked; editing it here later does not change links
        already created.
      </p>
      {presets.map((p, i) => {
        const set = (patch: Partial<SocialPreset>) => setPresets(presets.map((x, j) => (j === i ? { ...x, ...patch } : x)));
        return (
          <div style={{ ...row, alignItems: 'flex-end', gap: 8 }} key={i}>
            <div class="form-grid" style={{ flex: 1 }}>
              <Field>
                <Label htmlFor={`social-name-${i}`}>Social preset name</Label>
                <Input id={`social-name-${i}`} value={p.name} onInput={(e) => set({ name: e.currentTarget.value })} placeholder="Launch" />
              </Field>
              <Field>
                <Label htmlFor={`social-title-${i}`}>Preview title</Label>
                <Input
                  id={`social-title-${i}`}
                  value={p.title ?? ''}
                  maxLength={100}
                  onInput={(e) => set({ title: e.currentTarget.value })}
                  placeholder="Sam invited you"
                />
              </Field>
              <Field>
                <Label htmlFor={`social-image-${i}`}>Preview image URL</Label>
                <Input
                  id={`social-image-${i}`}
                  value={p.image ?? ''}
                  onInput={(e) => set({ image: e.currentTarget.value })}
                  placeholder="https://example.com/card.png"
                />
              </Field>
              <Field>
                <Label htmlFor={`social-desc-${i}`}>Preview description</Label>
                <Textarea
                  id={`social-desc-${i}`}
                  value={p.description ?? ''}
                  maxLength={300}
                  rows={2}
                  onInput={(e) => set({ description: e.currentTarget.value })}
                />
              </Field>
            </div>
            <Button
              type="button"
              size="sm"
              variant="outline"
              aria-label={`Remove social preset ${i + 1}`}
              onClick={() => setPresets(presets.filter((_, j) => j !== i))}
            >
              Remove
            </Button>
          </div>
        );
      })}
      <div style={{ ...row, justifyContent: 'space-between' }}>
        <Button
          type="button"
          variant="outline"
          disabled={presets.length >= 10}
          onClick={() => setPresets([...presets, { name: '', title: '', description: '', image: '' }])}
        >
          Add social preset
        </Button>
        <Button type="submit">Save</Button>
      </div>
      {error && <Alert variant="destructive">{error}</Alert>}
      {saved && <Alert variant="success">Saved.</Alert>}
    </form>
  );
}

function MatchingTable({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  const [threshold, setThreshold] = useState(String(app.matchThreshold));
  const [windowMinutes, setWindowMinutes] = useState(String(app.matchWindowMinutes));
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  const save = async () => {
    setError('');
    setSaved(false);
    if (threshold.trim() === '' || windowMinutes.trim() === '') {
      setError('threshold and window are required');
      return;
    }
    const th = Number(threshold);
    const win = Number(windowMinutes);
    if (!Number.isFinite(th) || !Number.isFinite(win)) {
      setError('threshold and window must be numbers');
      return;
    }
    if (!inRange(th, THRESHOLD) || !inRange(win, WINDOW)) {
      setError(`threshold must be ${THRESHOLD.min}–${THRESHOLD.max}, window ${WINDOW.min}–${WINDOW.max} minutes`);
      return;
    }
    try {
      const updated = await adminCall(() => api.saveMatching(app.id, { threshold: th, windowMinutes: win }));
      setThreshold(String(updated.matchThreshold));
      setWindowMinutes(String(updated.matchWindowMinutes));
      setSaved(true);
      onSaved(updated);
    } catch (e) {
      if (e instanceof AdminGateAborted) return;
      setError(String(e));
    }
  };

  const field = (id: string, value: string, set: (v: string) => void) => (
    <Input id={id} type="number" value={value} onInput={(e) => set(e.currentTarget.value)} style={{ maxWidth: 120 }} />
  );

  return (
    <form
      style={{ display: 'grid', gap: 16 }}
      onSubmit={(e) => {
        e.preventDefault();
        save();
      }}
    >
      <Table>
        <thead>
          <tr>
            <th>Setting</th>
            <th>Value</th>
            <th>Range</th>
            <th>Effect</th>
          </tr>
        </thead>
        <tbody>
          <tr>
            <td>
              <Label htmlFor="set-th">Match threshold</Label>
            </td>
            <td>{field('set-th', threshold, setThreshold)}</td>
            <td data-label="Range" style={muted}>
              {THRESHOLD.min}–{THRESHOLD.max}
            </td>
            <td style={muted}>Higher means stricter install matching.</td>
          </tr>
          <tr>
            <td>
              <Label htmlFor="set-win">Match window (minutes)</Label>
            </td>
            <td>{field('set-win', windowMinutes, setWindowMinutes)}</td>
            <td data-label="Range" style={muted}>
              {WINDOW.min}–{WINDOW.max}
            </td>
            <td style={muted}>How long after a click an install can still match it.</td>
          </tr>
        </tbody>
      </Table>
      {error && <Alert variant="destructive">{error}</Alert>}
      {saved && <Alert variant="success">Saved.</Alert>}
      <div style={{ ...row, justifyContent: 'flex-end' }}>
        <Button type="submit">Save</Button>
      </div>
    </form>
  );
}

// saves on press (Fraud tab pattern): toast; rejected -> revert + toast why
const TAG_SNIPPET = "DetourAnalytics.logEvent('detur_link', { link: link.params.detur_link })";

function TagLinksRow({ app, onSaved }: { app: App; onSaved: (a: App) => void }) {
  const [on, setOn] = useState(app.tagLinks);
  const save = async (next: boolean) => {
    setOn(next);
    try {
      const updated = await adminCall(() => api.saveTagging(app.id, next));
      setOn(updated.tagLinks);
      onSaved(updated);
      toast.show(`Tag deferred links: ${updated.tagLinks ? 'on' : 'off'}`);
    } catch (e) {
      setOn(!next);
      if (e instanceof AdminGateAborted) return;
      toast.show(String(e), { title: 'Not saved' });
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
          <code>{TAG_SNIPPET}</code>
          <CopyButton icon value={TAG_SNIPPET} label="Copy snippet" />
        </div>
      </div>
      <Switch id="set-tag" checked={on} onChange={(e) => save(e.currentTarget.checked)} />
    </div>
  );
}

const STATUS = { ok: 'OK', warn: 'Check', fail: 'Fix' };

// Refetch on app change (config save).
function HealthTable({ app }: { app: App }) {
  const [checks, setChecks] = useState<HealthCheck[] | null>(null);
  useEffect(() => {
    api.getHealth(app.id).then(setChecks, () => setChecks([]));
  }, [app]);
  if (!checks) return <Loading />;
  return (
    <Table>
      <thead>
        <tr>
          <th>Check</th>
          <th>Status</th>
          <th>Detail</th>
        </tr>
      </thead>
      <tbody>
        {checks.map((c) => (
          <tr key={c.check}>
            <td>{c.check}</td>
            <td data-label="Status">
              <span class={`health ${c.status}`}>{STATUS[c.status]}</span>
            </td>
            <td style={muted}>{c.detail}</td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

const METHODS: [string, string][] = [
  ['click_id', 'Click ID'],
  ['pasteboard', 'Clipboard'],
  ['probabilistic', 'Fingerprint'],
  ['prior', 'Retry'],
  ['organic', 'Organic'],
  ['', 'Before receipts'],
];

// Method mix, score histogram, threshold what-if.
export function MatchQualityView({ app }: { app: App }) {
  const [q, setQ] = useState<MatchQuality | null>(null);
  const [at, setAt] = useState(app.matchThreshold);
  useEffect(() => {
    api.getMatchQuality(app.id).then(setQ, () => setQ({ methods: {}, buckets: [] }));
  }, [app.id]);
  useEffect(() => setAt(app.matchThreshold), [app.matchThreshold]);
  if (!q) return <Loading />;

  // every 50-point bucket across the threshold range + seen scores; empty ones as zero (one bucket alone filled the card)
  const seen = new Map(q.buckets.map((b) => [b.from, b]));
  const lo = Math.min(THRESHOLD.min, ...q.buckets.map((b) => b.from));
  const hi = Math.max(THRESHOLD.max, ...q.buckets.map((b) => b.from));
  const bars: typeof q.buckets = [];
  for (let f = lo; f <= hi; f += 50) bars.push(seen.get(f) ?? { from: f, matched: 0, organic: 0 });
  const top = Math.max(1, ...q.buckets.map((b) => b.matched + b.organic));
  const lose = q.buckets.filter((b) => b.from < at).reduce((n, b) => n + b.matched, 0);
  const gain = q.buckets.filter((b) => b.from >= at).reduce((n, b) => n + b.organic, 0);
  return (
    <Card style={{ display: 'grid', gap: 16 }}>
      <div style={{ ...row, flexWrap: 'wrap' }}>
        {METHODS.filter(([k]) => q.methods[k]).map(([k, label]) => (
          <Badge key={k} variant="secondary" style={{ letterSpacing: 'normal' }}>
            {label} {q.methods[k]}
          </Badge>
        ))}
        {Object.keys(q.methods).length === 0 && <p style={muted}>No installs yet.</p>}
      </div>
      {q.buckets.length > 0 && (
        <>
          <div class="score-bars" role="img" aria-label="Installs per fingerprint score">
            {bars.map((b) => (
              <div key={b.from} title={`${b.from}–${b.from + 49}: ${b.matched} matched, ${b.organic} organic`}>
                <span class="score-bar organic" style={{ height: `${(b.organic / top) * 100}%` }} />
                <span class="score-bar matched" style={{ height: `${(b.matched / top) * 100}%`, opacity: b.from < at ? 0.35 : 1 }} />
                <small>{b.from}</small>
              </div>
            ))}
          </div>
          <Field>
            <Label htmlFor="whatif">
              At threshold {at}: lose {lose} matches, gain up to {gain} organic installs
            </Label>
            <input
              id="whatif"
              type="range"
              min={THRESHOLD.min}
              max={THRESHOLD.max}
              step={50}
              value={at}
              onInput={(e) => setAt(Number(e.currentTarget.value))}
            />
            <Field.Description>Fingerprint matches only. Click ID matches never depend on the threshold.</Field.Description>
          </Field>
        </>
      )}
    </Card>
  );
}

// mints new key, old dies now; plaintext shown once, like create
function RotateKeyDialog({ app, onChanged }: { app: App; onChanged: () => void }) {
  const [rotated, setRotated] = useState<CreatedApp | null>(null);
  const [err, setErr] = useState('');
  const id = `dlg-rotate-${app.id}`;

  // ref guard: a second rotate would kill the key this dialog is about to show
  const rotating = useRef(false);
  const [busy, setBusy] = useState(false);
  const doRotate = async () => {
    if (rotating.current) return;
    rotating.current = true;
    setBusy(true);
    setErr('');
    try {
      setRotated(await adminCall(() => api.rotateKey(app.id)));
      onChanged();
    } catch (e) {
      if (e instanceof AdminGateAborted) return;
      setErr(String(e));
    } finally {
      rotating.current = false;
      setBusy(false);
    }
  };

  return (
    <Dialog id={id}>
      <Button size="sm" variant="outline" onClick={() => gateOpen(id)}>
        Rotate
      </Button>
      <Dialog.Content>
        {rotated ? (
          <div style={{ display: 'grid', gap: 16 }}>
            <h2 style={{ margin: 0 }}>Key rotated</h2>
            <Alert variant="warning">Old key stopped working. New key shown once.</Alert>
            <Field>
              <Label htmlFor={`${id}-key`}>API key</Label>
              <Input id={`${id}-key`} readOnly value={rotated.apiKey} style={mono} />
              <Field.Description>EXPO_PUBLIC_DETOUR_API_KEY in the SDK config.</Field.Description>
            </Field>
            <div style={{ ...row, justifyContent: 'flex-end' }}>
              <Button
                variant="outline"
                onClick={() => {
                  setRotated(null);
                  closeDialog(id);
                }}
              >
                Done
              </Button>
              <CopyButton value={rotated.apiKey} label="Copy API key" />
            </div>
          </div>
        ) : (
          <form
            style={{ display: 'grid', gap: 16 }}
            onSubmit={(e) => {
              e.preventDefault();
              doRotate();
            }}
          >
            <h2 style={{ margin: 0 }}>Rotate API key</h2>
            <p style={muted}>A new key is generated; the current one stops working immediately.</p>
            {err && <Alert variant="destructive">{err}</Alert>}
            <div style={{ ...row, justifyContent: 'flex-end' }}>
              <Button type="button" variant="outline" onClick={() => closeDialog(id)}>
                Cancel
              </Button>
              <Button type="submit" disabled={busy} aria-busy={busy}>
                Rotate key
              </Button>
            </div>
          </form>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
