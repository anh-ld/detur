import { cloneElement, JSX } from 'preact';
import { useEffect, useRef, useState } from 'preact/hooks';
import { Alert, Button, Dialog } from 'kinu';
import { api } from '../../../lib/api';
import { Link, LinkRule, RedirectProfile, SocialPreset } from '../../../lib/types';
import { closeDialog } from '../../../lib/dialog';
import { Icon, ICONS } from '../../../components/icons';
import { Loading } from '../../../components/page';
import { muted, row } from '../../../lib/styles';
import { ShortLink } from '../../../components/short-link';
import { LinkDraft, draftDirty, draftError, draftFrom, linkPayload } from './link-draft';
import { LinkFields } from './link-fields';
import { blankRule } from './rule-draft';
import { RuleEditor } from './rule-editor';
import { RuleList } from './rule-list';
import { useLinkRules } from './use-link-rules';

// One editor per link: target, redirects, tags, rules. Stats live in Analytics.
export function LinkEditor({
  appId,
  link,
  linkBase = '',
  existingTags,
  profiles,
  socialPresets,
  trigger,
  onSaved,
}: {
  appId: string;
  link: Link;
  linkBase?: string;
  existingTags: string[];
  profiles: RedirectProfile[];
  socialPresets: SocialPreset[];
  trigger: JSX.Element;
  onSaved?: () => void;
}) {
  const dialogId = `dlg-edit-link-${link.id}`;
  const fid = (s: string) => `${dialogId}-${s}`;
  const [draft, setDraft] = useState<LinkDraft>(() => draftFrom(link));
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState('');
  const [success, setSuccess] = useState('');
  const [editing, setEditing] = useState<{ index: number; rule: LinkRule } | null>(null);
  const r = useLinkRules(appId, link.id, setErr);

  const baseDirty = draftDirty(draft, draftFrom(link));
  const changed = r.dirty || baseDirty;
  const canAdd = !saving && r.loaded;

  // Close (Escape, backdrop, button) keeps unsaved edits; reopening shows them until Save or Discard.
  const discard = () => {
    setEditing(null);
    setSuccess('');
    setDraft(draftFrom(link));
    r.load();
  };
  const startEdit = (index: number) => {
    setEditing({ index, rule: index === -1 ? blankRule() : r.rules[index] });
    setErr('');
  };

  const save = async () => {
    setErr('');
    const bad = draftError(draft, 'edit');
    if (bad) return setErr(bad);
    setSaving(true);
    setSuccess('');
    try {
      if (baseDirty) setDraft(draftFrom(await api.updateLink(link.id, linkPayload(draft))));
      if (r.dirty) await r.save();
      setSuccess('Link saved');
      onSaved?.();
      setTimeout(() => setSuccess(''), 2500);
    } catch (e) {
      setErr(String(e));
    } finally {
      setSaving(false);
    }
  };

  // After Done/Cancel, focus Save if enabled, else Close; otherwise focus stays on the button that replaced the clicked one.
  const wasEditing = useRef(false);
  useEffect(() => {
    if (wasEditing.current && !editing) {
      const save = document.getElementById(fid('save')) as HTMLButtonElement | null;
      (save && !save.disabled ? save : document.getElementById(fid('close')))?.focus();
    }
    wasEditing.current = !!editing;
  }, [editing]);

  return (
    <Dialog id={dialogId}>
      <Dialog.Trigger>{cloneElement(trigger, { onClick: () => !changed && !editing && discard() })}</Dialog.Trigger>
      <Dialog.Content class="rules-drawer-content">
        <div style={{ display: 'grid', gap: 20 }}>
          <div style={{ display: 'grid', gap: 4 }}>
            <h2 style={{ margin: 0 }}>Edit link</h2>
            <ShortLink base={linkBase} linkKey={link.key} />
          </div>
          {err && (
            <Alert variant="destructive" role="alert">
              {err}
            </Alert>
          )}
          {success && <Alert variant="success">{success}</Alert>}
          {r.loading && <Loading />}

          {!editing && (
            <div style={{ display: 'grid', gap: 20 }}>
              <LinkFields
                id={dialogId}
                draft={draft}
                setDraft={setDraft}
                existingTags={existingTags}
                profiles={profiles}
                socialPresets={socialPresets}
                linkBase={linkBase}
              />
              <div class="form-section">
                <h3>Rules & splits</h3>
                <p style={muted}>Checked top to bottom. The first rule that matches wins; anything else uses the target above.</p>
              </div>
            </div>
          )}

          {!r.loading && editing && (
            <RuleEditor
              idp={dialogId}
              link={link}
              rules={r.rules}
              index={editing.index}
              initial={editing.rule}
              setErr={setErr}
              onDone={(next) => {
                r.change(next);
                setEditing(null);
              }}
              onCancel={() => setEditing(null)}
            />
          )}

          {!r.loading && !editing && (
            <RuleList
              rules={r.rules}
              link={link}
              busy={saving}
              canAdd={canAdd}
              onAdd={() => startEdit(-1)}
              onEdit={startEdit}
              onMove={r.move}
              onDelete={r.remove}
            />
          )}

          {!editing && (
            <div class="dialog-actions">
              {r.rules.length > 0 && !r.loading && (
                <Button variant="outline" disabled={!canAdd} onClick={() => startEdit(-1)} style={{ marginInlineEnd: 'auto' }}>
                  <Icon d={ICONS.plus} />
                  Add rule
                </Button>
              )}
              {changed && !saving && (
                <Button variant="ghost" onClick={discard}>
                  Discard
                </Button>
              )}
              <Button id={fid('close')} variant="outline" disabled={saving} onClick={() => closeDialog(dialogId)}>
                Close
              </Button>
              <Button id={fid('save')} disabled={r.loading || saving || !changed} aria-busy={saving} onClick={save}>
                {saving ? 'Saving…' : 'Save'}
              </Button>
            </div>
          )}
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
