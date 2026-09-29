import { describe, expect, it } from 'vitest';
import { inRange, THRESHOLD, WINDOW } from './matching';

describe('matching ranges', () => {
  it('mirrors the server constants', () => {
    expect(THRESHOLD.min).toBe(700);
    expect(THRESHOLD.max).toBe(1200);
    expect(WINDOW.min).toBe(5);
    expect(WINDOW.max).toBe(180);
  });

  it('accepts boundaries and rejects out-of-range values', () => {
    expect(inRange(700, THRESHOLD)).toBe(true);
    expect(inRange(1200, THRESHOLD)).toBe(true);
    expect(inRange(699, THRESHOLD)).toBe(false);
    expect(inRange(5, WINDOW)).toBe(true);
    expect(inRange(180, WINDOW)).toBe(true);
    expect(inRange(181, WINDOW)).toBe(false);
  });
});