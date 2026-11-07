import { cloneElement, JSX } from 'preact';
import { useState } from 'preact/hooks';
import { Alert, Button, Dialog } from 'kinu';
import { RedirectProfile, SocialPreset } from '../../lib/types';
import { api } from '../../lib/api';
import { closeDialog } from '../../lib/dialog';
import { LinkDraft, draftError, emptyDraft, linkPayload } from './links/link-draft';
import { LinkFields } from './links/link-fields';

const id = 'dlg-create-link';

export function LinkDialog({
  appId,
  linkBase,
  existingTags,
  profiles,
  socialPresets,
  onSaved,
  trigger,
}: {
  appId: string;
  linkBase: string;
  existingTags: string[];
  profiles: RedirectProfile[];
  socialPresets: SocialPreset[];
  onSaved: () => void;
  trigger: JSX.Element;
}) {
  const [draft, setDraft] = useState<LinkDraft>(emptyDraft());
  const [err, setErr] = useState('');

  const save = async () => {
    const bad = draftError(draft, 'create');
    setErr(bad ?? '');
    if (bad) return;
    try {
      await api.createLink(appId, { key: draft.key.trim(), ...linkPayload(draft) });
      closeDialog(id);
      onSaved();
    } catch (e) {
      setErr(String(e));
    }
  };

  return (
    <Dialog id={id}>
      <Dialog.Trigger>
        {cloneElement(trigger, {
          onClick: () => {
            setDraft(emptyDraft());
            setErr('');
          },
        })}
      </Dialog.Trigger>
      <Dialog.Content>
        <form
          style={{ display: 'grid', gap: 16 }}
          onSubmit={(e) => {
            e.preventDefault();
            save();
          }}
        >
          <h2 style={{ margin: 0 }}>Create link</h2>
          {err && <Alert variant="destructive">{err}</Alert>}
          <LinkFields
            id={id}
            draft={draft}
            setDraft={setDraft}
            existingTags={existingTags}
            profiles={profiles}
            socialPresets={socialPresets}
            linkBase={linkBase}
            showKey
          />
          <div class="dialog-actions">
            <Dialog.Close>
              <Button variant="outline">Cancel</Button>
            </Dialog.Close>
            <Button type="submit">Create</Button>
          </div>
        </form>
      </Dialog.Content>
    </Dialog>
  );
}
