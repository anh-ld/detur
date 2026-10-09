import { afterEach, expect, it } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/preact';
import { AnalyticsView, Filters, RANGE_OPTIONS } from '../src/analytics';
import { DateRangeFilter } from '../src/api';

afterEach(cleanup);

// Retention cells: share of the mark's devices; – when no cohort reached the mark in range.
it('retention by link renders rates from counts', () => {
  render(
    <AnalyticsView
      data={{
        days: [],
        links: [],
        events: [],
        sources: [],
        conversions: [],
        retention: [{ linkId: 'l1', key: 'promo', devices: 4, d1: { returned: 2, devices: 4 }, d7: { returned: 0, devices: 0 }, d30: { returned: 1, devices: 3 } }],
      }}
    />,
  );
  const tr = within(screen.getByRole('region', { name: 'Retention by link' })).getByText('promo').closest('tr')!;
  const cell = (m: string) => tr.querySelector(`[data-label="${m}"]`)!.textContent;
  expect([cell('Devices'), cell('Day 1'), cell('Day 7'), cell('Day 30')]).toEqual(['4', '50%', '–', '33%']);
  screen.getByText('No conversions yet');
});

it('filters render all date range options including custom range', () => {
  render(<Filters days={7} platform="" onDays={() => {}} onPlatform={() => {}} />);
  const select = screen.getByLabelText('Date range') as HTMLSelectElement;
  const presets = Array.from(select.options).slice(0, -1).map((o) => ({
    days: Number(o.value),
    label: o.textContent,
  }));
  expect(presets).toEqual(RANGE_OPTIONS);
  expect(select.options[select.options.length - 1].value).toBe('custom');
  expect(select.options[select.options.length - 1].textContent).toBe('Custom range');
});

it('custom date range displays inputs and applies new range', () => {
  let selectedRange: DateRangeFilter | null = null;
  render(<Filters range={{ days: 7 }} platform="" onRange={(r) => (selectedRange = r)} onPlatform={() => {}} />);
  fireEvent.change(screen.getByLabelText('Date range'), { target: { value: 'custom' } });

  const startInput = screen.getByLabelText('Start date') as HTMLInputElement;
  const endInput = screen.getByLabelText('End date') as HTMLInputElement;
  expect(startInput).toBeDefined();
  expect(endInput).toBeDefined();

  fireEvent.input(startInput, { target: { value: '2026-10-01' } });
  fireEvent.input(endInput, { target: { value: '2026-10-08' } });
  fireEvent.click(screen.getByRole('button', { name: 'Apply' }));

  expect(selectedRange).toEqual({ from: '2026-10-01', to: '2026-10-08' });
});

