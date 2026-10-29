import { TableState } from '../types';

export interface SeatAmount {
  seat: number;
  amount: number;
}

// TableEvents is what changed between two states, so the table can animate
// the change. The server state itself is always rendered immediately.
export interface TableEvents {
  newHand: boolean;
  newBoardCards: number[];
  revealedSeats: number[];
  foldedSeats: number[];
  betsPlaced: SeatAmount[];
  betsToPot: SeatAmount[];
  wins: SeatAmount[];
}

export const NO_EVENTS: TableEvents = {
  newHand: false,
  newBoardCards: [],
  revealedSeats: [],
  foldedSeats: [],
  betsPlaced: [],
  betsToPot: [],
  wins: [],
};

// diffStates compares two consecutive states. A null prev means the first
// state after (re)connecting, which is shown without animations.
export function diffStates(prev: TableState | null, next: TableState): TableEvents {
  if (!prev) {
    return NO_EVENTS;
  }
  if (prev.hand !== next.hand) {
    return { ...NO_EVENTS, newHand: next.hand > 0 };
  }
  const streetChanged = prev.street !== next.street;
  // Only seats occupied in both states: a player who just sat down or left has nothing to animate.
  const pairs = next.players.flatMap((after, seat) => {
    const before = prev.players[seat];
    return before && after ? [{ seat, before, after }] : [];
  });

  return {
    newHand: false,
    newBoardCards: range(prev.board.length, next.board.length),
    revealedSeats: pairs
      .filter(({ seat, before, after }) => seat !== next.you && !before.cards && after.cards)
      .map(({ seat }) => seat),
    foldedSeats: pairs.filter(({ before, after }) => !before.folded && after.folded).map(({ seat }) => seat),
    betsPlaced: streetChanged
      ? []
      : pairs
          .filter(({ before, after }) => after.bet > before.bet)
          .map(({ seat, before, after }) => ({ seat, amount: after.bet - before.bet })),
    betsToPot: streetChanged
      ? pairs.filter(({ before }) => before.bet > 0).map(({ seat, before }) => ({ seat, amount: before.bet }))
      : [],
    wins:
      next.street === 'Showdown' && prev.street !== 'Showdown'
        ? pairs.filter(({ after }) => (after.won ?? 0) > 0).map(({ seat, after }) => ({ seat, amount: after.won ?? 0 }))
        : [],
  };
}

function range(from: number, to: number): number[] {
  return Array.from({ length: Math.max(0, to - from) }, (_, i) => from + i);
}
