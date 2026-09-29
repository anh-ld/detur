import { describe, expect, it } from 'vitest';
import { parseHash } from './app';

describe('parseHash', () => {
  it('routes the apps page by default', () => {
    expect(parseHash('')).toEqual({ name: 'apps' });
    expect(parseHash('#/')).toEqual({ name: 'apps' });
  });

  it('routes the settings page', () => {
    expect(parseHash('#/settings')).toEqual({ name: 'settings' });
  });

  it('decodes a detail route id', () => {
    expect(parseHash('#/apps/abc123')).toEqual({ name: 'detail', id: 'abc123' });
    expect(parseHash('#/apps/a%20b')).toEqual({ name: 'detail', id: 'a b' });
  });

  it('does not throw on a malformed percent-encoded id', () => {
    expect(() => parseHash('#/apps/%zz')).not.toThrow();
    expect(parseHash('#/apps/%zz')).toEqual({ name: 'apps' });
  });
});