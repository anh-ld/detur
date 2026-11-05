import { useEffect, useState } from 'preact/hooks';
import { Alert, Button, Card, Checkbox, Dialog, Empty, Field, Input, Label, Table } from 'kinu';
import { api, ApiToken, App, CreatedToken, HttpError } from './api';
import {
  adminCall,
  AdminGateAborted,
  AdminRequired,
  closeDialog,
  CopyButton,
  gateOpen,
  getAdmin,
  getAdminSet,
  Loading,
  mono,
  muted,
  openDialog,
  PageHeader,
  row,
  subscribeAdmin,
} from './ui';

const fmtDate = (iso: string) => new Date(iso).toLocaleDateString('en', { month: 'short', day: 'numeric', year: 'numeric' });

// app checkboxes shared by create form and edit dialog
function AppChecks({ apps, value, onChange }: { apps: App[]; value: string[]; onChange: (ids: string[]) => void }) {
  if (apps.length === 0) return <p style={muted}>No apps yet. Create an app first.</p>;
  return (
    <div style={{ display: 'grid', gap: 10, marginTop: 4 }}>
      {apps.map((a) => (
        <label key={a.id} class="check-option">
          <Checkbox
            checked={value.includes(a.id)}
            onChange={() => onChange(value.includes(a.id) ? value.filter((x) => x !== a.id) : [...value, a.id])}
          />
          <span class="check-option-title">{a.name}</span>
        </label>
      ))}
    </div>
  );
}

// Admin-managed data-API / MCP tokens. Plaintext token lives in state only while its dialog is open.
export function ApiTokensPage() {
  const [tokens, setTokens] = useState<ApiToken[] | null>(null);
  const [apps, setApps] = useState<App[]>([]);
  const [linkBase, setLinkBase] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [admin, setAdmin] = useState(() => ({ adminSet: getAdminSet(), admin: getAdmin() }));

  const [name, setName] = useState('');
  const [picked, setPicked] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [created, setCreated] = useState<CreatedToken | null>(null);
  const [editing, setEditing] = useState<ApiToken | null>(null);
  const [revoking, setRevoking] = useState<ApiToken | null>(null);

  useEffect(() => subscribeAdmin(() => setAdmin({ adminSet: getAdminSet(), admin: getAdmin() })), []);

  const fail = (e: unknown) => {
    if (!(e instanceof AdminGateAborted)) setError(String(e));
  };

  useEffect(() => {
    api.listApps().then(setApps, fail);
    api.getConfig().then(
      (c) => setLinkBase(c.linkBase),
      () => {},
    );
  }, []);

  useEffect(() => {
    if (!admin.adminSet || !admin.admin) {
      // server 403s until elevated: skip
      setTokens([]);
      setLoading(false);
      return;
    }
    api
      .listTokens()
      .then(setTokens, (e) => (e instanceof HttpError && e.status === 403 ? setTokens([]) : setError(String(e))))
      .finally(() => setLoading(false));
  }, [admin.adminSet, admin.admin]);

  const appNames = (ids: string[]) => ids.map((id) => apps.find((a) => a.id === id)?.name ?? id).join(', ');

  const create = async (e: Event) => {
    e.preventDefault();
    if (busy || !name.trim() || picked.length === 0) return;
    setBusy(true);
    setError('');
    try {
      const t = await adminCall(() => api.createToken({ name: name.trim(), appIds: picked }));
      const { token: _plain, ...rest } = t;
      setTokens((prev) => [...(prev ?? []), rest]);
      setCreated(t);
      setName('');
      setPicked([]);
      openDialog('dlg-created-token');
    } catch (e) {
      fail(e);
    } finally {
      setBusy(false);
    }
  };

  const revoke = async () => {
    if (!revoking) return;
    const id = revoking.id;
    closeDialog('dlg-revoke-token');
    setRevoking(null);
    try {
      await adminCall(() => api.deleteToken(id));
      setTokens((prev) => (prev ? prev.filter((t) => t.id !== id) : null));
    } catch (e) {
      fail(e);
    }
  };

  const done = () => {
    closeDialog('dlg-created-token');
    setCreated(null); // plaintext gone from state
  };

  if (loading) return <Loading />;

  const command = created ? mcpJson(`${linkBase || location.origin}/api/mcp`, created.token) : '';

  return (
    <div>
      <PageHeader title="API" description="Tokens let agents and scripts read analytics, read-only, for the apps you pick." />

      {error && (
        <Alert variant="destructive" style={{ marginBottom: 16 }}>
          {error}
        </Alert>
      )}

      {!admin.adminSet ? (
        <Alert>
          API tokens need an admin password. Set <code>ADMIN_PASSWORD</code> on the server, then restart to create tokens.
        </Alert>
      ) : (
        <div style={{ display: 'grid', gap: 24 }}>
          {!admin.admin && <AdminRequired>Enter your admin password to view, create, edit or revoke API tokens.</AdminRequired>}

          <Card>
            <form style={{ display: 'grid', gap: 16, padding: 16 }} onSubmit={create}>
              <h2 style={{ margin: 0, fontSize: 20 }}>New token</h2>
              <Field>
                <Label htmlFor="tok-name">Name</Label>
                <Input id="tok-name" placeholder="Claude Code" value={name} onInput={(e) => setName(e.currentTarget.value)} />
              </Field>
              <Field>
                <Label>Allowed apps</Label>
                <AppChecks apps={apps} value={picked} onChange={setPicked} />
              </Field>
              <div style={{ ...row, justifyContent: 'flex-end' }}>
                <Button type="submit" disabled={busy || !name.trim() || picked.length === 0}>
                  {busy ? 'Creating…' : 'Create token'}
                </Button>
              </div>
            </form>
          </Card>

          <Connect base={linkBase || location.origin} />

          {admin.admin &&
            (tokens && tokens.length === 0 ? (
              <Card>
                <Empty>
                  <h3>No tokens yet</h3>
                  Create one to connect an agent.
                </Empty>
              </Card>
            ) : (
              <Table>
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Token</th>
                    <th>Apps</th>
                    <th>Created</th>
                    <th>Last used</th>
                    <th />
                  </tr>
                </thead>
                <tbody>
                  {tokens?.map((t) => (
                    <tr key={t.id}>
                      <td>{t.name}</td>
                      <td style={mono}>{t.prefix}…</td>
                      <td>{appNames(t.appIds)}</td>
                      <td>{fmtDate(t.createdAt)}</td>
                      <td>{t.lastUsedAt ? fmtDate(t.lastUsedAt) : 'Never'}</td>
                      <td style={{ textAlign: 'right' }}>
                        <div style={{ ...row, justifyContent: 'flex-end' }}>
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => {
                              setEditing(t);
                              openDialog('dlg-edit-token');
                            }}
                          >
                            Edit
                          </Button>
                          <Button
                            size="sm"
                            variant="destructive"
                            onClick={() => {
                              setRevoking(t);
                              gateOpen('dlg-revoke-token');
                            }}
                          >
                            Revoke
                          </Button>
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </Table>
            ))}
        </div>
      )}

      <Dialog id="dlg-created-token">
        <Dialog.Content>
          <div style={{ display: 'grid', gap: 16 }}>
            <h2 style={{ margin: 0 }}>Token created</h2>
            <p style={muted}>This token won't be shown again. Copy it now.</p>
            {created && (
              <>
                <Field>
                  <Label>Token</Label>
                  <div style={{ ...row, gap: 8 }}>
                    <Input readOnly value={created.token} style={mono} />
                    <CopyButton value={created.token} label="Copy token" />
                  </div>
                </Field>
                <Field>
                  <div style={{ ...row, justifyContent: 'space-between' }}>
                    <Label>MCP config</Label>
                    <CopyButton value={command} label="Copy MCP config" />
                  </div>
                  <pre style={codeBlock}>{command}</pre>
                </Field>
              </>
            )}
            <div style={{ ...row, justifyContent: 'flex-end' }}>
              <Button onClick={done}>Done</Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog>

      <EditTokenDialog
        token={editing}
        apps={apps}
        onSaved={(t) => setTokens((prev) => (prev ? prev.map((x) => (x.id === t.id ? t : x)) : prev))}
        onError={fail}
      />

      <Dialog id="dlg-revoke-token">
        <Dialog.Content>
          <div style={{ display: 'grid', gap: 16 }}>
            <h2 style={{ margin: 0 }}>Revoke token</h2>
            <p style={muted}>
              Revoke <strong>{revoking?.name}</strong>? Anything using it stops working immediately.
            </p>
            <div style={{ ...row, justifyContent: 'flex-end' }}>
              <Button variant="outline" onClick={() => closeDialog('dlg-revoke-token')}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={revoke}>
                Revoke
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog>
    </div>
  );
}

// fields re-prefill when the target token changes
function EditTokenDialog({
  token,
  apps,
  onSaved,
  onError,
}: {
  token: ApiToken | null;
  apps: App[];
  onSaved: (t: ApiToken) => void;
  onError: (e: unknown) => void;
}) {
  const [name, setName] = useState(token?.name ?? '');
  const [ids, setIds] = useState(token?.appIds ?? []);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    setName(token?.name ?? '');
    setIds(token?.appIds ?? []);
  }, [token]);

  const save = async (e: Event) => {
    e.preventDefault();
    if (!token) return;
    setBusy(true);
    try {
      onSaved(await adminCall(() => api.updateToken(token.id, { name: name.trim(), appIds: ids })));
      closeDialog('dlg-edit-token');
    } catch (e) {
      onError(e);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog id="dlg-edit-token">
      <Dialog.Content>
        <form style={{ display: 'grid', gap: 16 }} onSubmit={save}>
          <h2 style={{ margin: 0 }}>Edit token</h2>
          <Field>
            <Label htmlFor="tok-edit-name">Name</Label>
            <Input id="tok-edit-name" value={name} onInput={(e) => setName(e.currentTarget.value)} />
          </Field>
          <Field>
            <Label>Allowed apps</Label>
            <AppChecks apps={apps} value={ids} onChange={setIds} />
          </Field>
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Button type="button" variant="outline" onClick={() => closeDialog('dlg-edit-token')}>
              Cancel
            </Button>
            <Button type="submit" disabled={busy || !name.trim() || ids.length === 0}>
              {busy ? 'Saving…' : 'Save'}
            </Button>
          </div>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}

// any MCP client speaking Streamable HTTP: URL + bearer header; most read an mcp.json-style config
const mcpJson = (url: string, token: string) =>
  JSON.stringify({ mcpServers: { detur: { type: 'http', url, headers: { Authorization: `Bearer ${token}` } } } }, null, 2);

const codeBlock = {
  ...mono,
  margin: 0,
  padding: 12,
  whiteSpace: 'pre-wrap',
  wordBreak: 'break-all',
  background: 'hsl(var(--k-muted))',
  border: '1px solid hsl(var(--k-border))',
  borderRadius: 'var(--k-radius)',
} as const;

// how-to, agent-agnostic: placeholder token; real token only in the created dialog
function Connect({ base }: { base: string }) {
  const url = `${base}/api/mcp`;
  const snippets = [
    { label: 'MCP URL (Streamable HTTP)', value: url },
    { label: 'Header', value: 'Authorization: Bearer <token>' },
    { label: 'mcp.json (Claude Code, Cursor)', value: mcpJson(url, '<token>') },
    { label: 'HTTP API', value: `curl -H "Authorization: Bearer <token>" "${base}/api/v1/apps"` },
  ];
  return (
    <Card>
      <div style={{ display: 'grid', gap: 16, padding: 16 }}>
        <h2 style={{ margin: 0, fontSize: 20 }}>Connect</h2>
        <ol style={{ ...muted, margin: 0, paddingLeft: 20, display: 'grid', gap: 4 }}>
          <li>Create a token below and pick its apps.</li>
          <li>
            Add detur as a remote (Streamable HTTP) MCP server in your agent: URL + header works everywhere; the mcp.json block fits Claude
            Code and Cursor (VS Code uses a "servers" key, Codex its config.toml).
          </li>
          <li>Ask: "Which links drove installs last week?" Agents start with list_apps.</li>
        </ol>
        {snippets.map((s) => (
          <Field key={s.label}>
            <div style={{ ...row, justifyContent: 'space-between' }}>
              <Label>{s.label}</Label>
              <CopyButton icon value={s.value} label={`Copy ${s.label}`} />
            </div>
            <pre style={codeBlock}>{s.value}</pre>
          </Field>
        ))}
        <p style={{ ...muted, margin: 0 }}>Read-only: apps, links, rules, variants, analytics, match quality, fraud counts, health.</p>
      </div>
    </Card>
  );
}
