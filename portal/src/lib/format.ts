export const fmt = (n: number) => n.toLocaleString('en');
export const fmtDate = (iso: string) => new Date(iso).toLocaleDateString('en', { month: 'short', day: 'numeric', year: 'numeric' });
export const hostOf = (url: string) => url.replace(/^https?:\/\//, '');
