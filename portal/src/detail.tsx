import { JSX } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { Button, Dialog, Input, Label } from 'kinu';
import { api, App, Link, Readout } from './api';

function closeDialog(id: string) {
  (document.getElementById(id) as HTMLDialogElement | null)?.close();
}

interface LinkDraft {
  key: string;
  url: string;
  ios: string;
  android: string;
  fallbackUrl: string;
  threshold: string;
  windowMinutes: string;
}

const emptyDraft = (): LinkDraft => ({
  key: '',
  url: '',
  ios: '',
  android: '',
  fallbackUrl: '',
  threshold: '850',
  windowMinutes: '15',
});

const draftFrom = (l: Link): LinkDraft => ({
  key: l.key,
  url: l.url,
  ios: l.ios,
  android: l.android,
  fallbackUrl: l.fallbackUrl,
  threshold: String(l.threshold),
  windowMinutes: String(l.windowMinutes),
});

export function DetailPage({ id }: { id: string }) {
  const [app, setApp] = useState<App | null>(null);
  const [links, setLinks] = useState<Link[] | null>(null);
  const [readout, setReadout] = useState<Readout | null>(null);
  const [error, setError] = useState('');

  const loadAll = () => {
    setError('');
    api
      .listApps()
      .then((apps) => setApp(apps.find((a) => a.id === id) ?? null))
      .catch((e) => setError(String(e)));
    api
      .listLinks(id)
      .then(setLinks)
      .catch((e) => setError(String(e)));
    api
      .getReadout(id)
      .then(setReadout)
      .catch((e) => setError(String(e)));
  };
  useEffect(loadAll, [id]);

  if (app === null && error === '') return <p class="hint">Loading…</p>;
  if (app === null)
    return (
      <p class="hint">
        App not found — <a href="#/">back to apps</a>.
      </p>
    );

  const del = async (l: Link) => {
    if (!confirm(`Delete link "${l.key}"?`)) return;
    setError('');
    try {
      await api.deleteLink(l.id);
      loadAll();
    } catch (e) {
      setError(String(e));
    }
  };

  return (
    <div>
      <div class="page-head">
        <h1>
          <a href="#/">Apps</a> / {app.name}
        </h1>
        <div class="row">
          <Button size="sm" variant="outline" onClick={loadAll}>
            Refresh
          </Button>
          <LinkDialog
            key={'create' + app.id}
            appId={app.id}
            link={null}
            onSaved={loadAll}
            trigger={<Button>New link</Button>}
          />
        </div>
      </div>
      {error && <p class="error">{error}</p>}

      <div class="readout">
        <div class="stat">
          <div class="num">{readout ? readout.clicks : '–'}</div>
          <div class="lbl">clicks</div>
        </div>
        <div class="stat">
          <div class="num">{readout ? readout.nonOrganic : '–'}</div>
          <div class="lbl">non-organic installs</div>
        </div>
        <div class="stat">
          <div class="num">{readout ? readout.organic : '–'}</div>
          <div class="lbl">organic installs</div>
        </div>
      </div>

      {links === null ? (
        <p class="hint">Loading links…</p>
      ) : links.length === 0 ? (
        <p class="hint">No links yet — create one.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Key</th>
              <th>URL</th>
              <th>iOS</th>
              <th>Android</th>
              <th>Fallback</th>
              <th>Threshold</th>
              <th>Window</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {links.map((l) => (
              <tr key={l.id}>
                <td class="mono">{l.key}</td>
                <td>
                  <a href={l.url} target="_blank" rel="noreferrer">
                    {l.url}
                  </a>
                </td>
                <td class="hint">{l.ios || '—'}</td>
                <td class="hint">{l.android || '—'}</td>
                <td class="hint">{l.fallbackUrl || '—'}</td>
                <td>{l.threshold}</td>
                <td>{l.windowMinutes}</td>
                <td class="actions">
                  <LinkDialog
                    key={'edit' + l.id}
                    appId={app.id}
                    link={l}
                    onSaved={loadAll}
                    trigger={
                      <Button size="sm" variant="secondary">
                        Edit
                      </Button>
                    }
                  />
                  <Button size="sm" variant="destructive" onClick={() => del(l)}>
                    Delete
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

function LinkDialog({
  appId,
  link,
  onSaved,
  trigger,
}: {
  appId: string;
  link: Link | null;
  onSaved: () => void;
  trigger: JSX.Element;
}) {
  const [draft, setDraft] = useState<LinkDraft>(link ? draftFrom(link) : emptyDraft());
  const [err, setErr] = useState('');
  const id = link ? `dlg-edit-link-${link.id}` : 'dlg-create-link';

  const save = async () => {
    setErr('');
    const threshold = Number(draft.threshold);
    const windowMinutes = Number(draft.windowMinutes);
    if (!draft.key.trim() || !draft.url.trim()) {
      setErr('key and url are required');
      return;
    }
    if (!Number.isFinite(threshold) || !Number.isFinite(windowMinutes)) {
      setErr('threshold and window must be numbers');
      return;
    }
    const payload = {
      url: draft.url.trim(),
      ios: draft.ios,
      android: draft.android,
      fallbackUrl: draft.fallbackUrl,
      threshold,
      windowMinutes,
    };
    try {
      if (link) {
        await api.updateLink(link.id, payload);
      } else {
        await api.createLink(appId, { key: draft.key.trim(), ...payload });
      }
      closeDialog(id);
      onSaved();
    } catch (e) {
      setErr(String(e));
    }
  };

  return (
    <Dialog id={id}>
      <Dialog.Trigger>{trigger}</Dialog.Trigger>
      <Dialog.Content>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            save();
          }}
        >
          <h2>{link ? 'Edit link' : 'Create link'}</h2>
          <Label htmlFor={`${id}-key`}>Key</Label>
          <Input
            id={`${id}-key`}
            class="mono"
            value={draft.key}
            onInput={(e) => setDraft({ ...draft, key: e.currentTarget.value })}
            placeholder="summer-sale"
            disabled={!!link}
          />
          <Label htmlFor={`${id}-url`}>Destination URL</Label>
          <Input
            id={`${id}-url`}
            value={draft.url}
            onInput={(e) => setDraft({ ...draft, url: e.currentTarget.value })}
            placeholder="https://example.com/product"
          />
          <Label htmlFor={`${id}-ios`}>iOS URL (store page, optional)</Label>
          <Input
            id={`${id}-ios`}
            value={draft.ios}
            onInput={(e) => setDraft({ ...draft, ios: e.currentTarget.value })}
            placeholder="https://apps.apple.com/app/id123"
          />
          <Label htmlFor={`${id}-android`}>Android URL (store page, optional)</Label>
          <Input
            id={`${id}-android`}
            value={draft.android}
            onInput={(e) => setDraft({ ...draft, android: e.currentTarget.value })}
            placeholder="https://play.google.com/store/apps/details?id=com.example"
          />
          <Label htmlFor={`${id}-fb`}>Fallback URL (desktop, optional)</Label>
          <Input
            id={`${id}-fb`}
            value={draft.fallbackUrl}
            onInput={(e) => setDraft({ ...draft, fallbackUrl: e.currentTarget.value })}
            placeholder="https://example.com"
          />
          <div class="row">
            <div>
              <Label htmlFor={`${id}-th`}>Threshold (700–1200)</Label>
              <Input
                id={`${id}-th`}
                type="number"
                value={draft.threshold}
                onInput={(e) => setDraft({ ...draft, threshold: e.currentTarget.value })}
              />
            </div>
            <div>
              <Label htmlFor={`${id}-win`}>Window (min, 5–180)</Label>
              <Input
                id={`${id}-win`}
                type="number"
                value={draft.windowMinutes}
                onInput={(e) => setDraft({ ...draft, windowMinutes: e.currentTarget.value })}
              />
            </div>
          </div>
          {err && <p class="error">{err}</p>}
          <div class="row">
            <Button type="submit">{link ? 'Save' : 'Create'}</Button>
            <Dialog.Close>
              <Button variant="outline">Cancel</Button>
            </Dialog.Close>
          </div>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}