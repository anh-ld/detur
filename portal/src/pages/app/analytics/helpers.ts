import { DayStat, Mark } from '../../../lib/types';
import { fmt } from '../../../lib/format';
import { fmtTzTime } from '../../../lib/tz';
import { Metric } from './constants';

export const sum = (days: DayStat[], k: Metric) => days.reduce((n, d) => n + d[k], 0);

export const fmtDay = (d: string) => {
  if (d.includes('T')) {
    return fmtTzTime(d);
  }
  return new Date(d + 'T00:00:00Z').toLocaleDateString('en', { month: 'short', day: 'numeric', timeZone: 'UTC' });
};

export const rate = (m: Mark) => (m.devices === 0 ? '–' : `${Math.round((m.returned / m.devices) * 100)}%`);

export const markTitle = (m: Mark) =>
  m.devices === 0 ? 'No cohort reached this day in range' : `${fmt(m.returned)} of ${fmt(m.devices)} devices`;
