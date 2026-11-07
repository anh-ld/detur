import { DayStat, Mark } from '../../../lib/types';
import { fmt } from '../../../lib/format';
import { Metric } from './constants';

export const sum = (days: DayStat[], k: Metric) => days.reduce((n, d) => n + d[k], 0);

// hourly buckets arrive as ISO timestamps, daily ones as YYYY-MM-DD
export const fmtDay = (d: string) => {
  if (d.includes('T')) {
    return new Date(d).toLocaleTimeString('en', { hour: '2-digit', minute: '2-digit', hour12: false, timeZone: 'UTC' });
  }
  return new Date(d + 'T00:00:00Z').toLocaleDateString('en', { month: 'short', day: 'numeric', timeZone: 'UTC' });
};

// share of mark's devices that returned; – = no cohort reached the mark in range
export const rate = (m: Mark) => (m.devices === 0 ? '–' : `${Math.round((m.returned / m.devices) * 100)}%`);

export const markTitle = (m: Mark) =>
  m.devices === 0 ? 'No cohort reached this day in range' : `${fmt(m.returned)} of ${fmt(m.devices)} devices`;
