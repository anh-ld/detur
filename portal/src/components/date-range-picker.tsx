import { useEffect, useState } from 'preact/hooks';
import { Button, Input, Select } from 'kinu';
import { muted, row } from '../lib/styles';
import { isWindow, type DateRangeFilter, type WindowId } from '../lib/range';

export type RangePreset =
  { window: WindowId; label: string; days?: never } | { days: number; label: string; window?: never };

export const RANGE_OPTIONS: readonly RangePreset[] = [
  { window: '15m', label: 'Last 15 minutes' },
  { window: '1h', label: 'Last 1 hour' },
  { window: '3h', label: 'Last 3 hours' },
  { window: '12h', label: 'Last 12 hours' },
  { days: 1, label: 'Last 1 day' },
  { days: 7, label: 'Last 7 days' },
  { days: 14, label: 'Last 14 days' },
  { days: 30, label: 'Last 30 days' },
  { days: 60, label: 'Last 60 days' },
  { days: 90, label: 'Last 90 days' },
  { days: 180, label: 'Last 180 days' },
  { days: 365, label: 'Last 365 days' },
] as const;

const toDateStr = (d: Date) => d.toISOString().slice(0, 10);
const todayStr = () => toDateStr(new Date());
const daysAgoStr = (n: number) => {
  const d = new Date();
  d.setDate(d.getDate() - (n - 1));
  return toDateStr(d);
};

export function DateRangePicker({
  ariaLabel = 'Date range',
  range,
  onRange,
  allowWindows = true,
}: {
  ariaLabel?: string;
  allowWindows?: boolean;
  range: DateRangeFilter;
  onRange: (r: DateRangeFilter) => void;
}) {
  const [customMode, setCustomMode] = useState(Boolean(range.from && range.to));
  const [from, setFrom] = useState(range.from ?? daysAgoStr(range.days ?? 7));
  const [to, setTo] = useState(range.to ?? todayStr());

  useEffect(() => {
    if (range.from && range.to) {
      setCustomMode(true);
      setFrom(range.from);
      setTo(range.to);
    } else {
      setCustomMode(false);
    }
  }, [range.days, range.window, range.from, range.to]);

  const selectVal = customMode ? 'custom' : range.window ? range.window : String(range.days ?? 7);

  const handleSelect = (val: string) => {
    if (val === 'custom') {
      setCustomMode(true);
      const f = from || daysAgoStr(range.days ?? 7);
      const t = to || todayStr();
      setFrom(f);
      setTo(t);
      onRange({ from: f, to: t, linkId: range.linkId });
    } else if (isWindow(val)) {
      setCustomMode(false);
      onRange({ window: val, linkId: range.linkId });
    } else {
      setCustomMode(false);
      onRange({ days: Number(val), linkId: range.linkId });
    }
  };

  const handleApply = (e?: Event) => {
    e?.preventDefault();
    if (from && to && from <= to) {
      onRange({ from, to, linkId: range.linkId });
    }
  };

  return (
    <div style={{ ...row, flexWrap: 'wrap', gap: 6 }}>
      <Select aria-label={ariaLabel} value={selectVal} onChange={(e) => handleSelect(e.currentTarget.value)}>
        {RANGE_OPTIONS.filter((o) => allowWindows || !o.window).map((o) => (
          <option key={o.window ?? o.days} value={o.window ?? String(o.days)}>
            {o.label}
          </option>
        ))}
        <option value="custom">Custom range</option>
      </Select>
      {customMode && (
        <form onSubmit={handleApply} style={{ ...row, gap: 6, margin: 0, flexWrap: 'wrap' }}>
          <Input
            type="date"
            aria-label="Start date"
            value={from}
            max={to || todayStr()}
            onInput={(e) => setFrom(e.currentTarget.value)}
            style={{ width: 'auto', padding: '4px 8px', fontSize: 13 }}
          />
          <span style={muted}>to</span>
          <Input
            type="date"
            aria-label="End date"
            value={to}
            min={from}
            max={todayStr()}
            onInput={(e) => setTo(e.currentTarget.value)}
            style={{ width: 'auto', padding: '4px 8px', fontSize: 13 }}
          />
          <Button type="submit" size="sm" variant="secondary" disabled={!from || !to || from > to}>
            Apply
          </Button>
        </form>
      )}
    </div>
  );
}
