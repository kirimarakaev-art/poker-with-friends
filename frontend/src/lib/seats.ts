// A point: either fractions of the felt (seatPoint) or pixels inside it.
export interface Point {
  x: number;
  y: number;
}

export const CENTER: Point = { x: 0.5, y: 0.5 };

// seatPoint places seats evenly on the rail (the edge of the oval), turned so
// that the viewing player always sits at the bottom centre.
export function seatPoint(seat: number, total: number, you: number): Point {
  const angle = Math.PI / 2 + ((seat - you) / total) * 2 * Math.PI;
  return {
    x: 0.5 + 0.5 * Math.cos(angle),
    y: 0.5 + 0.5 * Math.sin(angle),
  };
}

// stepToward moves distance pixels from `from` towards `to` (never past it).
export function stepToward(from: Point, to: Point, distance: number): Point {
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  const len = Math.hypot(dx, dy);
  if (len === 0) return from;
  const t = Math.min(1, distance / len);
  return { x: from.x + dx * t, y: from.y + dy * t };
}

// sideStep moves distance pixels sideways (to the right when facing `to`).
export function sideStep(from: Point, to: Point, at: Point, distance: number): Point {
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  const len = Math.hypot(dx, dy) || 1;
  return { x: at.x - (dy / len) * distance, y: at.y + (dx / len) * distance };
}

export interface HalfSize {
  hw: number; // half width
  hh: number; // half height
}

// clearDistance is how far from a box's centre, walking towards `to`, an item
// must be placed so that the item lies fully outside the box, plus a gap.
export function clearDistance(from: Point, to: Point, box: HalfSize, item: HalfSize, gap: number): number {
  const len = Math.hypot(to.x - from.x, to.y - from.y);
  if (len === 0) return 0;
  const ux = Math.abs(to.x - from.x) / len;
  const uy = Math.abs(to.y - from.y) / len;
  const toEdge = (s: HalfSize) => Math.min(ux > 0 ? s.hw / ux : Infinity, uy > 0 ? s.hh / uy : Infinity);
  return toEdge(box) + toEdge(item) + gap;
}
