import { afterEach, describe, expect, it, vi } from 'vitest';
import { req } from './api';

const stubFetch = (res: { ok: boolean; status: number; body?: unknown }) => {
  vi.stubGlobal('fetch', vi.fn(async () => ({
    ok: res.ok,
    status: res.status,
    statusText: String(res.status),
    json: async () => res.body,
    text: async () => (typeof res.body === 'string' ? res.body : JSON.stringify(res.body ?? '')),
  })));
};

describe('req', () => {
  afterEach(() => vi.unstubAllGlobals());

  it('returns parsed JSON on success', async () => {
    stubFetch({ ok: true, status: 200, body: { name: 'x' } });
    await expect(req('GET', '/api/apps')).resolves.toEqual({ name: 'x' });
  });

  it('returns undefined on 204', async () => {
    stubFetch({ ok: true, status: 204 });
    await expect(req('DELETE', '/api/apps/1')).resolves.toBeUndefined();
  });

  it('throws with status and body text on failure', async () => {
    stubFetch({ ok: false, status: 400, body: 'name is required' });
    await expect(req('POST', '/api/apps', { name: '' })).rejects.toThrow(/400 name is required/);
  });
});