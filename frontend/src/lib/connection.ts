// What the client should do when the game WebSocket closes. See docs/API.md.
export type CloseOutcome = 'replaced' | 'left' | 'not-seated' | 'room-gone' | 'reconnect';

export const CLOSE_REPLACED = 4001;
export const CLOSE_LEFT = 4002;
export const CLOSE_ROOM_NOT_FOUND = 4004;
export const CLOSE_POLICY_VIOLATION = 1008;

export function closeOutcome(code: number): CloseOutcome {
  switch (code) {
    case CLOSE_REPLACED:
      return 'replaced';
    case CLOSE_LEFT:
      return 'left';
    case CLOSE_POLICY_VIOLATION:
      return 'not-seated'; // not logged in, or not sitting at this table
    case CLOSE_ROOM_NOT_FOUND:
      return 'room-gone';
  }
  return 'reconnect';
}

const RECONNECT_DELAYS_MS = [1000, 2000, 5000, 10000];

export function reconnectDelay(attempt: number): number {
  return RECONNECT_DELAYS_MS[Math.min(attempt, RECONNECT_DELAYS_MS.length - 1)];
}
