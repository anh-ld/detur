import { afterEach, expect, it } from 'vitest';
import { cleanup, render, screen, within } from '@testing-library/preact';
import { AnalyticsView } from '../src/analytics';

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
