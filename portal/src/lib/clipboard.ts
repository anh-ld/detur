// navigator.clipboard is undefined outside a secure context (http://<LAN-IP>:8081): fall back to execCommand
export const copyText = async (v: string) => {
  if (navigator.clipboard) return navigator.clipboard.writeText(v);
  const ta = document.createElement('textarea');
  ta.value = v;
  ta.setAttribute('readonly', '');
  ta.style.position = 'fixed';
  ta.style.opacity = '0';
  // inside the open modal: showModal() makes everything outside it inert, so a body child can't hold the selection
  (document.querySelector('dialog[open]') ?? document.body).appendChild(ta);
  const prev = document.activeElement as HTMLElement | null;
  ta.focus();
  ta.select();
  const ok = document.execCommand('copy');
  ta.remove();
  prev?.focus();
  if (!ok) throw new Error('copy failed');
};
