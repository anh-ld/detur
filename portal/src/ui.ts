// Tiny shared UI helpers for the kinu portal.
export function closeDialog(id: string) {
  (document.getElementById(id) as HTMLDialogElement | null)?.close();
}