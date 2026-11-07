import { ComponentChildren } from 'preact';
import { Select } from 'kinu';
import { row } from '../lib/styles';
import type { DateRangeFilter } from '../lib/range';
import type { Link, Platform } from '../lib/types';
import { DateRangePicker } from './date-range-picker';

export function Filters({
  range,
  days,
  platform,
  links,
  linkId,
  children,
  onRange,
  onDays,
  onPlatform,
  onLinkId,
}: {
  range?: DateRangeFilter;
  days?: number;
  platform: Platform;
  links?: Link[];
  linkId?: string;
  children?: ComponentChildren;
  onRange?: (r: DateRangeFilter) => void;
  onDays?: (d: number) => void;
  onPlatform: (p: Platform) => void;
  onLinkId?: (id: string) => void;
}) {
  const currentRange: DateRangeFilter = range ?? { days: days ?? 7 };
  const handleRange = (r: DateRangeFilter) => {
    if (onRange) onRange(r);
    if (onDays && r.days) onDays(r.days);
  };

  return (
    <div style={{ ...row, flexWrap: 'wrap', gap: 6 }}>
      {links && onLinkId && (
        <Select aria-label="Link" value={linkId ?? ''} onChange={(e) => onLinkId(e.currentTarget.value)}>
          <option value="">All links</option>
          {links.map((l) => (
            <option key={l.id} value={l.id}>
              {l.title ? `${l.title} (/${l.key})` : `/${l.key}`}
            </option>
          ))}
        </Select>
      )}
      <Select aria-label="Platform" value={platform} onChange={(e) => onPlatform(e.currentTarget.value as Platform)}>
        <option value="">All platforms</option>
        <option value="ios">iOS</option>
        <option value="android">Android</option>
        <option value="desktop">Desktop</option>
      </Select>
      <DateRangePicker range={currentRange} onRange={handleRange} />
      {children}
    </div>
  );
}
