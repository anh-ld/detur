import { ComponentChildren } from 'preact';
import { Button, Dialog } from 'kinu';
import { useAdmin } from '../lib/admin';
import { closeDialog, openDialog } from '../lib/dialog';
import { muted, row } from '../lib/styles';
import { AdminButton, lockedProps } from './admin';
import { ICONS, IconButton } from './icons';

// requireAdmin: trigger is disabled (lock) until admin is entered. Open/close by id: kinu Dialog.Close needs the
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
  const locked = useAdmin().locked && !!requireAdmin;
  return (
    <Dialog id={id}>
      {icon ? (
        <IconButton
          icon={ICONS.trash}
          label="Delete"
          danger
          onClick={() => openDialog(id)}
          {...lockedProps(locked)}
        />
      ) : requireAdmin ? (
        <AdminButton size="sm" variant="destructive" onClick={() => openDialog(id)}>
          Delete
        </AdminButton>
      ) : (
        <Button size="sm" variant="destructive" onClick={() => openDialog(id)}>
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
