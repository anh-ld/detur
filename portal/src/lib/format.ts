import { fmtTzDate } from './tz';
export const fmt = (n: number) => n.toLocaleString('en');
export const fmtDate = fmtTzDate;
export const hostOf = (url: string) => url.replace(/^https?:\/\//, '');
