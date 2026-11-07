import { ComponentChildren } from 'preact';
import { Button, Dialog } from 'kinu';
import { gateOpen } from '../lib/admin';
import { closeDialog, openDialog } from '../lib/dialog';
import { muted, row } from '../lib/styles';
import { ICONS, IconButton } from './icons';

// in-page confirm (replaces window.confirm). requireAdmin gates the trigger: prompt first,
// confirm opens only on successful elevation. Open/close by id: kinu Dialog.Close needs the
// Dialog.Trigger polyfill, absent here.
export function ConfirmDelete({
  title,
  body,
  onConfirm,
  id,
  requireAdmin,
  icon,
}: {
  title: string;
  body: ComponentChildren;
  onConfirm: () => void;
  id: string;
  requireAdmin?: boolean;
  icon?: boolean;
}) {
  return (
    <Dialog id={id}>
      {icon ? (
        <IconButton icon={ICONS.trash} label="Delete" danger onClick={() => (requireAdmin ? gateOpen(id) : openDialog(id))} />
      ) : (
        <Button size="sm" variant="destructive" onClick={() => (requireAdmin ? gateOpen(id) : openDialog(id))}>
          Delete
        </Button>
      )}
      <Dialog.Content>
        <div style={{ display: 'grid', gap: 16 }}>
          <h2 style={{ margin: 0 }}>{title}</h2>
          <p style={muted}>{body}</p>
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Button variant="outline" onClick={() => closeDialog(id)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                onConfirm();
                closeDialog(id);
              }}
            >
              Delete
            </Button>
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
