import { ComponentChildren } from 'preact';
import { Select } from 'kinu';
import { row } from '../lib/styles';
import type { DateRangeFilter } from '../lib/range';
import { PLATFORMS, type Link, type Platform } from '../lib/types';
import { DateRangePicker } from './date-range-picker';

export function Filters({
  range,
  platform,
  links,
  linkId,
  children,
  onRange,
  onPlatform,
  onLinkId,
}: {
  range: DateRangeFilter;
  platform: Platform;
  links?: Link[];
  linkId?: string;
  children?: ComponentChildren;
  onRange: (r: DateRangeFilter) => void;
  onPlatform: (p: Platform) => void;
  onLinkId?: (id: string) => void;
}) {
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
        {PLATFORMS.map((p) => (
          <option key={p.id} value={p.id}>
            {p.label}
          </option>
        ))}
      </Select>
      <DateRangePicker range={range} onRange={onRange} />
      {children}
    </div>
  );
}
