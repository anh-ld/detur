// Ranges mirrored from server/internal/match/match.go (ValidateSettings).
export const THRESHOLD = { min: 700, max: 1200 };
export const WINDOW = { min: 5, max: 180 };

export function inRange(v: number, range: { min: number; max: number }): boolean {
  return v >= range.min && v <= range.max;
}