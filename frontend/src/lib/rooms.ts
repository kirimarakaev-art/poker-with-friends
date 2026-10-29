// Room rules mirrored from docs/API.md ("Столы").

export const MAX_CHIPS = 1_000_000;
const CODE_RE = /^[ABCDEFGHJKLMNPQRSTUVWXYZ23456789]{6}$/;

export interface BuyInBounds {
  min: number;
  max: number;
  initial: number;
  canAfford: boolean; // false: the balance is below the table minimum
}

// buyInBounds is the range a player can bring to a table: up to the table maximum,
// but not more than the balance. The default is as much as allowed.
export function buyInBounds(room: { minBuyIn: number; maxBuyIn: number }, balance: number): BuyInBounds {
  const canAfford = balance >= room.minBuyIn;
  const max = canAfford ? Math.min(room.maxBuyIn, balance) : room.minBuyIn;
  return { min: room.minBuyIn, max, initial: max, canAfford };
}

export function clampBuyIn(value: number, bounds: BuyInBounds): number {
  if (!Number.isFinite(value)) return bounds.min;
  return Math.min(bounds.max, Math.max(bounds.min, Math.floor(value)));
}

export interface RoomSettings {
  smallBlind: number;
  bigBlind: number;
  minBuyIn: number;
  maxBuyIn: number;
}

// validateRoomSettings checks 0 < smallBlind <= bigBlind <= minBuyIn <= maxBuyIn <= 1 000 000.
export function validateRoomSettings(s: RoomSettings): string | null {
  const values = [s.smallBlind, s.bigBlind, s.minBuyIn, s.maxBuyIn];
  if (!values.every(Number.isInteger)) return 'Все значения — целые числа';
  if (!(s.smallBlind > 0)) return 'Малый блайнд должен быть больше нуля';
  if (s.bigBlind < s.smallBlind) return 'Большой блайнд не меньше малого';
  if (s.minBuyIn < s.bigBlind) return 'Минимальный бай-ин не меньше большого блайнда';
  if (s.maxBuyIn < s.minBuyIn) return 'Максимальный бай-ин не меньше минимального';
  if (s.maxBuyIn > MAX_CHIPS) return 'Бай-ин не больше 1 000 000';
  return null;
}

// normalizeRoomCode returns the code in upper case, or null if it cannot be a room code.
export function normalizeRoomCode(input: string): string | null {
  const code = input.trim().toUpperCase();
  return CODE_RE.test(code) ? code : null;
}
