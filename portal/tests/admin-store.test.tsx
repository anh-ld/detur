import { expect, it } from 'bun:test';
import { render } from 'preact'; // raw render: effects are NOT flushed (testing-library's act would flush them)
import { setAdminAvailable, setElevated, useAdmin } from '../src/lib/admin';

function Probe() {
  const { locked } = useAdmin();
  return <p>{locked ? 'LOCKED' : 'OPEN'}</p>;
}

it('an admin state change between render and effect-subscribe is not lost', async () => {
  setAdminAvailable(true);
  setElevated(false);
  const host = document.createElement('div');
  document.body.append(host);
  render(<Probe />, host);
  expect(host.textContent).toBe('LOCKED');
  setElevated(true, new Date(Date.now() + 3600_000).toISOString());
  await new Promise((r) => setTimeout(r, 500));
  expect(host.textContent).toBe('OPEN');
  render(null, host);
  setElevated(false);
  setAdminAvailable(false);
});
