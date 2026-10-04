import { afterEach, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/preact';
import { DetailPage } from '../src/detail';

// Unit: an analytics response for a filter the user already left must not overwrite the current one.
// Mocked fetch, so the slow response can be released on demand (the real binary can't delay one).

const json = (body: unknown) => Promise.resolve(new Response(JSON.stringify(body), { status: 200 }));
const stats = (clicks: number) => ({
  days: [{ day: '2026-10-04', clicks, webFallbacks: 0, opens: 0, organic: 0, nonOrganic: 0 }],
  links: [],
  events: [],
});
const app = {
  id: 'app1', name: 'Stale App', apiKeyHash: 'h', iosAppId: '', androidPackage: '',
  androidCertFingerprint: '', matchThreshold: 850, matchWindowMinutes: 15,
};

const clicksTile = () =>
  parseInt(
    screen
      .getAllByText('Clicks', { exact: true })
      .map((e) => e.closest('[k=card][padding=sm]'))
      .find(Boolean)!
      .querySelector(':scope > div')!.textContent!,
    10,
  );

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it('stale analytics response: a slow 7-day reply after switching to 30 days is dropped', async () => {
  let releaseSlow!: () => void;
  vi.stubGlobal('fetch', (url: string) => {
    if (url.includes('/analytics?days=7'))
      return new Promise<Response>((r) => (releaseSlow = () => r(new Response(JSON.stringify(stats(7))))));
    if (url.includes('/analytics?days=30')) return json(stats(30));
    if (url.endsWith('/links')) return json([]);
    return json(app);
  });

  render(<DetailPage id="app1" />);
  fireEvent.change(await screen.findByLabelText('Date range'), { target: { value: '30' } });
  await waitFor(() => expect(clicksTile()).toBe(30));

  releaseSlow();
  await new Promise((r) => setTimeout(r, 50));
  expect(clicksTile()).toBe(30);
});
