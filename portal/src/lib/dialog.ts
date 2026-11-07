// backdrop press closes any open dialog like Escape (kinu does it only for Dialog.Trigger;
// portal opens dialogs by id). Press must start + end outside the panel: drag-select out of a field won't close.
const onBackdrop = (e: MouseEvent) => {
  const d = e.target as HTMLElement;
  if (d.localName !== 'dialog') return false;
  const r = d.getBoundingClientRect();
  return e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom;
};
let pressedBackdrop = false;
addEventListener('pointerdown', (e) => (pressedBackdrop = onBackdrop(e)));
addEventListener('click', (e) => {
  if (pressedBackdrop && onBackdrop(e)) (e.target as HTMLDialogElement).close();
  pressedBackdrop = false;
});

export function openDialog(id: string) {
  (document.getElementById(id) as HTMLDialogElement | null)?.showModal();
}

export function closeDialog(id: string) {
  (document.getElementById(id) as HTMLDialogElement | null)?.close();
}
