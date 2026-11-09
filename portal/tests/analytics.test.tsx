import { afterEach, expect, it } from 'bun:test';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/preact';
import { AnalyticsView } from '../src/pages/app/analytics';
import { Filters } from '../src/components/filters';
import { TzToggle } from '../src/components/tz-toggle';
import { DateRangeFilter } from '../src/lib/range';
import { buildAppHash, parseHash } from '../src/lib/route';

afterEach(cleanup);

it('retention by link renders rates from counts', () => {
  render(
    <AnalyticsView
      data={{
        days: [],
        links: [],
        events: [],
        sources: [],
        conversions: [],
        retention: [
          {
            linkId: 'l1',
            key: 'promo',
            devices: 4,
            d1: { returned: 2, devices: 4 },
            d7: { returned: 0, devices: 0 },
            d30: { returned: 1, devices: 3 },
          },
        ],
      }}
    />,
  );
  const tr = within(screen.getByRole('region', { name: 'Retention by link' }))
    .getByText('promo')
    .closest('tr')!;
  const cell = (m: string) => tr.querySelector(`[data-label="${m}"]`)!.textContent;
  expect([cell('Devices'), cell('Day 1'), cell('Day 7'), cell('Day 30')]).toEqual(['4', '50%', '–', '33%']);
  screen.getByText('No conversions yet');
});

it('selecting sub-day window applies window filter', () => {
  let selectedRange: DateRangeFilter | null = null;
  render(<Filters range={{ days: 7 }} platform="" onRange={(r) => (selectedRange = r)} onPlatform={() => {}} />);
  fireEvent.change(screen.getByLabelText('Date range'), { target: { value: '15m' } });
  expect(selectedRange).toEqual({ window: '15m', linkId: undefined });

  fireEvent.change(screen.getByLabelText('Date range'), { target: { value: '1h' } });
  expect(selectedRange).toEqual({ window: '1h', linkId: undefined });
});

it('filters render link selector and calls onLinkId', () => {
  let selectedLink = '';
  const testLinks = [
    { id: 'l1', appId: 'a1', key: 'promo', url: '/p', title: 'John Promo', ios: '', android: '', fallbackUrl: '', ogTitle: '', ogDescription: '', ogImage: '', expiresAt: '', expiredUrl: '' },
    { id: 'l2', appId: 'a1', key: 'summer', url: '/s', title: '', ios: '', android: '', fallbackUrl: '', ogTitle: '', ogDescription: '', ogImage: '', expiresAt: '', expiredUrl: '' },
  ];
  render(
    <Filters
      range={{ days: 7 }}
      platform=""
      links={testLinks}
      linkId="l1"
      onRange={() => {}}
      onPlatform={() => {}}
      onLinkId={(id) => (selectedLink = id)}
    />,
  );
  const select = screen.getByLabelText('Link') as HTMLSelectElement;
  expect(select.value).toBe('l1');
  expect(select.options[1].textContent).toBe('John Promo (/promo)');
  expect(select.options[2].textContent).toBe('/summer');

  fireEvent.change(select, { target: { value: 'l2' } });
  expect(selectedLink).toBe('l2');
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

it('app hash round-trips filters and ignores invalid ones', () => {
  const rt = (range: DateRangeFilter) => {
    const r = parseHash(buildAppHash('a1', 'analytics', { linkId: 'l1', platform: 'ios', range }));
    return r.name === 'app' ? [r.linkId, r.platform, r.range] : null;
  };
  expect(rt({ window: '1h' })).toEqual(['l1', 'ios', { window: '1h' }]);
  expect(rt({ days: 30 })).toEqual(['l1', 'ios', { days: 30 }]);
  expect(rt({ from: '2026-01-01', to: '2026-01-31' })).toEqual(['l1', 'ios', { from: '2026-01-01', to: '2026-01-31' }]);
  const bad = parseHash('#/apps/a1/analytics?platform=palm&window=2h&range=15m');
  expect(bad).toMatchObject({ platform: undefined, range: undefined });
});

it('time zone toggle switches sub-day labels between UTC and browser time and persists across remount', () => {
  const data = { days: [{ day: '2026-01-01T10:00:00Z', clicks: 3, webFallbacks: 0, opens: 0, organic: 0, nonOrganic: 0 }], links: [], events: [], sources: [], conversions: [], retention: [] } as any;
  const local = new Date('2026-01-01T10:00:00Z');
  const browser = new Date(local.getTime() - local.getTimezoneOffset() * 60000).toISOString().slice(11, 16);
  const ui = () => (
    <>
      <TzToggle />
      <AnalyticsView data={data} range={{ window: '1h' }} />
    </>
  );
  render(ui());
  fireEvent.click(screen.getByLabelText('Time zone'));
  expect(screen.getAllByText(browser).length).toBeGreaterThan(0);
  cleanup();
  render(ui());
  expect(screen.getByLabelText('Time zone').textContent).toBe('Local');
  expect(screen.getAllByText(browser).length).toBeGreaterThan(0);
  fireEvent.click(screen.getByLabelText('Time zone'));
  expect(screen.getAllByText('10:00').length).toBeGreaterThan(0);
});
