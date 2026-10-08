import { ComponentChildren } from 'preact';
import { useEffect, useState } from 'preact/hooks';
import { Button, Dialog } from 'kinu';

// Shared layout bits: kinu token colors, no layout primitives.
export const muted = { color: 'hsl(var(--k-muted-foreground))', fontSize: 14, margin: 0 };
export const mono = { fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace', fontSize: 13 };
export const row = { display: 'flex', gap: 8, alignItems: 'center' };

export const copyText = async (v: string) => {
  await navigator.clipboard.writeText(v);
};

// Ghost copy button: flashes "Copied" 1.5s after copy.
export function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (!copied) return;
    const t = setTimeout(() => setCopied(false), 1500);
    return () => clearTimeout(t);
  }, [copied]);
  return (
    <Button
      size="sm"
      variant="ghost"
      onClick={async () => {
        await copyText(value);
        setCopied(true);
      }}
    >
      {copied ? 'Copied' : label}
    </Button>
  );
}

export function openDialog(id: string) {
  (document.getElementById(id) as HTMLDialogElement | null)?.showModal();
}

export function closeDialog(id: string) {
  (document.getElementById(id) as HTMLDialogElement | null)?.close();
}

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: ComponentChildren;
  description?: ComponentChildren;
  actions?: ComponentChildren;
}) {
  return (
    <div style={{ ...row, justifyContent: 'space-between', margin: '32px 0 24px', flexWrap: 'wrap', gap: 16 }}>
      <div style={{ display: 'grid', gap: 6 }}>
        <h1 style={{ margin: 0, fontSize: 28 }}>{title}</h1>
        {description && <p style={muted}>{description}</p>}
      </div>
      {actions && <div style={row}>{actions}</div>}
    </div>
  );
}

// Delete button: in-page confirm step (replaces window.confirm).
export function ConfirmDelete({
  title,
  body,
  onConfirm,
}: {
  title: string;
  body: ComponentChildren;
  onConfirm: () => void;
}) {
  return (
    <Dialog>
      <Dialog.Trigger>
        <Button size="sm" variant="destructive">
          Delete
        </Button>
      </Dialog.Trigger>
      <Dialog.Content>
        <div style={{ display: 'grid', gap: 16 }}>
          <h2 style={{ margin: 0 }}>{title}</h2>
          <p style={muted}>{body}</p>
          <div style={{ ...row, justifyContent: 'flex-end' }}>
            <Dialog.Close>
              <Button variant="outline">Cancel</Button>
            </Dialog.Close>
            <Dialog.Close>
              <Button variant="destructive" onClick={onConfirm}>
                Delete
              </Button>
            </Dialog.Close>
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
